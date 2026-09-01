package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/schema"
)

// seedIndex 直接写入索引文件（当前信封格式），用于构造"历史上已存在某时刻快照"的场景。
// 走公开 API 无法伪造过去的时间，只能这样注入。
func seedIndex(t *testing.T, workspace string, snaps []*Snapshot) {
	t.Helper()
	data, err := schema.MarshalAs(schema.SnapshotIndex, snapshotIndex{Snapshots: snaps})
	if err != nil {
		t.Fatalf("序列化种子索引失败: %v", err)
	}
	if err := atomicWrite(snapshotIndexPath(workspace), data, 0644); err != nil {
		t.Fatalf("写入种子索引失败: %v", err)
	}
}

// seedIndexV1 写入 v1 旧格式索引（顶层内联 schemaVersion，无信封），
// 用于验证升级路径不会丢历史。
func seedIndexV1(t *testing.T, workspace string, snaps []*Snapshot) {
	t.Helper()
	data, err := json.MarshalIndent(snapshotIndexV1{SchemaVersion: 1, Snapshots: snaps}, "", "  ")
	if err != nil {
		t.Fatalf("序列化 v1 种子索引失败: %v", err)
	}
	if err := atomicWrite(snapshotIndexPath(workspace), data, 0644); err != nil {
		t.Fatalf("写入 v1 种子索引失败: %v", err)
	}
}

// seedObject 为种子快照补上内容对象，否则读取会报「内容已丢失」
func seedObject(t *testing.T, s *SnapshotService, workspace, content string) string {
	t.Helper()
	hash := hashContent(content)
	if err := s.writeObject(workspace, hash, content); err != nil {
		t.Fatalf("写入种子对象失败: %v", err)
	}
	return hash
}

func countIndexEntries(t *testing.T, s *SnapshotService, workspace string) int {
	t.Helper()
	return len(s.loadIndex(workspace).Snapshots)
}

// ---------------------------------------------------------------------------
// 留存
// ---------------------------------------------------------------------------

func TestSnapshot_CaptureCreatesObjectAndIndex(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, "note.md", "# Hello\nworld", "")
	if err != nil {
		t.Fatalf("captureSnapshot 失败: %v", err)
	}
	if snap == nil {
		t.Fatal("首次留存不应被跳过")
	}
	if snap.Path != "note.md" {
		t.Fatalf("path 应为 note.md，得到 %s", snap.Path)
	}
	if snap.Reason != SnapshotReasonSave {
		t.Fatalf("reason 应为 save，得到 %s", snap.Reason)
	}
	if snap.Size != int64(len("# Hello\nworld")) {
		t.Fatalf("size 应为原始字节数，得到 %d", snap.Size)
	}
	// 对象须落在内容寻址路径上
	if _, err := os.Stat(snapshotObjectPath(dir, snap.Hash)); err != nil {
		t.Fatalf("内容对象未落盘: %v", err)
	}
	// 派生数据必须只在 .notevault/（红线 2）
	if _, err := os.Stat(filepath.Join(dir, ".notevault", "history", "index.json")); err != nil {
		t.Fatalf("索引应位于 .notevault/history/: %v", err)
	}
}

func TestSnapshot_ObjectIsCompressed(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	// 高度重复的内容，gzip 应显著小于原文
	content := strings.Repeat("重复的一行内容用于验证压缩生效\n", 200)
	snap, err := s.captureSnapshot(dir, "big.md", content, SnapshotReasonManual)
	if err != nil {
		t.Fatalf("captureSnapshot 失败: %v", err)
	}
	info, err := os.Stat(snapshotObjectPath(dir, snap.Hash))
	if err != nil {
		t.Fatalf("对象未落盘: %v", err)
	}
	if info.Size() >= int64(len(content)) {
		t.Fatalf("对象应被压缩：磁盘 %d 字节 vs 原文 %d 字节", info.Size(), len(content))
	}
	// 解压后必须与原文完全一致
	got, err := s.GetSnapshotContent(dir, snap.ID)
	if err != nil {
		t.Fatalf("GetSnapshotContent 失败: %v", err)
	}
	if got != content {
		t.Fatal("解压内容与原文不一致")
	}
}

