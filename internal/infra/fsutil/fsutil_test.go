package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWrite_CreatesFileAndParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deep", "index.json")

	if err := AtomicWrite(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWrite_OverwritesExistingFile(t *testing.T) {
	// Windows 上 rename 覆盖已存在文件依赖 MoveFileEx(REPLACE_EXISTING)，
	// 这条断言就是防止将来有人把实现换成 os.Link/裸 Rename 语义。
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := AtomicWrite(path, []byte("new"), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

// TestAtomicWrite_LeavesNoTempFile 锁住「不留 .tmp 残留」这条契约：
// 搜索摘要的测试会断言目录里没有 .tmp，索引目录也不该被脏文件污染。
func TestAtomicWrite_LeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")

	if err := AtomicWrite(path, []byte("x"), 0644); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly 1 file in dir, got %d", len(entries))
	}
}

func TestAtomicWrite_CleansUpTempOnRenameFailure(t *testing.T) {
	// 让 rename 必然失败：目标路径是一个非空目录。
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0750); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	err := AtomicWrite(target, []byte("x"), 0644)
	if err == nil {
		t.Skip("platform allowed renaming a file over a non-empty directory; nothing to assert")
	}

	if _, statErr := os.Stat(target + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("temp file should be removed after a failed rename (stat err = %v)", statErr)
	}
}

func TestAtomicWrite_AppliesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不按 Unix 位应用文件权限")
	}
	path := filepath.Join(t.TempDir(), "secret.json")

	if err := AtomicWrite(path, []byte("x"), 0600); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %04o, want 0600", perm)
	}
}
