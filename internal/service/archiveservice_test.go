package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchive_ArchiveFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "note.md"), "# Note\ncontent")

	s := NewArchiveService()
	archived, err := s.ArchiveFile(dir, "note.md")
	if err != nil {
		t.Fatalf("ArchiveFile failed: %v", err)
	}
	if archived == nil {
		t.Fatal("returned nil ArchivedFile")
	}
	if archived.Name != "note.md" {
		t.Fatalf("expected name 'note.md', got '%s'", archived.Name)
	}
	// 原文件应不存在
	if _, err := os.Stat(filepath.Join(dir, "note.md")); !os.IsNotExist(err) {
		t.Fatal("original file should be removed after archive")
	}
	// 归档目录中应存在
	if !s.IsArchived(dir, "note.md") {
		t.Fatal("file should be marked as archived")
	}
}

func TestArchive_ArchiveInSubdir(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	os.MkdirAll(subdir, 0755)
	mustWrite(t, filepath.Join(subdir, "deep.md"), "deep content")

	s := NewArchiveService()
	_, err := s.ArchiveFile(dir, filepath.Join("sub", "deep.md"))
	if err != nil {
		t.Fatalf("ArchiveFile in subdir failed: %v", err)
	}
	// 验证归档文件保留目录结构
	archivedFiles, err := s.GetArchivedFiles(dir)
	if err != nil {
		t.Fatalf("GetArchivedFiles failed: %v", err)
	}
	if len(archivedFiles) != 1 {
		t.Fatalf("expected 1 archived file, got %d", len(archivedFiles))
	}
}

func TestArchive_UnarchiveFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "# Doc\nhello")

	s := NewArchiveService()
	if _, err := s.ArchiveFile(dir, "doc.md"); err != nil {
		t.Fatalf("ArchiveFile failed: %v", err)
	}

	// 取消归档
	if err := s.UnarchiveFile(dir, "doc.md"); err != nil {
		t.Fatalf("UnarchiveFile failed: %v", err)
	}
	// 原文件应恢复
	data, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatalf("restored file should exist: %v", err)
	}
	if string(data) != "# Doc\nhello" {
		t.Fatalf("content mismatch: %s", string(data))
	}
	// 不再标记为已归档
	if s.IsArchived(dir, "doc.md") {
		t.Fatal("file should not be archived after unarchive")
	}
}

func TestArchive_GetArchivedFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "a")
	mustWrite(t, filepath.Join(dir, "b.md"), "b")

	s := NewArchiveService()
	s.ArchiveFile(dir, "a.md")
	s.ArchiveFile(dir, "b.md")

	files, err := s.GetArchivedFiles(dir)
	if err != nil {
		t.Fatalf("GetArchivedFiles failed: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 archived files, got %d", len(files))
	}
}

func TestArchive_EmptyArchive(t *testing.T) {
	dir := t.TempDir()
	s := NewArchiveService()
	files, err := s.GetArchivedFiles(dir)
	if err != nil {
		t.Fatalf("GetArchivedFiles on empty workspace failed: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(files))
	}
	if s.IsArchived(dir, "nonexistent.md") {
		t.Fatal("nonexistent file should not be archived")
	}
}

func TestArchive_UnarchiveNonExist(t *testing.T) {
	dir := t.TempDir()
	s := NewArchiveService()
	err := s.UnarchiveFile(dir, "nonexistent.md")
	if err == nil {
		t.Fatal("expected error for unarchiving non-existent file")
	}
}
