package service

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

func TestFileOperationsRenameRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{
		"", ".", "..", "../escape.md", `..\escape.md`, "child/new.md", `child\new.md`,
		"/absolute.md", `C:\absolute.md`, "note.md:stream", "bad<name.md", "bad>name.md",
		`bad"name.md`, "bad|name.md", "bad?name.md", "bad*name.md", "trailing.", "trailing ",
		" ", "control\x00.md", "control\x1f.md", "control\x7f.md", "control\u0085.md",
		"line\u2028.md", "line\u2029.md", "invalid\xff.md", "CON", "con.md", "CON .md",
		"PRN.data", "aux", "NUL", "CLOCK$", "CONIN$", "CONOUT$", "COM1", "COM9.txt",
		"LPT1", "LPT9.txt", "COM¹.md", "LPT²", "LPT³",
	} {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			s, workspace := createTestFileService(t)
			if err := os.MkdirAll(filepath.Join(workspace, "notes", "child"), 0750); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(workspace, "notes", "original.md")
			if err := os.WriteFile(source, []byte("original note"), 0644); err != nil {
				t.Fatal(err)
			}

			node, err := s.RenameFile(workspace, "notes/original.md", name)
			if !core.IsCode(err, core.ErrInvalidInput) || node != nil {
				t.Errorf("unsafe leaf name should return INVALID_INPUT without a node: node=%+v err=%v", node, err)
			}
			assertFileOperationContent(t, source, "original note")
		})
	}
}

func TestFileOperationsRenameRejectsUnsafeSourcePaths(t *testing.T) {
	for _, relative := range []string{"", ".", "..", "notes/..", "notes/../original.md", "../outside.md", `..\outside.md`, "notes./original.md", "notes /original.md"} {
		t.Run(strconv.Quote(relative), func(t *testing.T) {
			sandbox := t.TempDir()
			workspace := filepath.Join(sandbox, "workspace")
			if err := os.MkdirAll(filepath.Join(workspace, "notes"), 0750); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(workspace, "original.md"), filepath.Join(sandbox, "outside.md"), filepath.Join(workspace, "notes", "original.md")} {
				if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			node, err := NewFileService().RenameFile(workspace, relative, "renamed.md")
			if !core.IsCode(err, core.ErrInvalidInput) || node != nil {
				t.Errorf("unsafe source path should return INVALID_INPUT: node=%+v err=%v", node, err)
			}
			assertFileOperationContent(t, filepath.Join(workspace, "original.md"), "keep")
			assertFileOperationContent(t, filepath.Join(workspace, "notes", "original.md"), "keep")
			assertFileOperationContent(t, filepath.Join(sandbox, "outside.md"), "keep")
		})
	}
}

