package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

func TestMoveEntryAcrossFolders(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "Inbox", "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "Projects"), 0750); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(workspace, "Inbox", "note.md")
	if err := os.WriteFile(source, []byte("move me"), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := s.MoveEntry(workspace, "Inbox/note.md", "Projects/note.md")
	if err != nil {
		t.Fatalf("MoveEntry failed: %v", err)
	}
	if node.Path != "Projects/note.md" || node.IsDir {
		t.Errorf("unexpected node: %+v", node)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Errorf("source should be gone after move, stat err=%v", err)
	}
	assertFileOperationContent(t, filepath.Join(workspace, "Projects", "note.md"), "move me")
}

func TestMoveEntryDirectory(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "Inbox", "sub"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "Archive"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "Inbox", "sub", "a.md"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := s.MoveEntry(workspace, "Inbox/sub", "Archive/sub")
	if err != nil {
		t.Fatalf("MoveEntry directory failed: %v", err)
	}
	if !node.IsDir {
		t.Errorf("moved node should be a dir: %+v", node)
	}
	assertFileOperationContent(t, filepath.Join(workspace, "Archive", "sub", "a.md"), "a")
}

func TestMoveEntryRejectsUnsafePaths(t *testing.T) {
	cases := []struct {
		name       string
		oldRel     string
		newRel     string
		setupFiles bool
	}{
		{"escape target", "Inbox/note.md", "../outside.md", true},
		{"absolute target", "Inbox/note.md", "/tmp/evil.md", true},
		{"same path", "Inbox/note.md", "Inbox/note.md", true},
		{"empty target", "Inbox/note.md", "  ", true},
		{"missing source", "Inbox/ghost.md", "Projects/ghost.md", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, workspace := createTestFileService(t)
			if err := os.MkdirAll(filepath.Join(workspace, "Inbox"), 0750); err != nil {
				t.Fatal(err)
			}
			if tc.setupFiles {
				if err := os.WriteFile(filepath.Join(workspace, "Inbox", "note.md"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			node, err := s.MoveEntry(workspace, tc.oldRel, tc.newRel)
			if err == nil || node != nil {
				t.Errorf("expected error without node, got node=%+v err=%v", node, err)
			}
		})
	}
}

func TestMoveEntryRejectsDirIntoItself(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "Inbox", "sub", "deeper"), 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveEntry(workspace, "Inbox/sub", "Inbox/sub/deeper/sub"); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("moving a dir into its own subtree should be INVALID_INPUT, got %v", err)
	}
}

func TestMoveEntryRejectsExistingDestination(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "Inbox"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "Inbox", "note.md"), []byte("src"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "existing.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveEntry(workspace, "Inbox/note.md", "existing.md"); !core.IsCode(err, core.ErrAlreadyExists) {
		t.Errorf("existing destination should be ALREADY_EXISTS, got %v", err)
	}
	assertFileOperationContent(t, filepath.Join(workspace, "existing.md"), "keep")
	assertFileOperationContent(t, filepath.Join(workspace, "Inbox", "note.md"), "src")
}
