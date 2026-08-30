package service

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestExportService_ExportWorkspaceMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "A.md"), "# A")
	writeFile(t, filepath.Join(dir, "sub", "B.md"), "# B")
	writeFile(t, filepath.Join(dir, "skip.txt"), "not md")

	dest := filepath.Join(dir, "export.zip")
	svc := NewExportService()
	if err := svc.ExportWorkspaceMarkdown(dir, dest); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := map[string]bool{}
	for _, f := range r.File {
		names[f.Name] = true
	}
	if !names["A.md"] || !names["sub/B.md"] {
		t.Errorf("expected A.md and sub/B.md in zip, got %v", names)
	}
	if names["skip.txt"] {
		t.Errorf("non-md file should not be exported")
	}
}

func TestExportService_ExportWorkspaceMarkdown_AppendsZipExt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "A.md"), "# A")
	dest := filepath.Join(dir, "export")
	svc := NewExportService()
	if err := svc.ExportWorkspaceMarkdown(dir, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest + ".zip"); err != nil {
		t.Errorf("expected .zip extension to be appended: %v", err)
	}
}

func TestExportService_ExportNoteMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "A.md"), "# Hello")
	dest := filepath.Join(dir, "A-copy.md")
	svc := NewExportService()
	if err := svc.ExportNoteMarkdown(dir, "A.md", dest); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "# Hello" {
		t.Errorf("content mismatch: %s", data)
	}
}

func TestExportService_SaveText(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.html")
	svc := NewExportService()
	if err := svc.SaveText(dest, "<h1>Hi</h1>"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "<h1>Hi</h1>" {
		t.Errorf("content mismatch: %s", data)
	}
}

func TestExportService_ExportWorkspaceMarkdown_Empty(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "x")
	dest := filepath.Join(dir, "e.zip")
	svc := NewExportService()
	if err := svc.ExportWorkspaceMarkdown(dir, dest); err == nil {
		t.Errorf("expected error for empty workspace (no md files)")
	}
}
