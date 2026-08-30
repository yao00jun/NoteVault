package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTrash_MoveToTrash(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note.md"), "# Note\ncontent")

	s := NewTrashService()
	trashed, err := s.MoveToTrash(dir, "note.md")
	if err != nil {
		t.Fatalf("MoveToTrash failed: %v", err)
	}
	if trashed == nil {
		t.Fatal("returned nil TrashedFile")
	}
	if trashed.Name != "note.md" {
		t.Fatalf("expected name 'note.md', got '%s'", trashed.Name)
	}
	if trashed.OriginalPath != "note.md" {
		t.Fatalf("expected originalPath 'note.md', got '%s'", trashed.OriginalPath)
	}
	// 原文件应不存在
	if _, err := os.Stat(filepath.Join(dir, "note.md")); !os.IsNotExist(err) {
		t.Fatal("original file should be removed after trash")
	}
	// 回收站索引应存在
	files, err := s.GetTrashedFiles(dir)
	if err != nil {
		t.Fatalf("GetTrashedFiles failed: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 trashed file, got %d", len(files))
	}
}

func TestTrash_RestoreFromTrash(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "# Doc\nhello")

	s := NewTrashService()
	trashed, err := s.MoveToTrash(dir, "doc.md")
	if err != nil {
		t.Fatalf("MoveToTrash failed: %v", err)
	}

	// 恢复
	if err := s.RestoreFromTrash(dir, trashed.ID); err != nil {
		t.Fatalf("RestoreFromTrash failed: %v", err)
	}
	// 原文件应恢复
	data, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatalf("restored file should exist: %v", err)
	}
	if string(data) != "# Doc\nhello" {
		t.Fatalf("content mismatch after restore: %s", string(data))
	}
	// 回收站应为空
	files, _ := s.GetTrashedFiles(dir)
	if len(files) != 0 {
		t.Fatalf("expected 0 trashed after restore, got %d", len(files))
	}
}

func TestTrash_RestoreNonExist(t *testing.T) {
	dir := t.TempDir()
	s := NewTrashService()
	err := s.RestoreFromTrash(dir, "nonexistent_id")
	if err == nil {
		t.Fatal("expected error for non-existent id")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestTrash_PermanentlyDelete(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "temp.md"), "temp content")

	s := NewTrashService()
	trashed, _ := s.MoveToTrash(dir, "temp.md")

	if err := s.PermanentlyDelete(dir, trashed.ID); err != nil {
		t.Fatalf("PermanentlyDelete failed: %v", err)
	}
	// 回收站应为空
	files, _ := s.GetTrashedFiles(dir)
	if len(files) != 0 {
		t.Fatalf("expected 0 after permanent delete, got %d", len(files))
	}
}

func TestTrash_EmptyTrash(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "a")
	mustWrite(t, filepath.Join(dir, "b.md"), "b")

	s := NewTrashService()
	s.MoveToTrash(dir, "a.md")
	s.MoveToTrash(dir, "b.md")

	files, _ := s.GetTrashedFiles(dir)
	if len(files) != 2 {
		t.Fatalf("expected 2 trashed, got %d", len(files))
	}

	if err := s.EmptyTrash(dir); err != nil {
		t.Fatalf("EmptyTrash failed: %v", err)
	}

	files, _ = s.GetTrashedFiles(dir)
	if len(files) != 0 {
		t.Fatalf("expected 0 after empty, got %d", len(files))
	}
}

func TestTrash_EmptyTrashRemoveFailure(t *testing.T) {
	// Windows（尤其沙箱环境）对非空目录的 os.Remove 行为不稳定（delete-pending），
	// 该分支由 Linux CI 覆盖（POSIX rmdir 对非空目录必失败）
	if runtime.GOOS == "windows" {
		t.Skip("non-empty dir removal unreliable on Windows; covered by Linux CI")
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "a")

	s := NewTrashService()
	f, err := s.MoveToTrash(dir, "a.md")
	if err != nil {
		t.Fatalf("MoveToTrash failed: %v", err)
	}

	// 将回收站中的文件替换为非空目录，使 os.Remove 失败
	target := filepath.Join(dir, ".trash", f.Path)
	if err := os.Remove(target); err != nil {
		t.Fatalf("remove trashed file failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0750); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	if err := s.EmptyTrash(dir); err == nil {
		t.Fatal("expected error when removing trashed dir fails, got nil")
	}
}

func TestTrash_GetTrashStats(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "12345")
	mustWrite(t, filepath.Join(dir, "b.md"), "6789")

	s := NewTrashService()
	s.MoveToTrash(dir, "a.md")
	s.MoveToTrash(dir, "b.md")

	stats, err := s.GetTrashStats(dir)
	if err != nil {
		t.Fatalf("GetTrashStats failed: %v", err)
	}
	if stats["count"] != 2 {
		t.Fatalf("expected count=2, got %d", stats["count"])
	}
	if stats["size"] != 9 {
		t.Fatalf("expected size=9, got %d", stats["size"])
	}
}

func TestTrash_EmptyWorkspace(t *testing.T) {
	dir := t.TempDir()
	s := NewTrashService()
	files, err := s.GetTrashedFiles(dir)
	if err != nil {
		t.Fatalf("GetTrashedFiles on empty workspace failed: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
}