func TestSnapshot_DedupesIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	first, err := s.captureSnapshot(dir, "note.md", "same", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("首次留存失败: %v", err)
	}
	second, err := s.captureSnapshot(dir, "note.md", "same", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("二次留存失败: %v", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatal("内容未变时应复用已有快照，不新增条目")
	}
	if n := countIndexEntries(t, s, dir); n != 1 {
		t.Fatalf("索引应只有 1 条，得到 %d", n)
	}
}

// 内容寻址的核心收益：两个不同文件内容相同时只占一份磁盘
func TestSnapshot_ContentAddressingSharesObject(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	a, err := s.captureSnapshot(dir, "a.md", "shared body", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("留存 a 失败: %v", err)
	}
	b, err := s.captureSnapshot(dir, "b.md", "shared body", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("留存 b 失败: %v", err)
	}
	if a.Hash != b.Hash {
		t.Fatal("相同内容应得到相同 hash")
	}
	stats, err := s.GetSnapshotStats(dir)
	if err != nil {
		t.Fatalf("GetSnapshotStats 失败: %v", err)
	}
	if stats.Snapshots != 2 {
		t.Fatalf("应有 2 条快照，得到 %d", stats.Snapshots)
	}
	if stats.Objects != 1 {
		t.Fatalf("相同内容应只占 1 个对象，得到 %d", stats.Objects)
	}
	if stats.Files != 2 {
		t.Fatalf("应覆盖 2 个文件，得到 %d", stats.Files)
	}
}

// 节流：编辑器自动保存频繁，短时间内的连续 save 不该堆快照
func TestSnapshot_ThrottlesFrequentSaves(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if _, err := s.captureSnapshot(dir, "note.md", "v1", SnapshotReasonSave); err != nil {
		t.Fatalf("首次留存失败: %v", err)
	}
	skipped, err := s.captureSnapshot(dir, "note.md", "v2", SnapshotReasonSave)
	if err != nil {
		t.Fatalf("二次留存不应报错: %v", err)
	}
	if skipped != nil {
		t.Fatal("间隔内的 save 应被跳过（返回 nil 快照且无错误）")
	}
	if n := countIndexEntries(t, s, dir); n != 1 {
		t.Fatalf("节流后索引应仍为 1 条，得到 %d", n)
	}
}

// manual / restore / delete 属于用户主动语义，必须无视节流
func TestSnapshot_ManualBypassesThrottle(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if _, err := s.captureSnapshot(dir, "note.md", "v1", SnapshotReasonSave); err != nil {
		t.Fatalf("首次留存失败: %v", err)
	}
	manual, err := s.captureSnapshot(dir, "note.md", "v2", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("manual 留存失败: %v", err)
	}
	if manual == nil {
		t.Fatal("manual 快照不应被节流跳过")
	}
	if n := countIndexEntries(t, s, dir); n != 2 {
		t.Fatalf("索引应有 2 条，得到 %d", n)
	}
}

// 跨日：即使距上次快照很近（按 interval 该跳过），当日首份也必须留下
func TestSnapshot_PromotesFirstOfDayToDaily(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	yesterday := time.Now().AddDate(0, 0, -1)
	hash := seedObject(t, s, dir, "old body")
	seedIndex(t, dir, []*Snapshot{{
		ID:        "seed-1",
		Path:      "note.md",
		Hash:      hash,
		Size:      int64(len("old body")),
		CreatedAt: yesterday.Format(time.RFC3339),
		Reason:    SnapshotReasonSave,
	}})

	snap, err := s.captureSnapshot(dir, "note.md", "new body", SnapshotReasonSave)
	if err != nil {
		t.Fatalf("captureSnapshot 失败: %v", err)
	}
	if snap == nil {
		t.Fatal("当日首份快照不应被跳过")
	}
	if snap.Reason != SnapshotReasonDaily {
		t.Fatalf("当日首份应标记为 daily，得到 %s", snap.Reason)
	}
}

func TestSnapshot_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	for _, bad := range []string{"../outside.md", filepath.Join("..", "..", "etc", "passwd")} {
		if _, err := s.captureSnapshot(dir, bad, "x", SnapshotReasonManual); err == nil {
			t.Fatalf("越界路径 %q 应被拒绝", bad)
		} else if !core.IsCode(err, core.ErrInvalidInput) {
			t.Fatalf("越界路径应返回 INVALID_INPUT，得到 %v", err)
		}
	}
}

