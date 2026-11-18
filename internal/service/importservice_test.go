package service

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

func newImportTestService(t *testing.T) *ImportService {
	t.Helper()
	return NewImportService()
}

// writeTestFile 在目录下写入测试文件
func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestImport_FolderBasic(t *testing.T) {
	s := newImportTestService(t)
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, src, "a.md", "# A")
	writeTestFile(t, src, "sub/b.md", "# B")
	writeTestFile(t, src, "notes.txt", "not markdown") // 应跳过
	writeTestFile(t, src, "c.markdown", "# C")

	res, err := s.ImportMarkdownFolder(src, dst, ImportOptions{IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 3 {
		t.Errorf("Imported = %d, want 3", res.Imported)
	}
	if res.Skipped != 0 || res.Renamed != 0 {
		t.Errorf("unexpected stats: %+v", res)
	}
	// 内容验证
	data, err := os.ReadFile(filepath.Join(dst, "a.md"))
	if err != nil || string(data) != "# A" {
		t.Errorf("a.md not imported correctly: %v %q", err, data)
	}
	data, err = os.ReadFile(filepath.Join(dst, "sub", "b.md"))
	if err != nil || string(data) != "# B" {
		t.Errorf("sub/b.md not imported correctly: %v %q", err, data)
	}
	data, err = os.ReadFile(filepath.Join(dst, "c.markdown"))
	if err != nil || string(data) != "# C" {
		t.Errorf("c.markdown not imported: %v %q", err, data)
	}
}

func TestImport_FolderFlat(t *testing.T) {
	s := newImportTestService(t)
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, src, "sub/a.md", "# A")
	writeTestFile(t, src, "sub/deep/b.md", "# B")

	res, err := s.ImportMarkdownFolder(src, dst, ImportOptions{IncludeSubdirs: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 2 {
		t.Errorf("Imported = %d, want 2", res.Imported)
	}
	if _, err := os.Stat(filepath.Join(dst, "a.md")); err != nil {
		t.Errorf("flat import a.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "b.md")); err != nil {
		t.Errorf("flat import b.md missing: %v", err)
	}
}

func TestImport_FolderConflictSkip(t *testing.T) {
	s := newImportTestService(t)
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, src, "a.md", "# NEW")
	writeTestFile(t, dst, "a.md", "# OLD")

	res, err := s.ImportMarkdownFolder(src, dst, ImportOptions{ConflictStrategy: "skip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 0 || res.Skipped != 1 {
		t.Errorf("skip stats = imported:%d skipped:%d, want 0/1", res.Imported, res.Skipped)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "a.md"))
	if string(data) != "# OLD" {
		t.Errorf("conflict skip should keep OLD, got %q", data)
	}
}

func TestImport_FolderConflictRename(t *testing.T) {
	s := newImportTestService(t)
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, src, "a.md", "# NEW")
	writeTestFile(t, src, "b.md", "# B")
	writeTestFile(t, dst, "a.md", "# OLD")
	writeTestFile(t, dst, "b.md", "# B-OLD")

	res, err := s.ImportMarkdownFolder(src, dst, ImportOptions{ConflictStrategy: "rename"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Renamed != 2 || res.Imported != 2 {
		t.Errorf("rename stats = imported:%d renamed:%d, want 2/2", res.Imported, res.Renamed)
	}
	if _, err := os.Stat(filepath.Join(dst, "a (1).md")); err != nil {
		t.Errorf("renamed a (1).md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "b (1).md")); err != nil {
		t.Errorf("renamed b (1).md missing: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "a (1).md"))
	if string(data) != "# NEW" {
		t.Errorf("renamed content wrong: %q", data)
	}
}

func TestImport_FolderConflictOverwrite(t *testing.T) {
	s := newImportTestService(t)
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, src, "a.md", "# NEW")
	writeTestFile(t, dst, "a.md", "# OLD")

	res, err := s.ImportMarkdownFolder(src, dst, ImportOptions{ConflictStrategy: "overwrite"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("Imported = %d, want 1", res.Imported)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "a.md"))
	if string(data) != "# NEW" {
		t.Errorf("overwrite should be NEW, got %q", data)
	}
}

func TestImport_FolderMissingSource(t *testing.T) {
	s := newImportTestService(t)
	_, err := s.ImportMarkdownFolder(filepath.Join(t.TempDir(), "nope"), t.TempDir(), ImportOptions{})
	if !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("expected core.ErrNotFound, got %v", err)
	}
}

func TestImport_FolderNotDir(t *testing.T) {
	s := newImportTestService(t)
	file := filepath.Join(t.TempDir(), "f.md")
	if err := os.WriteFile(file, []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := s.ImportMarkdownFolder(file, t.TempDir(), ImportOptions{})
	if !core.IsCode(err, core.ErrIsDirectory) {
		t.Errorf("expected core.ErrIsDirectory, got %v", err)
	}
}

func TestImport_PathTraversal(t *testing.T) {
	s := newImportTestService(t)
	dst := t.TempDir()
	// 构造 zip 包含 ../evil.md（ZipSlip 攻击）
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil.md")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("# evil"))
	zw.Close()
	f.Close()

	res, err := s.ImportZip(zipPath, dst, ImportOptions{IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 0 || len(res.Errors) != 1 {
		t.Errorf("traversal stats = imported:%d errors:%d, want 0/1", res.Imported, len(res.Errors))
	}
	// evil.md 不应出现在 dst 之外
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "evil.md")); err == nil {
		t.Error("path traversal wrote file outside workspace!")
	}
}

func TestImport_ZipBasic(t *testing.T) {
	s := newImportTestService(t)
	dst := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "notes.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, item := range []struct{ name, content string }{
		{"a.md", "# A"},
		{"sub/b.md", "# B"},
		{"readme.txt", "ignore"},
		{"sub/deep/c.markdown", "# C"},
	} {
		w, err := zw.Create(item.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(item.content))
	}
	zw.Close()
	f.Close()

	res, err := s.ImportZip(zipPath, dst, ImportOptions{IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 3 {
		t.Errorf("Imported = %d, want 3", res.Imported)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "deep", "c.markdown")); err != nil {
		t.Errorf("nested zip file missing: %v", err)
	}
}

func TestImport_ZipFlatConflict(t *testing.T) {
	s := newImportTestService(t)
	dst := t.TempDir()
	writeTestFile(t, dst, "a.md", "# OLD")
	zipPath := filepath.Join(t.TempDir(), "notes.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("a.md")
	w.Write([]byte("# NEW"))
	zw.Close()
	f.Close()

	res, err := s.ImportZip(zipPath, dst, ImportOptions{ConflictStrategy: "rename", IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 1 || res.Renamed != 1 {
		t.Errorf("stats = imported:%d renamed:%d, want 1/1", res.Imported, res.Renamed)
	}
	data, _ := os.ReadFile(filepath.Join(dst, "a (1).md"))
	if string(data) != "# NEW" {
		t.Errorf("renamed zip content wrong: %q", data)
	}
}

func TestImport_ZipMissing(t *testing.T) {
	s := newImportTestService(t)
	_, err := s.ImportZip(filepath.Join(t.TempDir(), "nope.zip"), t.TempDir(), ImportOptions{})
	if !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("expected core.ErrNotFound, got %v", err)
	}
}

func TestImport_InvalidInput(t *testing.T) {
	s := newImportTestService(t)
	if _, err := s.ImportMarkdownFolder("", t.TempDir(), ImportOptions{}); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("empty src should be core.ErrInvalidInput, got %v", err)
	}
	if _, err := s.ImportMarkdownFolder(t.TempDir(), "", ImportOptions{}); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("empty dst should be core.ErrInvalidInput, got %v", err)
	}
	if _, err := s.ImportZip("", t.TempDir(), ImportOptions{}); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("empty zip should be core.ErrInvalidInput, got %v", err)
	}
}

// TestImport_DuplicateInSameBatch 验证同批次导入时大小写冲突（仅 Windows 文件系统不区分大小写）
// 在大小写敏感的文件系统（Linux/macOS APFS 默认）上跳过，因为 a.md 与 A.md 视为不同文件无冲突。
func TestImport_DuplicateInSameBatch(t *testing.T) {
	if !isCaseInsensitiveFS() {
		t.Skip("skipping on case-sensitive filesystem (a.md and A.md are distinct files)")
	}
	s := newImportTestService(t)
	dst := t.TempDir()
	// 用 zip 构造两个独立条目 a.md / A.md（zip 内不受宿主 FS 大小写限制）
	zipPath := filepath.Join(t.TempDir(), "dup.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, item := range []struct{ name, content string }{
		{"a.md", "# A1"},
		{"A.md", "# A2"}, // 第二个导入时 os.Stat(dst/A.md) 命中已写入的 dst/a.md
	} {
		w, err := zw.Create(item.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(item.content))
	}
	zw.Close()
	f.Close()

	res, err := s.ImportZip(zipPath, dst, ImportOptions{ConflictStrategy: "rename", IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Imported != 2 {
		t.Errorf("Imported = %d, want 2 (one renamed)", res.Imported)
	}
	if res.Renamed != 1 {
		t.Errorf("Renamed = %d, want 1 for case-conflict", res.Renamed)
	}
	// 至少一个文件带序号
	entries, _ := os.ReadDir(dst)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), " (") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a renamed file with suffix, got %v", entries)
	}
}

// isCaseInsensitiveFS 判断当前文件系统是否大小写不敏感。
// 通过创建文件后用不同大小写名再 Stat 验证。
func isCaseInsensitiveFS() bool {
	dir, err := os.MkdirTemp("", "caseprobe-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	lower := filepath.Join(dir, "probe.md")
	if err := os.WriteFile(lower, []byte("x"), 0644); err != nil {
		return false
	}
	upper := filepath.Join(dir, "PROBE.md")
	_, err = os.Stat(upper)
	return err == nil // 大小写不敏感时 PROBE.md 命中 probe.md
}

// ---------------------------------------------------------------------------
// E-5 异步导入
// ---------------------------------------------------------------------------

// TestImportAsync_ImportsFiles 异步导入的最终结果与同步版一致。
// 这条锁住的是"异步只是调度方式变了，导入逻辑本身没变"。
func TestImportAsync_ImportsFiles(t *testing.T) {
	tasks := NewTaskService(nil)
	s := NewImportServiceWithTasks(tasks)

	src := t.TempDir()
	ws := t.TempDir()
	writeTestFile(t, src, "a.md", "# A")
	writeTestFile(t, src, "sub/b.md", "# B")

	id, err := s.ImportMarkdownFolderAsync(src, ws, ImportOptions{IncludeSubdirs: true})
	if err != nil {
		t.Fatalf("ImportMarkdownFolderAsync: %v", err)
	}
	if id == "" {
		t.Fatal("expected a task ID")
	}
	tasks.Wait()

	info := tasks.GetTask(id)
	if info == nil || info.Status != TaskSucceeded {
		t.Fatalf("expected task to succeed, got %+v", info)
	}
	for _, rel := range []string{"a.md", filepath.Join("sub", "b.md")} {
		if _, err := os.Stat(filepath.Join(ws, rel)); err != nil {
			t.Errorf("expected %s to be imported: %v", rel, err)
		}
	}
}

// TestImportAsync_ValidatesSynchronously 参数错误必须在提交任务前就返回。
// 若等到任务里才发现，用户点完导入什么都没发生，几秒后才冒出一个错误弹窗。
func TestImportAsync_ValidatesSynchronously(t *testing.T) {
	s := NewImportServiceWithTasks(NewTaskService(nil))

	if _, err := s.ImportMarkdownFolderAsync("", t.TempDir(), ImportOptions{}); err == nil {
		t.Error("empty source dir should be rejected synchronously")
	}
	if _, err := s.ImportMarkdownFolderAsync(t.TempDir(), "", ImportOptions{}); err == nil {
		t.Error("empty workspace should be rejected synchronously")
	}
}

// TestImportAsync_RequiresTaskFramework 没接任务框架时，异步导入应当明确报错，
// 而不是静默退化成同步执行（那会让调用方误以为已经异步了）。
func TestImportAsync_RequiresTaskFramework(t *testing.T) {
	s := NewImportService()
	if _, err := s.ImportMarkdownFolderAsync(t.TempDir(), t.TempDir(), ImportOptions{}); err == nil {
		t.Error("async import without a task service should fail")
	}
}

// TestImportAsync_CancelStopsEarly 取消后不应开始导入。
// 这是异步导入相对同步导入的核心收益：用户能中途叫停。
//
// 用占位任务占住唯一并发名额来固定取消时机，而不是 sleep 一段时间再取消：
// 后者依赖「导入还没跑完」这个假设，而 300 个小文件在 Linux 上几十毫秒就导完了，
// 在 Windows 上却慢得多——同一个 sleep 在两边结论完全不同（CI 上实测必挂）。
func TestImportAsync_CancelStopsEarly(t *testing.T) {
	tasks := NewTaskServiceWithOptions(nil, 1, 10)
	s := NewImportServiceWithTasks(tasks)

	src := t.TempDir()
	ws := t.TempDir()
	for i := 0; i < 300; i++ {
		writeTestFile(t, src, fmt.Sprintf("note%03d.md", i), "# note")
	}

	// 占位任务：拿到唯一的并发名额并卡住，导入任务因此停在 pending
	started := make(chan struct{})
	release := make(chan struct{})
	tasks.submit(context.Background(), "occupier", func(_ context.Context, _ TaskReporter) error {
		close(started)
		<-release
		return nil
	})
	<-started

	id, err := s.ImportMarkdownFolderAsync(src, ws, ImportOptions{})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// 排队阶段取消：taskService 在拿到名额前后各检查一次 ctx，
	// 排队中被取消的任务不会被执行
	if !tasks.Cancel(id) {
		t.Fatal("cancel should be accepted while the task is queued")
	}
	close(release)
	tasks.Wait()

	info := tasks.GetTask(id)
	if info.Status != TaskCancelled {
		t.Fatalf("expected cancelled, got %s (err=%s)", info.Status, info.Error)
	}

	// 核心不变量：取消生效后一个文件都不该被导入
	imported := 0
	_ = filepath.WalkDir(ws, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			imported++
		}
		return nil
	})
	if imported != 0 {
		t.Errorf("cancelled before start, but %d files were imported", imported)
	}
}

// TestImportAsync_ReportsProgress 进度必须推进到 100%：
// 前端进度条依赖这个数字，停在 50% 会让用户以为卡住了。
func TestImportAsync_ReportsProgress(t *testing.T) {
	em := &recordingEmitter{}
	tasks := NewTaskService(em)
	s := NewImportServiceWithTasks(tasks)

	src := t.TempDir()
	ws := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTestFile(t, src, fmt.Sprintf("n%02d.md", i), "# n")
	}

	id, err := s.ImportMarkdownFolderAsync(src, ws, ImportOptions{})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	tasks.Wait()

	finished := em.last(EventTaskFinished)
	if finished == nil {
		t.Fatal("no finished event")
	}
	if finished.Percent != 100 {
		t.Errorf("finished task should report 100%%, got %d", finished.Percent)
	}
	if finished.Message == "" {
		t.Error("finished task should carry a summary message for the user")
	}
	if tasks.GetTask(id).Status != TaskSucceeded {
		t.Error("task should succeed")
	}
}
