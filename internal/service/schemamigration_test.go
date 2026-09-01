package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/notevault/notevault/internal/infra/schema"
)

// E-7 统一 schema version 的回归测试。
//
// 这组测试保护的是升级路径，而不是功能本身：已装机用户磁盘上是加信封之前的
// 裸数组 / 裸 map / 内联 version 格式，读不出来等于静默丢数据，而且不会有任何报错
// ——正是这类问题让「加版本字段」这件事必须一次做对。
//
// 每条用例的形状都一样：写入旧格式 → 用公开 API 读 → 断言数据完整 →
// 触发一次写入 → 断言磁盘已升级成信封格式。

// writeLegacyJSON 写入加信封之前的裸格式
func writeLegacyJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("序列化旧格式失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("写入旧格式失败: %v", err)
	}
}

// assertEnveloped 断言文件已经是统一信封格式，且 kind 正确
func assertEnveloped(t *testing.T, path string, d schema.Descriptor) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	var envelope struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		UpdatedAt     string `json:"updatedAt"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	if envelope.SchemaVersion != d.Version {
		t.Errorf("%s schemaVersion = %d, want %d", filepath.Base(path), envelope.SchemaVersion, d.Version)
	}
	if envelope.Kind != d.Kind {
		t.Errorf("%s kind = %q, want %q", filepath.Base(path), envelope.Kind, d.Kind)
	}
	if envelope.UpdatedAt == "" {
		t.Errorf("%s 缺少 updatedAt", filepath.Base(path))
	}
}

// ---------------------------------------------------------------------------
// 回收站
// ---------------------------------------------------------------------------

func TestSchema_TrashIndexReadsLegacyBareArray(t *testing.T) {
	ws := t.TempDir()
	s := NewTrashService()
	indexPath := s.getTrashIndexPath(ws)

	writeLegacyJSON(t, indexPath, []*TrashedFile{
		{ID: "trash_1", Path: "trash_1_a.md", Name: "a.md", OriginalPath: "a.md", DeletedAt: "2026-01-01T00:00:00Z", Size: 12},
		{ID: "trash_2", Path: "trash_2_b.md", Name: "b.md", OriginalPath: "sub/b.md", DeletedAt: "2026-01-02T00:00:00Z", Size: 34},
	})

	files, err := s.GetTrashedFiles(ws)
	if err != nil {
		t.Fatalf("GetTrashedFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("旧格式回收站索引应读出 2 条，得到 %d 条（升级会导致孤儿文件）", len(files))
	}
	if files[0].ID != "trash_1" && files[1].ID != "trash_1" {
		t.Errorf("条目内容丢失: %+v", files)
	}

	// 任意一次写入都应把磁盘升级成信封格式
	if err := s.saveTrashIndex(ws, files); err != nil {
		t.Fatalf("saveTrashIndex: %v", err)
	}
	assertEnveloped(t, indexPath, schema.TrashIndex)

	// 升级后仍能读出同样的数据
	again, err := s.GetTrashedFiles(ws)
	if err != nil {
		t.Fatalf("升级后 GetTrashedFiles: %v", err)
	}
	if len(again) != 2 {
		t.Fatalf("升级后应仍有 2 条，得到 %d 条", len(again))
	}
}

func TestSchema_TrashIndexRoundTripsThroughRealOperations(t *testing.T) {
	ws := t.TempDir()
	s := NewTrashService()
	if err := os.WriteFile(filepath.Join(ws, "note.md"), []byte("hi"), 0644); err != nil {
		t.Fatalf("seed note: %v", err)
	}

	if _, err := s.MoveToTrash(ws, "note.md"); err != nil {
		t.Fatalf("MoveToTrash: %v", err)
	}
	assertEnveloped(t, s.getTrashIndexPath(ws), schema.TrashIndex)

	files, err := s.GetTrashedFiles(ws)
	if err != nil {
		t.Fatalf("GetTrashedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("应有 1 条回收站记录，得到 %d", len(files))
	}
	if err := s.RestoreFromTrash(ws, files[0].ID); err != nil {
		t.Fatalf("RestoreFromTrash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "note.md")); err != nil {
		t.Errorf("恢复后原文件应存在: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 提醒
// ---------------------------------------------------------------------------

func TestSchema_RemindersReadLegacyBareArray(t *testing.T) {
	ws := t.TempDir()
	s := NewReminderService()
	path := s.getRemindersFilePath(ws)

	writeLegacyJSON(t, path, []*Reminder{
		{ID: "r1", FilePath: "a.md", FileName: "a.md", Content: "交房租", RemindAt: "2026-09-01T09:00:00Z", CreatedAt: "2026-08-01T09:00:00Z"},
	})

	reminders, err := s.GetAllReminders(ws)
	if err != nil {
		t.Fatalf("GetAllReminders: %v", err)
	}
	if len(reminders) != 1 || reminders[0].Content != "交房租" {
		t.Fatalf("旧格式提醒应完整读出，得到 %+v", reminders)
	}

	// 新增一条会触发写入，磁盘应升级
	if _, err := s.AddReminder(ws, "b.md", "取快递", "2026-09-02T09:00:00Z"); err != nil {
		t.Fatalf("AddReminder: %v", err)
	}
	assertEnveloped(t, path, schema.Reminders)

	after, err := s.GetAllReminders(ws)
	if err != nil {
		t.Fatalf("升级后 GetAllReminders: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("升级不应丢掉旧提醒，期望 2 条，得到 %d 条", len(after))
	}
}

// ---------------------------------------------------------------------------
// 工作区
// ---------------------------------------------------------------------------

func TestSchema_WorkspaceListReadsLegacyBareArray(t *testing.T) {
	cfg := t.TempDir()
	s := &WorkspaceService{configDir: cfg}

	writeLegacyJSON(t, s.workspacesFilePath(), []Workspace{
		{ID: "ws_1", Name: "知识库", Path: filepath.Join(cfg, "vault1"), CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "ws_2", Name: "工作", Path: filepath.Join(cfg, "vault2"), CreatedAt: "2026-01-02T00:00:00Z"},
	})

	list, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("旧格式工作区列表应读出 2 条（丢了等于用户要重新添加所有知识库），得到 %d", len(list))
	}

	if err := s.saveWorkspaces(list); err != nil {
		t.Fatalf("saveWorkspaces: %v", err)
	}
	assertEnveloped(t, s.workspacesFilePath(), schema.WorkspaceList)

	again, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("升级后 ListWorkspaces: %v", err)
	}
	if len(again) != 2 || again[0].Name != "知识库" {
		t.Fatalf("升级后数据不一致: %+v", again)
	}
}

func TestSchema_CurrentWorkspaceReadsLegacyBareString(t *testing.T) {
	cfg := t.TempDir()
	s := &WorkspaceService{configDir: cfg}

	writeLegacyJSON(t, s.workspacesFilePath(), []Workspace{
		{ID: "ws_1", Name: "知识库", Path: filepath.Join(cfg, "vault1")},
	})
	// 旧格式：裸 JSON 字符串
	if err := os.WriteFile(s.currentWorkspaceFilePath(), []byte(`"ws_1"`), 0644); err != nil {
		t.Fatalf("写入旧格式当前工作区失败: %v", err)
	}

	ws, err := s.GetCurrentWorkspace()
	if err != nil {
		t.Fatalf("GetCurrentWorkspace: %v", err)
	}
	if ws == nil || ws.ID != "ws_1" {
		t.Fatalf("旧格式当前工作区指针应读出 ws_1，得到 %+v", ws)
	}

	if err := s.SetCurrentWorkspace("ws_1"); err != nil {
		t.Fatalf("SetCurrentWorkspace: %v", err)
	}
	assertEnveloped(t, s.currentWorkspaceFilePath(), schema.CurrentWorkspace)

	again, err := s.GetCurrentWorkspace()
	if err != nil {
		t.Fatalf("升级后 GetCurrentWorkspace: %v", err)
	}
	if again == nil || again.ID != "ws_1" {
		t.Fatalf("升级后当前工作区应仍是 ws_1，得到 %+v", again)
	}
}

// ---------------------------------------------------------------------------
// 快照索引
// ---------------------------------------------------------------------------

// TestSchema_SnapshotIndexUpgradesFromV1 是这组测试里最重要的一条：
// 快照索引丢了，objects/ 里的内容还在但再也定位不到，等于历史版本全部消失。
func TestSchema_SnapshotIndexUpgradesFromV1(t *testing.T) {
	ws := t.TempDir()
	s := NewSnapshotService()

	hash := seedObject(t, s, ws, "旧版本内容\n")
	seedIndexV1(t, ws, []*Snapshot{{
		ID:        "snap_v1",
		Path:      "note.md",
		Hash:      hash,
		Size:      int64(len("旧版本内容\n")),
		CreatedAt: time.Now().Add(-48 * time.Hour).Format(time.RFC3339),
		Reason:    SnapshotReasonSave,
	}})

	snaps, err := s.ListSnapshots(ws, "note.md")
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 || snaps[0].ID != "snap_v1" {
		t.Fatalf("v1 索引应被读出，得到 %+v", snaps)
	}

	content, err := s.GetSnapshotContent(ws, "snap_v1")
	if err != nil {
		t.Fatalf("GetSnapshotContent: %v", err)
	}
	if content != "旧版本内容\n" {
		t.Errorf("v1 快照内容应可读，得到 %q", content)
	}

	// 触发一次写入，磁盘升级为信封格式
	if err := os.WriteFile(filepath.Join(ws, "note.md"), []byte("新内容\n"), 0644); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if _, err := s.CreateManualSnapshot(ws, "note.md"); err != nil {
		t.Fatalf("CreateManualSnapshot: %v", err)
	}
	assertEnveloped(t, snapshotIndexPath(ws), schema.SnapshotIndex)

	after, err := s.ListSnapshots(ws, "note.md")
	if err != nil {
		t.Fatalf("升级后 ListSnapshots: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("升级不应丢掉 v1 的历史，期望 2 条，得到 %d 条", len(after))
	}
}

// TestSchema_SnapshotIndexIgnoresNewerVersion 降级安装场景：
// 高版本索引结构未知，按当前结构解析可能读出错误的哈希，进而在清理时删错对象。
func TestSchema_SnapshotIndexIgnoresNewerVersion(t *testing.T) {
	ws := t.TempDir()
	s := NewSnapshotService()

	data, err := schema.Marshal(schema.SnapshotIndex.Kind, schema.SnapshotIndex.Version+1,
		snapshotIndex{Snapshots: []*Snapshot{{ID: "future", Path: "note.md"}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := atomicWrite(snapshotIndexPath(ws), data, 0644); err != nil {
		t.Fatalf("写入高版本索引失败: %v", err)
	}

	snaps, err := s.ListSnapshots(ws, "note.md")
	if err != nil {
		t.Fatalf("ListSnapshots 不应因高版本索引报错: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("高版本索引应被忽略而不是按当前结构解析，得到 %+v", snaps)
	}
}

// ---------------------------------------------------------------------------
// 搜索摘要（唯一「版本不符即丢弃」的文件）
// ---------------------------------------------------------------------------

func TestSchema_SearchSummaryDiscardsLegacyFormat(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("APPDATA", t.TempDir()) // 隔离摘要目录，别污染真实配置

	// 旧格式：顶层 version + entries，没有信封
	legacy := map[string]any{
		"version":       2,
		"workspacePath": filepath.ToSlash(filepath.Clean(ws)),
		"updatedAt":     time.Now().Format(time.RFC3339),
		"entries": []map[string]any{{
			"relPath": "note.md",
			"title":   "Note",
			"modTime": time.Now().Format(time.RFC3339),
			"tokens":  []string{"hello"},
			"freqs":   []int{1},
			"docLen":  1,
		}},
	}
	writeLegacyJSON(t, summaryPathFor(ws), legacy)

	idx := newSearchIndex()
	if err := idx.LoadSummary(ws); err != nil {
		t.Fatalf("LoadSummary 不应因旧格式报错: %v", err)
	}
	if len(idx.docs) != 0 {
		t.Errorf("摘要是纯缓存，旧格式应丢弃重建而不是按当前结构解析，得到 %d 条", len(idx.docs))
	}
}

func TestSchema_SearchSummaryRoundTripsCurrentFormat(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())

	src := newSearchIndex()
	src.docs["note.md"] = &cachedDoc{
		relPath:   "note.md",
		title:     "Note",
		modTime:   time.Now(),
		tokenFreq: map[string]int{"hello": 2},
		docLen:    2,
	}
	if err := src.SaveSummary(ws); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}
	assertEnveloped(t, summaryPathFor(ws), schema.SearchSummary)

	dst := newSearchIndex()
	if err := dst.LoadSummary(ws); err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	doc, ok := dst.docs["note.md"]
	if !ok {
		t.Fatal("当前格式摘要应能加载回来")
	}
	if doc.tokenFreq["hello"] != 2 || doc.docLen != 2 {
		t.Errorf("摘要载荷不一致: %+v", doc)
	}
}

// ---------------------------------------------------------------------------
// Bases 视图定义（.nvbase）
// ---------------------------------------------------------------------------

// TestSchema_BaseDefinitionRoundTrip：.nvbase 从一开始就是信封格式（没有裸格式历史），
// 所以这里保护的是另一头——落盘的信封字段正确、且能原样读回来。
// 视图定义是用户手工搭的查询，读不回来等于配置白搭。
func TestSchema_BaseDefinitionRoundTrip(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()

	def := BaseDef{
		Name:        "阅读清单",
		Description: "在读的 AI 相关笔记",
		Folder:      "notes",
		Filters: BaseFilterGroup{
			Conjunction: ConjAnd,
			Conditions: []BaseFilter{
				{Property: "status", Operator: OpEq, Value: "reading"},
				{Property: "tags", Operator: OpContains, Value: "AI"},
			},
			Groups: []BaseFilterGroup{{
				Conjunction: ConjOr,
				Conditions: []BaseFilter{
					{Property: "rating", Operator: OpGte, Value: "4"},
					{Property: "pinned", Operator: OpEq, Value: "true"},
				},
			}},
		},
		Views: []BaseView{{
			ID:      "v1",
			Name:    "表格",
			Type:    ViewTable,
			Columns: []string{"file.title", "status", "rating"},
			Sort:    []BaseSort{{Property: "rating", Desc: true}},
			GroupBy: "tags",
			Limit:   50,
		}},
	}

	if err := s.SaveBase(ws, def); err != nil {
		t.Fatalf("SaveBase: %v", err)
	}

	full, err := basePath(ws, def.Name)
	if err != nil {
		t.Fatalf("basePath: %v", err)
	}
	assertEnveloped(t, full, schema.BaseDefinition)

	got, err := s.LoadBase(ws, def.Name)
	if err != nil {
		t.Fatalf("LoadBase: %v", err)
	}
	if got.Name != def.Name || got.Description != def.Description || got.Folder != def.Folder {
		t.Errorf("元信息丢失: %+v", got)
	}
	if countFilters(got.Filters) != 4 {
		t.Errorf("条件数 = %d, want 4（嵌套组没读回来）", countFilters(got.Filters))
	}
	if len(got.Views) != 1 {
		t.Fatalf("视图数 = %d, want 1", len(got.Views))
	}
	v := got.Views[0]
	if v.ID != "v1" || v.Type != ViewTable || v.GroupBy != "tags" || v.Limit != 50 {
		t.Errorf("视图配置丢失: %+v", v)
	}
	if len(v.Columns) != 3 || len(v.Sort) != 1 || !v.Sort[0].Desc {
		t.Errorf("列/排序配置丢失: columns=%v sort=%+v", v.Columns, v.Sort)
	}
}

// TestSchema_BaseDefinitionFromNewerVersionStillLoads：高版本写的视图按已知字段解析，
// 而不是整个视图消失。视图是只读查询，最坏结果只是少几个筛选条件。
func TestSchema_BaseDefinitionFromNewerVersionStillLoads(t *testing.T) {
	ws := t.TempDir()
	s := NewBaseService()

	full, err := basePath(ws, "future")
	if err != nil {
		t.Fatalf("basePath: %v", err)
	}
	writeLegacyJSON(t, full, map[string]any{
		"schemaVersion": schema.BaseDefinition.Version + 5,
		"kind":          schema.BaseDefinition.Kind,
		"updatedAt":     time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{
			"name": "future",
			"views": []map[string]any{
				{"id": "v1", "type": "table", "columns": []string{"file.title"}},
			},
			// 未来版本新增的字段：当前代码不认识，但不该因此整份读不出来
			"someFieldFromTheFuture": map[string]any{"nested": true},
		},
	})

	got, err := s.LoadBase(ws, "future")
	if err != nil {
		t.Fatalf("高版本视图应能按已知字段读出来，却报错: %v", err)
	}
	if got.Name != "future" || len(got.Views) != 1 {
		t.Errorf("已知字段没读出来: %+v", got)
	}
}

// TestSchema_AllDescriptorsAreExercised 提醒作者：登记表里加了新文件，
// 这个文件里也要加一条对应的迁移用例，否则升级路径无人看守。
func TestSchema_AllDescriptorsAreExercised(t *testing.T) {
	covered := map[string]bool{
		schema.TrashIndex.Kind:       true, // 本文件
		schema.Reminders.Kind:        true, // 本文件
		schema.WorkspaceList.Kind:    true, // 本文件
		schema.CurrentWorkspace.Kind: true, // 本文件
		schema.SnapshotIndex.Kind:    true, // 本文件
		schema.SearchSummary.Kind:    true, // 本文件
		schema.BaseDefinition.Kind:   true, // 本文件（无裸格式历史，测往返 + 高版本兼容）
		schema.PluginState.Kind:      true, // internal/plugin/schemamigration_test.go
		schema.PluginTrust.Kind:      true, // internal/plugin/schemamigration_test.go
	}
	for _, d := range schema.All() {
		if !covered[d.Kind] {
			t.Errorf("kind %q（%s）没有迁移用例覆盖——旧格式读不出来是静默丢数据", d.Kind, d.Location)
		}
	}
}