func TestSnapshot_RejectsOversizedContent(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	huge := strings.Repeat("x", snapshotMaxFileSize+1)
	if _, err := s.captureSnapshot(dir, "huge.md", huge, SnapshotReasonManual); err == nil {
		t.Fatal("超过大小上限的内容应被拒绝")
	}
}

func TestSnapshot_RejectsEmptyWorkspace(t *testing.T) {
	s := NewSnapshotService()
	if _, err := s.captureSnapshot("", "note.md", "x", SnapshotReasonManual); err == nil {
		t.Fatal("空工作区路径应被拒绝")
	}
	if _, err := s.ListSnapshots("", ""); err == nil {
		t.Fatal("空工作区路径应被拒绝")
	}
	if _, err := s.ListSnapshotFiles(""); err == nil {
		t.Fatal("空工作区路径应被拒绝")
	}
}

// 路径分隔符归一化：同一份 .notevault 在 Windows / macOS 打开不能出现两套键
func TestSnapshot_NormalizesPathSeparators(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, filepath.Join("sub", "note.md"), "body", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("captureSnapshot 失败: %v", err)
	}
	if snap.Path != "sub/note.md" {
		t.Fatalf("索引键应统一为 / 分隔，得到 %q", snap.Path)
	}
	// 用反斜杠查询也应命中同一条
	list, err := s.ListSnapshots(dir, "sub\\note.md")
	if err != nil {
		t.Fatalf("ListSnapshots 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("反斜杠查询应命中 1 条，得到 %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

func TestSnapshot_ListOrdersNewestFirstAndFilters(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	base := time.Now()
	h1 := seedObject(t, s, dir, "a1")
	h2 := seedObject(t, s, dir, "a2")
	h3 := seedObject(t, s, dir, "b1")
	seedIndex(t, dir, []*Snapshot{
		{ID: "old", Path: "a.md", Hash: h1, CreatedAt: base.Add(-2 * time.Hour).Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "new", Path: "a.md", Hash: h2, CreatedAt: base.Add(-1 * time.Hour).Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "other", Path: "b.md", Hash: h3, CreatedAt: base.Format(time.RFC3339), Reason: SnapshotReasonSave},
	})

	all, err := s.ListSnapshots(dir, "")
	if err != nil {
		t.Fatalf("ListSnapshots 失败: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("应返回 3 条，得到 %d", len(all))
	}
	if all[0].ID != "other" {
		t.Fatalf("应按时间倒序，首条得到 %s", all[0].ID)
	}

	only, err := s.ListSnapshots(dir, "a.md")
	if err != nil {
		t.Fatalf("ListSnapshots(a.md) 失败: %v", err)
	}
	if len(only) != 2 || only[0].ID != "new" {
		t.Fatalf("按文件过滤结果不符: %+v", only)
	}
}

func TestSnapshot_ListFilesAggregates(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if _, err := s.captureSnapshot(dir, "a.md", "a1", SnapshotReasonManual); err != nil {
		t.Fatal(err)
	}
	if _, err := s.captureSnapshot(dir, "a.md", "a2", SnapshotReasonManual); err != nil {
		t.Fatal(err)
	}
	if _, err := s.captureSnapshot(dir, "b.md", "b1", SnapshotReasonManual); err != nil {
		t.Fatal(err)
	}

	files, err := s.ListSnapshotFiles(dir)
	if err != nil {
		t.Fatalf("ListSnapshotFiles 失败: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("应聚合出 2 个文件，得到 %d", len(files))
	}
	byPath := map[string]*SnapshotFileSummary{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if byPath["a.md"].Count != 2 {
		t.Fatalf("a.md 应有 2 份快照，得到 %d", byPath["a.md"].Count)
	}
	if byPath["a.md"].Bytes != 4 {
		t.Fatalf("a.md 累计字节应为 4，得到 %d", byPath["a.md"].Bytes)
	}
	if byPath["b.md"].LatestAt == "" {
		t.Fatal("latestAt 不应为空")
	}
}

func TestSnapshot_GetContentMissingObject(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	// 索引里有条目但对象文件缺失（用户手删了 objects 目录）：须报 NOT_FOUND 而非 panic
	seedIndex(t, dir, []*Snapshot{{
		ID: "ghost", Path: "note.md", Hash: hashContent("gone"),
		CreatedAt: time.Now().Format(time.RFC3339), Reason: SnapshotReasonSave,
	}})
	if _, err := s.GetSnapshotContent(dir, "ghost"); err == nil {
		t.Fatal("对象缺失应报错")
	} else if !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("应返回 NOT_FOUND，得到 %v", err)
	}
}

func TestSnapshot_FindRejectsUnknownID(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if _, err := s.GetSnapshotContent(dir, "no-such-id"); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("未知 ID 应返回 NOT_FOUND，得到 %v", err)
	}
	if _, err := s.GetSnapshotContent(dir, ""); !core.IsCode(err, core.ErrInvalidInput) {
		t.Fatalf("空 ID 应返回 INVALID_INPUT，得到 %v", err)
	}
}

// ---------------------------------------------------------------------------
// 比对
// ---------------------------------------------------------------------------

func TestSnapshot_DiffWithCurrent(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, "note.md", "line1\nline2", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "note.md"), "line1\nline2 changed")

	diff, err := s.DiffWithCurrent(dir, snap.ID)
	if err != nil {
		t.Fatalf("DiffWithCurrent 失败: %v", err)
	}
	if diff.ToID != "" {
		t.Fatalf("与当前文件比对时 toId 应为空，得到 %q", diff.ToID)
	}
	if diff.Added != 1 || diff.Removed != 1 {
		t.Fatalf("期望 +1 -1，得到 +%d -%d", diff.Added, diff.Removed)
	}
	if diff.Identical {
		t.Fatal("有差异时 identical 应为 false")
	}
	if diff.ToAt == "" {
		t.Fatal("当前文件存在时应带上其修改时间")
	}
}

// 文件已被删除：diff 应表达为「全部删除」，这是恢复误删的入口视图
func TestSnapshot_DiffWithCurrentWhenFileDeleted(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, "gone.md", "a\nb\nc", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := s.DiffWithCurrent(dir, snap.ID)
	if err != nil {
		t.Fatalf("DiffWithCurrent 失败: %v", err)
	}
	if diff.Removed != 3 || diff.Added != 0 {
		t.Fatalf("文件不存在应显示全部删除，得到 +%d -%d", diff.Added, diff.Removed)
	}
}

func TestSnapshot_DiffWithCurrentIdentical(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	mustWrite(t, filepath.Join(dir, "note.md"), "same\ncontent")
	snap, err := s.captureSnapshot(dir, "note.md", "same\ncontent", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := s.DiffWithCurrent(dir, snap.ID)
	if err != nil {
		t.Fatalf("DiffWithCurrent 失败: %v", err)
	}
	if !diff.Identical {
		t.Fatal("内容一致时 identical 应为 true")
	}
}

func TestSnapshot_DiffTwoSnapshots(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	v1, err := s.captureSnapshot(dir, "note.md", "a\nb", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.captureSnapshot(dir, "note.md", "a\nb\nc", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := s.DiffSnapshots(dir, v1.ID, v2.ID)
	if err != nil {
		t.Fatalf("DiffSnapshots 失败: %v", err)
	}
	if diff.Added != 1 || diff.Removed != 0 {
		t.Fatalf("期望 +1 -0，得到 +%d -%d", diff.Added, diff.Removed)
	}
	if diff.FromID != v1.ID || diff.ToID != v2.ID {
		t.Fatal("fromId/toId 应如实回传")
	}
}

func TestSnapshot_DiffRejectsCrossFile(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	a, err := s.captureSnapshot(dir, "a.md", "a", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.captureSnapshot(dir, "b.md", "b", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DiffSnapshots(dir, a.ID, b.ID); err == nil {
		t.Fatal("跨文件比对应被拒绝（否则 diff 结果毫无意义）")
	}
}

// ---------------------------------------------------------------------------
// 恢复
// ---------------------------------------------------------------------------

func TestSnapshot_RestoreWritesFileAndBacksUpCurrent(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	original := "# Draft\n原始内容"
	mustWrite(t, filepath.Join(dir, "note.md"), original)
	snap, err := s.captureSnapshot(dir, "note.md", original, SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}

	// 模拟误改
	mustWrite(t, filepath.Join(dir, "note.md"), "被误改的内容")

	res, err := s.RestoreSnapshot(dir, snap.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot 失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatalf("读回文件失败: %v", err)
	}
	if string(got) != original {
		t.Fatalf("恢复后内容不符，得到 %q", string(got))
	}
	if res.RestoredID != snap.ID {
		t.Fatalf("restoredId 不符: %s", res.RestoredID)
	}
	// 恢复前必须留安全备份，否则「恢复错了」无法回头
	if res.BackupID == "" {
		t.Fatal("恢复前应自动留存安全备份")
	}
	backup, err := s.GetSnapshotContent(dir, res.BackupID)
	if err != nil {
		t.Fatalf("读取安全备份失败: %v", err)
	}
	if backup != "被误改的内容" {
		t.Fatalf("安全备份应是恢复前的内容，得到 %q", backup)
	}
}

// 误删恢复：文件已不存在时 restore 要能重建它（含父目录）
func TestSnapshot_RestoreRecreatesDeletedFile(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, "sub/deep/note.md", "重要内容", SnapshotReasonDelete)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.RestoreSnapshot(dir, snap.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot 失败: %v", err)
	}
	if res.BackupID != "" {
		t.Fatal("目标文件不存在时不应产生安全备份")
	}
	got, err := os.ReadFile(filepath.Join(dir, "sub", "deep", "note.md"))
	if err != nil {
		t.Fatalf("恢复后文件应存在: %v", err)
	}
	if string(got) != "重要内容" {
		t.Fatalf("恢复内容不符: %q", string(got))
	}
}

// ---------------------------------------------------------------------------
// 清理与保留策略
// ---------------------------------------------------------------------------

func TestSnapshot_DeleteRemovesEntryAndObject(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	snap, err := s.captureSnapshot(dir, "note.md", "body", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	objPath := snapshotObjectPath(dir, snap.Hash)
	if err := s.DeleteSnapshot(dir, snap.ID); err != nil {
		t.Fatalf("DeleteSnapshot 失败: %v", err)
	}
	if n := countIndexEntries(t, s, dir); n != 0 {
		t.Fatalf("索引应清空，得到 %d 条", n)
	}
	// 无引用对象应被回收，否则历史目录只增不减
	if _, err := os.Stat(objPath); !os.IsNotExist(err) {
		t.Fatal("无引用的内容对象应被 GC")
	}
	if err := s.DeleteSnapshot(dir, snap.ID); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("重复删除应返回 NOT_FOUND，得到 %v", err)
	}
}

// 共享对象不能被误删：a.md 的快照删掉后，b.md 引用的同内容对象必须还在
func TestSnapshot_GCKeepsSharedObject(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	a, err := s.captureSnapshot(dir, "a.md", "shared", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.captureSnapshot(dir, "b.md", "shared", SnapshotReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSnapshot(dir, a.ID); err != nil {
		t.Fatalf("DeleteSnapshot 失败: %v", err)
	}
	content, err := s.GetSnapshotContent(dir, b.ID)
	if err != nil {
		t.Fatalf("共享对象被误删了: %v", err)
	}
	if content != "shared" {
		t.Fatalf("内容不符: %q", content)
	}
}

func TestSnapshot_ClearScopedAndGlobal(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	for _, c := range []struct{ path, body string }{
		{"a.md", "a1"}, {"a.md", "a2"}, {"b.md", "b1"},
	} {
		if _, err := s.captureSnapshot(dir, c.path, c.body, SnapshotReasonManual); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := s.ClearSnapshots(dir, "a.md")
	if err != nil {
		t.Fatalf("ClearSnapshots(a.md) 失败: %v", err)
	}
	if removed != 2 {
		t.Fatalf("应删除 2 条，得到 %d", removed)
	}
	if n := countIndexEntries(t, s, dir); n != 1 {
		t.Fatalf("应剩 1 条，得到 %d", n)
	}

	removed, err = s.ClearSnapshots(dir, "")
	if err != nil {
		t.Fatalf("ClearSnapshots(全量) 失败: %v", err)
	}
	if removed != 1 {
		t.Fatalf("应删除 1 条，得到 %d", removed)
	}
	if n := countIndexEntries(t, s, dir); n != 0 {
		t.Fatalf("应清空，得到 %d", n)
	}
	// 不存在的文件返回 0 而非报错
	if removed, err = s.ClearSnapshots(dir, "nope.md"); err != nil || removed != 0 {
		t.Fatalf("清理不存在的文件应返回 (0, nil)，得到 (%d, %v)", removed, err)
	}
}

func TestSnapshot_RetentionKeepsNewestAndManual(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -(snapshotKeepDays + 10))

	snaps := []*Snapshot{
		{ID: "newest", Path: "a.md", Hash: "h1", CreatedAt: now.Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "ancient-save", Path: "a.md", Hash: "h2", CreatedAt: old.Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "ancient-manual", Path: "a.md", Hash: "h3", CreatedAt: old.Format(time.RFC3339), Reason: SnapshotReasonManual},
	}
	kept, dropped := applySnapshotRetention(snaps, now)

	keptIDs := map[string]bool{}
	for _, s := range kept {
		keptIDs[s.ID] = true
	}
	if !keptIDs["newest"] {
		t.Error("最新一份必须保留（不能把唯一后路清掉）")
	}
	if !keptIDs["ancient-manual"] {
		t.Error("manual 快照永不自动淘汰")
	}
	if keptIDs["ancient-save"] {
		t.Error("超期的 save 快照应被淘汰")
	}
	if len(dropped) != 1 || dropped[0].ID != "ancient-save" {
		t.Errorf("淘汰集合不符: %+v", dropped)
	}
}

func TestSnapshot_RetentionCapsPerFileSaveCount(t *testing.T) {
	now := time.Now()
	snaps := make([]*Snapshot, 0, snapshotMaxPerFile+20)
	for i := 0; i < snapshotMaxPerFile+20; i++ {
		snaps = append(snaps, &Snapshot{
			ID:        "s" + string(rune('A'+i%26)) + time.Duration(i).String(),
			Path:      "a.md",
			Hash:      "h",
			CreatedAt: now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			Reason:    SnapshotReasonSave,
		})
	}
	kept, _ := applySnapshotRetention(snaps, now)
	// 最新那份走 i==0 分支单独保留，其余 save 受 maxPerFile 限制
	if len(kept) > snapshotMaxPerFile+1 {
		t.Fatalf("单文件 save 快照应受上限约束，保留了 %d 份（上限 %d）", len(kept), snapshotMaxPerFile+1)
	}
	if len(kept) < snapshotMaxPerFile {
		t.Fatalf("不应过度淘汰，仅保留了 %d 份", len(kept))
	}
}

// 保留策略按文件独立计算：A 文件超量不能拖累 B 文件
func TestSnapshot_RetentionIsPerFile(t *testing.T) {
	now := time.Now()
	snaps := []*Snapshot{
		{ID: "a-new", Path: "a.md", Hash: "h", CreatedAt: now.Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "b-new", Path: "b.md", Hash: "h", CreatedAt: now.Format(time.RFC3339), Reason: SnapshotReasonSave},
	}
	kept, dropped := applySnapshotRetention(snaps, now)
	if len(kept) != 2 || len(dropped) != 0 {
		t.Fatalf("两个文件各自的最新快照都应保留，kept=%d dropped=%d", len(kept), len(dropped))
	}
}

func TestSnapshot_PruneReturnsStats(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	old := time.Now().AddDate(0, 0, -(snapshotKeepDays + 5))
	hOld := seedObject(t, s, dir, "very old")
	hNew := seedObject(t, s, dir, "recent")
	seedIndex(t, dir, []*Snapshot{
		{ID: "old", Path: "a.md", Hash: hOld, Size: 8, CreatedAt: old.Format(time.RFC3339), Reason: SnapshotReasonSave},
		{ID: "new", Path: "a.md", Hash: hNew, Size: 6, CreatedAt: time.Now().Format(time.RFC3339), Reason: SnapshotReasonSave},
	})

	stats, err := s.PruneSnapshots(dir)
	if err != nil {
		t.Fatalf("PruneSnapshots 失败: %v", err)
	}
	if stats.Snapshots != 1 {
		t.Fatalf("超期快照应被清理，剩余 %d 条", stats.Snapshots)
	}
	if stats.Objects != 1 {
		t.Fatalf("失去引用的对象应被回收，剩余 %d 个", stats.Objects)
	}
	if stats.DiskBytes <= 0 {
		t.Fatal("diskBytes 应统计实际占用")
	}
}

// ---------------------------------------------------------------------------
// 健壮性
// ---------------------------------------------------------------------------

// schemaVersion 不匹配时丢弃重建（E-7 统一约定），绝不尝试兼容解析
func TestSnapshot_DiscardsIndexOnSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	raw := `{"schemaVersion":999,"snapshots":[{"id":"legacy","path":"a.md","hash":"x"}]}`
	if err := atomicWrite(snapshotIndexPath(dir), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	if n := countIndexEntries(t, s, dir); n != 0 {
		t.Fatalf("schema 不匹配应丢弃索引，得到 %d 条", n)
	}
}

// 索引损坏（半截 JSON）不能让整个功能崩掉——历史是保险，不是主链路
func TestSnapshot_SurvivesCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if err := atomicWrite(snapshotIndexPath(dir), []byte(`{"schemaVersion":1,"snap`), 0644); err != nil {
		t.Fatal(err)
	}
	snap, err := s.captureSnapshot(dir, "note.md", "body", SnapshotReasonManual)
	if err != nil {
		t.Fatalf("索引损坏时仍应能留存新快照: %v", err)
	}
	if snap == nil {
		t.Fatal("应返回新快照")
	}
}

// 并发保存不能丢条目（loadIndex → append → saveIndex 必须整体串行）
func TestSnapshot_ConcurrentCapturesDoNotLoseEntries(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := "note" + string(rune('a'+i)) + ".md"
			if _, err := s.captureSnapshot(dir, path, "body-"+path, SnapshotReasonManual); err != nil {
				t.Errorf("并发留存失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := countIndexEntries(t, s, dir); got != n {
		t.Fatalf("并发留存丢条目：期望 %d 条，实得 %d 条", n, got)
	}
}

// 未压缩的历史对象（早期格式）仍须能读出，避免格式演进废掉旧快照
func TestSnapshot_ReadsUncompressedObject(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	content := "plain stored body"
	hash := hashContent(content)
	if err := atomicWrite(snapshotObjectPath(dir, hash), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	seedIndex(t, dir, []*Snapshot{{
		ID: "legacy", Path: "a.md", Hash: hash,
		CreatedAt: time.Now().Format(time.RFC3339), Reason: SnapshotReasonSave,
	}})

	got, err := s.GetSnapshotContent(dir, "legacy")
	if err != nil {
		t.Fatalf("读取未压缩对象失败: %v", err)
	}
	if got != content {
		t.Fatalf("内容不符: %q", got)
	}
}

func TestSnapshot_StatsOnEmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	stats, err := s.GetSnapshotStats(dir)
	if err != nil {
		t.Fatalf("空工作区统计不应报错: %v", err)
	}
	if stats.Snapshots != 0 || stats.Objects != 0 || stats.DiskBytes != 0 {
		t.Fatalf("空工作区统计应全为 0，得到 %+v", stats)
	}
}

func TestSnapshot_CreateManualRequiresExistingFile(t *testing.T) {
	dir := t.TempDir()
	s := NewSnapshotService()

	if _, err := s.CreateManualSnapshot(dir, "missing.md"); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("文件不存在应返回 NOT_FOUND，得到 %v", err)
	}

	mustWrite(t, filepath.Join(dir, "note.md"), "手动打点内容")
	snap, err := s.CreateManualSnapshot(dir, "note.md")
	if err != nil {
		t.Fatalf("CreateManualSnapshot 失败: %v", err)
	}
	if snap.Reason != SnapshotReasonManual {
		t.Fatalf("reason 应为 manual，得到 %s", snap.Reason)
	}
}

func TestSnapshot_NewSnapshotIDIsUnique(t *testing.T) {
	now := time.Now()
	existing := []*Snapshot{}
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		id := newSnapshotID(existing, now, hashContent("same content"))
		if seen[id] {
			t.Fatalf("ID 重复: %s", id)
		}
		seen[id] = true
		existing = append(existing, &Snapshot{ID: id})
	}
}

// ---------------------------------------------------------------------------
// 与 FileService 的集成
// ---------------------------------------------------------------------------

func TestFileService_SaveCapturesPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	snapshots := NewSnapshotService()
	fs := NewFileServiceWithHistory(snapshots)

	mustWrite(t, filepath.Join(dir, "note.md"), "第一版")
	if err := fs.SaveFile(dir, "note.md", "第二版"); err != nil {
		t.Fatalf("SaveFile 失败: %v", err)
	}

	list, err := snapshots.ListSnapshots(dir, "note.md")
	if err != nil {
		t.Fatalf("ListSnapshots 失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("保存应留下 1 份旧版本快照，得到 %d", len(list))
	}
	// 关键语义：快照的是「覆盖前的内容」
	content, err := snapshots.GetSnapshotContent(dir, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if content != "第一版" {
		t.Fatalf("快照应保存覆盖前的内容，得到 %q", content)
	}
	// 当前文件必须是新内容
	got, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "第二版" {
		t.Fatalf("当前文件应为新内容，得到 %q", string(got))
	}
}

func TestFileService_NewFileHasNoSnapshot(t *testing.T) {
	dir := t.TempDir()
	snapshots := NewSnapshotService()
	fs := NewFileServiceWithHistory(snapshots)

	if err := fs.SaveFile(dir, "brand-new.md", "内容"); err != nil {
		t.Fatalf("SaveFile 失败: %v", err)
	}
	list, err := snapshots.ListSnapshots(dir, "brand-new.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("新建文件没有旧版本可存，应为 0 条，得到 %d", len(list))
	}
}

func TestFileService_DeleteCapturesContent(t *testing.T) {
	dir := t.TempDir()
	snapshots := NewSnapshotService()
	fs := NewFileServiceWithHistory(snapshots)

	mustWrite(t, filepath.Join(dir, "doomed.md"), "别删我")
	if err := fs.DeleteFile(dir, "doomed.md"); err != nil {
		t.Fatalf("DeleteFile 失败: %v", err)
	}
	list, err := snapshots.ListSnapshots(dir, "doomed.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("硬删除应留下快照，得到 %d 条", len(list))
	}
	if list[0].Reason != SnapshotReasonDelete {
		t.Fatalf("reason 应为 delete，得到 %s", list[0].Reason)
	}
	// 完整的误删恢复闭环
	if _, err := snapshots.RestoreSnapshot(dir, list[0].ID); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "doomed.md"))
	if err != nil {
		t.Fatalf("恢复后文件应存在: %v", err)
	}
	if string(got) != "别删我" {
		t.Fatalf("恢复内容不符: %q", string(got))
	}
}

// 红线 1：依赖必须可选可降级——不注入历史时功能不能残废
func TestFileService_WorksWithoutHistory(t *testing.T) {
	dir := t.TempDir()
	fs := NewFileService()

	mustWrite(t, filepath.Join(dir, "note.md"), "v1")
	if err := fs.SaveFile(dir, "note.md", "v2"); err != nil {
		t.Fatalf("无历史注入时 SaveFile 应正常: %v", err)
	}
	if err := fs.DeleteFile(dir, "note.md"); err != nil {
		t.Fatalf("无历史注入时 DeleteFile 应正常: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".notevault", "history")); !os.IsNotExist(err) {
		t.Fatal("未启用历史时不应创建 history 目录")
	}
}

// 历史留存失败绝不能阻断用户保存
type failingCapturer struct{ calls int }

func (f *failingCapturer) captureBeforeWrite(string, string) (*Snapshot, error) {
	f.calls++
	return nil, core.NewError(core.ErrInternal, "磁盘写满了")
}

func (f *failingCapturer) captureBeforeDelete(string, string) (*Snapshot, error) {
	f.calls++
	return nil, core.NewError(core.ErrInternal, "磁盘写满了")
}

func TestFileService_HistoryFailureDoesNotBlockSave(t *testing.T) {
	dir := t.TempDir()
	cap := &failingCapturer{}
	fs := NewFileServiceWithHistory(cap)

	mustWrite(t, filepath.Join(dir, "note.md"), "v1")
	if err := fs.SaveFile(dir, "note.md", "v2"); err != nil {
		t.Fatalf("快照失败不应阻断保存: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("保存应照常生效，得到 %q", string(got))
	}
	if err := fs.DeleteFile(dir, "note.md"); err != nil {
		t.Fatalf("快照失败不应阻断删除: %v", err)
	}
	if cap.calls != 2 {
		t.Fatalf("保存与删除各应尝试留存 1 次，实际 %d 次", cap.calls)
	}
}