func TestFileOperationsRenameRejectsExistingDestination(t *testing.T) {
	for _, targetIsDir := range []bool{false, true} {
		t.Run(strconv.FormatBool(targetIsDir), func(t *testing.T) {
			s, workspace := createTestFileService(t)
			source := filepath.Join(workspace, "original")
			target := filepath.Join(workspace, "existing")
			if targetIsDir {
				if err := os.Mkdir(source, 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "note.md"), []byte("source content"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(target, 0750); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(source, []byte("source content"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, []byte("target content"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			node, err := s.RenameFile(workspace, "original", "existing")
			if !core.IsCode(err, core.ErrAlreadyExists) || !errors.Is(err, os.ErrExist) || node != nil {
				t.Errorf("collision should return ALREADY_EXISTS with its cause: node=%+v err=%v", node, err)
			}
			if targetIsDir {
				assertFileOperationContent(t, filepath.Join(source, "note.md"), "source content")
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 0 {
					t.Fatalf("existing destination directory must stay empty: entries=%v err=%v", entries, err)
				}
			} else {
				assertFileOperationContent(t, source, "source content")
				assertFileOperationContent(t, target, "target content")
			}
		})
	}
}

func TestFileOperationsRenamePreservesFolderDescendants(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "Projects", "Old", "nested"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "Projects", "Old", "nested", "note.md"), []byte("nested note"), 0644); err != nil {
		t.Fatal(err)
	}

	node, err := s.RenameFile(workspace, "Projects/Old", "新项目")
	if err != nil {
		t.Fatal(err)
	}
	if !node.IsDir || node.Name != "新项目" || node.Path != "Projects/新项目" || node.FullPath != filepath.Join(workspace, "Projects", "新项目") {
		t.Fatalf("incorrect renamed folder node: %+v", node)
	}
	assertFileOperationContent(t, filepath.Join(workspace, "Projects", "新项目", "nested", "note.md"), "nested note")
	if _, err := os.Stat(filepath.Join(workspace, "Projects", "Old")); !os.IsNotExist(err) {
		t.Fatalf("old folder should be gone: %v", err)
	}
}

func TestFileOperationsRenameSamePathAndCaseOnly(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(strconv.FormatBool(directory), func(t *testing.T) {
			s, workspace := createTestFileService(t)
			source := filepath.Join(workspace, "note.md")
			if directory {
				if err := os.Mkdir(source, 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, "nested.md"), []byte("keep"), 0644); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(source, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"note.md", "NOTE.MD"} {
				node, err := s.RenameFile(workspace, "note.md", name)
				if err != nil {
					t.Fatalf("same-path or case-only rename failed: %v", err)
				}
				if node.Path != name || node.Name != name || node.IsDir != directory {
					t.Fatalf("incorrect node: %+v", node)
				}
			}
			entries, err := os.ReadDir(workspace)
			if err != nil || len(entries) != 1 || entries[0].Name() != "NOTE.MD" {
				t.Fatalf("case-only rename must update the actual directory entry: entries=%v err=%v", entries, err)
			}
			after, err := os.Stat(filepath.Join(workspace, "NOTE.MD"))
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("rename must preserve the original file identity: %v", err)
			}
			contentPath := filepath.Join(workspace, "NOTE.MD")
			if directory {
				contentPath = filepath.Join(contentPath, "nested.md")
			}
			assertFileOperationContent(t, contentPath, "keep")
		})
	}
}

func TestFileOperationsRenameAcceptsPortableNoteNames(t *testing.T) {
	for _, name := range []string{"中文笔记.md", "C++ [基础] #1.md", "COM10.md", "LPT10.md", ".notes.md"} {
		t.Run(name, func(t *testing.T) {
			s, workspace := createTestFileService(t)
			if err := os.WriteFile(filepath.Join(workspace, "old.md"), []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RenameFile(workspace, "old.md", name); err != nil {
				t.Fatal(err)
			}
			assertFileOperationContent(t, filepath.Join(workspace, name), "keep")
		})
	}
}

func TestFileOperationsRenameMissingSource(t *testing.T) {
	s, workspace := createTestFileService(t)
	node, err := s.RenameFile(workspace, "missing.md", "renamed.md")
	if !core.IsCode(err, core.ErrNotFound) || !errors.Is(err, os.ErrNotExist) || node != nil {
		t.Fatalf("missing source should return NOT_FOUND with its cause: node=%+v err=%v", node, err)
	}
}

func TestFileOperationsCreateFolderRejectsUnsafePaths(t *testing.T) {
	for _, relative := range []string{
		"", ".", "nested/..", "nested/../escaped", "../escaped", `..\escaped`, "/absolute", `\absolute`,
		"nested/NUL", "nested/con.txt", "nested/COM¹", "nested/LPT²", "nested/CON .txt", "nested/trailing.",
		"nested/trailing ", "nested/bad:name", "nested/bad|name", "nested/control\x7f", "nested/invalid\xff",
	} {
		t.Run(strconv.Quote(relative), func(t *testing.T) {
			s, workspace := createTestFileService(t)
			node, err := s.CreateFolder(workspace, relative)
			if !core.IsCode(err, core.ErrInvalidInput) || node != nil {
				t.Errorf("unsafe folder path should return INVALID_INPUT: node=%+v err=%v", node, err)
			}
			entries, err := os.ReadDir(workspace)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid folder path must not create partial parent directories: entries=%v err=%v", entries, err)
			}
		})
	}
}

func TestFileOperationsCreateFolderNormalizesNestedPaths(t *testing.T) {
	for _, relative := range []string{"notes/books", `notes\books`, "notes//./books/"} {
		t.Run(relative, func(t *testing.T) {
			s, workspace := createTestFileService(t)
			node, err := s.CreateFolder(workspace, relative)
			if err != nil {
				t.Fatal(err)
			}
			if !node.IsDir || node.Name != "books" || node.Path != "notes/books" || node.FullPath != filepath.Join(workspace, "notes", "books") {
				t.Fatalf("nested folder should return a canonical node: %+v", node)
			}
			info, err := os.Stat(filepath.Join(workspace, "notes", "books"))
			if err != nil || !info.IsDir() {
				t.Fatalf("nested folder was not created: %v", err)
			}
		})
	}
}

func TestFileOperationsCreateFolderKeepsExistingDirectory(t *testing.T) {
	s, workspace := createTestFileService(t)
	if err := os.MkdirAll(filepath.Join(workspace, "notes", "books"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "notes", "books", "note.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFolder(workspace, "notes/books"); err != nil {
		t.Fatalf("creating an existing folder should remain idempotent: %v", err)
	}
	assertFileOperationContent(t, filepath.Join(workspace, "notes", "books", "note.md"), "keep")
}

func TestFileOperationsCreateFolderRejectsFileCollision(t *testing.T) {
	s, workspace := createTestFileService(t)
	file := filepath.Join(workspace, "existing")
	if err := os.WriteFile(file, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateFolder(workspace, "existing")
	if !core.IsCode(err, core.ErrAlreadyExists) || !errors.Is(err, os.ErrExist) || node != nil {
		t.Errorf("file collision should return ALREADY_EXISTS with its cause: node=%+v err=%v", node, err)
	}
	assertFileOperationContent(t, file, "keep")
	if _, err := s.CreateFolder(workspace, "existing/nested"); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("a file cannot be a folder's parent: %v", err)
	}
	assertFileOperationContent(t, file, "keep")
}

func TestFileOperationsRejectSymlinkPaths(t *testing.T) {
	for _, tc := range []struct {
		name       string
		linkName   string
		linkToFile bool
		renameFrom string
		renameTo   string
		folder     string
	}{
		{name: "rename linked source", linkName: "linked.md", linkToFile: true, renameFrom: "linked.md", renameTo: "renamed.md"},
		{name: "rename through linked parent", linkName: "linked", renameFrom: "linked/note.md", renameTo: "renamed.md"},
		{name: "rename over linked destination", linkName: "linked.md", linkToFile: true, renameFrom: "note.md", renameTo: "linked.md"},
		{name: "create linked folder", linkName: "linked", folder: "linked"},
		{name: "create through linked parent", linkName: "linked", folder: "linked/new"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, workspace := createTestFileService(t)
			outside := t.TempDir()
			outsideNote := filepath.Join(outside, "note.md")
			if err := os.WriteFile(outsideNote, []byte("outside"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "note.md"), []byte("inside"), 0644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(workspace, tc.linkName)
			target := outside
			if tc.linkToFile {
				target = outsideNote
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var node *FileNode
			var err error
			if tc.folder != "" {
				node, err = s.CreateFolder(workspace, tc.folder)
			} else {
				node, err = s.RenameFile(workspace, tc.renameFrom, tc.renameTo)
			}
			if !core.IsCode(err, core.ErrInvalidInput) || node != nil {
				t.Errorf("symlink path should return INVALID_INPUT: node=%+v err=%v", node, err)
			}
			assertFileOperationContent(t, outsideNote, "outside")
			assertFileOperationContent(t, filepath.Join(workspace, "note.md"), "inside")
			info, err := os.Lstat(link)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("original symlink should be unchanged: %v", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "note.md" {
				t.Errorf("outside directory should be unchanged: entries=%v err=%v", entries, err)
			}
		})
	}
}

func assertFileOperationContent(t *testing.T, filename, want string) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil || string(data) != want {
		t.Errorf("file content changed: path=%s got=%q want=%q err=%v", filename, data, want, err)
	}
}
