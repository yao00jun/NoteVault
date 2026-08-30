package service

import (
	"archive/zip"
	"github.com/notevault/notevault/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
