package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

func createTestFileService(t *testing.T) (*FileService, string) {
	t.Helper()
	tmpDir := t.TempDir()
	return &FileService{}, tmpDir
}

func TestNewFileService(t *testing.T) {
	service := NewFileService()
	if service == nil {
		t.Fatal("NewFileService returned nil")
	}
}

func TestCreateFile(t *testing.T) {
	service, wsPath := createTestFileService(t)

	node, err := service.CreateFile(wsPath, "test.md", "# Hello\n\nWorld")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}
	if node == nil {
		t.Fatal("CreateFile returned nil node")
	}
	if node.Name != "test.md" {
		t.Errorf("expected name 'test.md', got '%s'", node.Name)
	}
	if node.Path != "test.md" {
		t.Errorf("expected path 'test.md', got '%s'", node.Path)
	}
	if node.IsDir {
		t.Error("expected IsDir to be false")
	}
	if node.Size != int64(len("# Hello\n\nWorld")) {
		t.Errorf("expected size %d, got %d", len("# Hello\n\nWorld"), node.Size)
	}

	// 验证文件实际存在
	fullPath := filepath.Join(wsPath, "test.md")
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Error("file should exist on disk")
	}

	// 验证文件内容
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "# Hello\n\nWorld" {
		t.Errorf("expected content '# Hello\\n\\nWorld', got '%s'", string(content))
	}
}

func TestCreateFileInSubdirectory(t *testing.T) {
	service, wsPath := createTestFileService(t)

	node, err := service.CreateFile(wsPath, "docs/guide/readme.md", "# Guide")
	if err != nil {
		t.Fatalf("CreateFile in subdir failed: %v", err)
	}
	if node.Path != "docs/guide/readme.md" {
		t.Errorf("expected path 'docs/guide/readme.md', got '%s'", node.Path)
	}

	// 验证父目录自动创建
	fullPath := filepath.Join(wsPath, "docs", "guide", "readme.md")
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Error("file in subdirectory should exist")
	}
}

func TestCreateFileAlreadyExists(t *testing.T) {
	service, wsPath := createTestFileService(t)

	_, err := service.CreateFile(wsPath, "test.md", "content1")
	if err != nil {
		t.Fatalf("first CreateFile failed: %v", err)
	}

	// 第二次创建同名文件应返回错误
	_, err = service.CreateFile(wsPath, "test.md", "content2")
	if err == nil {
		t.Fatal("expected error when creating existing file, got nil")
	}
	// 契约：错误码为 ErrAlreadyExists（前端据此做差异化处理）
	if !core.IsCode(err, core.ErrAlreadyExists) {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
	// 兼容性：原始 os 错误保留在 Cause 链上，errors.Is 仍可解析
	// （注意 os.IsExist 不再适用：它只解包 *PathError，不认 NVError）
	if !errors.Is(err, os.ErrExist) {
		t.Errorf("cause chain should still resolve to os.ErrExist, got %v", err)
	}
}

func TestReadFile(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 先创建文件
	_, err := service.CreateFile(wsPath, "test.md", "# Test Content")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 读取文件
	content, err := service.ReadFile(wsPath, "test.md")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if content != "# Test Content" {
		t.Errorf("expected '# Test Content', got '%s'", content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	service, wsPath := createTestFileService(t)

	_, err := service.ReadFile(wsPath, "nonexistent.md")
	if err == nil {
		t.Fatal("expected error when reading nonexistent file, got nil")
	}
	// 契约：错误码为 ErrNotFound
	if !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	// 兼容性：Cause 链仍可用 errors.Is 解析到底层 os 错误
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cause chain should still resolve to os.ErrNotExist, got %v", err)
	}
}

func TestSaveFile(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 先创建文件
	_, err := service.CreateFile(wsPath, "test.md", "original")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 保存新内容（覆盖）
	err = service.SaveFile(wsPath, "test.md", "updated content")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// 验证内容已更新
	content, err := service.ReadFile(wsPath, "test.md")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if content != "updated content" {
		t.Errorf("expected 'updated content', got '%s'", content)
	}
}

func TestSaveFileCreatesParentDirs(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 保存到不存在的子目录，应自动创建
	err := service.SaveFile(wsPath, "a/b/c/test.md", "nested")
	if err != nil {
		t.Fatalf("SaveFile in nested dir failed: %v", err)
	}

	fullPath := filepath.Join(wsPath, "a", "b", "c", "test.md")
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read nested file: %v", err)
	}
	if string(content) != "nested" {
		t.Errorf("expected 'nested', got '%s'", string(content))
	}
}

func TestDeleteFile(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 先创建文件
	_, err := service.CreateFile(wsPath, "test.md", "content")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 删除文件
	err = service.DeleteFile(wsPath, "test.md")
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// 验证文件已删除
	fullPath := filepath.Join(wsPath, "test.md")
	if _, err := os.Stat(fullPath); !os.IsNotExist(err) {
		t.Error("file should not exist after deletion")
	}
}

func TestDeleteFileNotFound(t *testing.T) {
	service, wsPath := createTestFileService(t)

	err := service.DeleteFile(wsPath, "nonexistent.md")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent file, got nil")
	}
}

func TestRenameFile(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 先创建文件
	_, err := service.CreateFile(wsPath, "old.md", "content")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	// 重命名文件
	node, err := service.RenameFile(wsPath, "old.md", "new.md")
	if err != nil {
		t.Fatalf("RenameFile failed: %v", err)
	}
	if node.Name != "new.md" {
		t.Errorf("expected name 'new.md', got '%s'", node.Name)
	}
	if node.Path != "new.md" {
		t.Errorf("expected path 'new.md', got '%s'", node.Path)
	}

	// 验证旧文件不存在，新文件存在
	oldPath := filepath.Join(wsPath, "old.md")
	newPath := filepath.Join(wsPath, "new.md")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old file should not exist after rename")
	}
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("new file should exist after rename")
	}
}

func TestRenameFileInSubdirectory(t *testing.T) {
	service, wsPath := createTestFileService(t)

	_, err := service.CreateFile(wsPath, "docs/old.md", "content")
	if err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	node, err := service.RenameFile(wsPath, "docs/old.md", "new.md")
	if err != nil {
		t.Fatalf("RenameFile failed: %v", err)
	}
	if node.Path != "docs/new.md" {
		t.Errorf("expected path 'docs/new.md', got '%s'", node.Path)
	}
}

func TestCreateFolder(t *testing.T) {
	service, wsPath := createTestFileService(t)

	node, err := service.CreateFolder(wsPath, "myfolder")
	if err != nil {
		t.Fatalf("CreateFolder failed: %v", err)
	}
	if node.Name != "myfolder" {
		t.Errorf("expected name 'myfolder', got '%s'", node.Name)
	}
	if !node.IsDir {
		t.Error("expected IsDir to be true")
	}

	// 验证文件夹实际存在
	fullPath := filepath.Join(wsPath, "myfolder")
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("folder should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory, got file")
	}
}

func TestCreateNestedFolder(t *testing.T) {
	service, wsPath := createTestFileService(t)

	node, err := service.CreateFolder(wsPath, "a/b/c")
	if err != nil {
		t.Fatalf("CreateFolder nested failed: %v", err)
	}
	if node.Path != "a/b/c" {
		t.Errorf("expected path 'a/b/c', got '%s'", node.Path)
	}

	fullPath := filepath.Join(wsPath, "a", "b", "c")
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		t.Error("nested folder should exist")
	}
}

func TestGetFileTree(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 创建测试文件结构
	service.CreateFolder(wsPath, "docs")
	service.CreateFile(wsPath, "readme.md", "# Readme")
	service.CreateFile(wsPath, "docs/guide.md", "# Guide")
	service.CreateFile(wsPath, "docs/api.md", "# API")

	tree, err := service.GetFileTree(wsPath)
	if err != nil {
		t.Fatalf("GetFileTree failed: %v", err)
	}

	// 根目录应该有 2 个节点：docs 文件夹 + readme.md
	if len(tree) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(tree))
	}

	// 第一个应该是文件夹（排序：文件夹在前）
	if !tree[0].IsDir {
		t.Error("expected first node to be directory (folders first)")
	}
	if tree[0].Name != "docs" {
		t.Errorf("expected folder 'docs', got '%s'", tree[0].Name)
	}

	// docs 文件夹应该有 2 个子文件
	if tree[0].Children == nil || len(tree[0].Children) != 2 {
		t.Fatalf("expected 2 children in docs, got %v", tree[0].Children)
	}

	// 第二个应该是 readme.md
	if tree[1].IsDir {
		t.Error("expected second node to be file")
	}
	if tree[1].Name != "readme.md" {
		t.Errorf("expected file 'readme.md', got '%s'", tree[1].Name)
	}
}

func TestGetFileTreeEmpty(t *testing.T) {
	service, wsPath := createTestFileService(t)

	tree, err := service.GetFileTree(wsPath)
	if err != nil {
		t.Fatalf("GetFileTree failed: %v", err)
	}
	if len(tree) != 0 {
		t.Errorf("expected empty tree, got %d nodes", len(tree))
	}
}

func TestGetFileTreeIgnoresHiddenFiles(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 创建隐藏文件和普通文件
	os.WriteFile(filepath.Join(wsPath, ".hidden.md"), []byte("hidden"), 0644)
	os.WriteFile(filepath.Join(wsPath, ".git"), []byte("git"), 0644)
	service.CreateFile(wsPath, "visible.md", "visible")

	tree, err := service.GetFileTree(wsPath)
	if err != nil {
		t.Fatalf("GetFileTree failed: %v", err)
	}

	// 应该只有 visible.md
	if len(tree) != 1 {
		t.Fatalf("expected 1 node (hidden files ignored), got %d", len(tree))
	}
	if tree[0].Name != "visible.md" {
		t.Errorf("expected 'visible.md', got '%s'", tree[0].Name)
	}
}

func TestGetFileTreeOnlyMarkdownFiles(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 创建不同类型的文件
	service.CreateFile(wsPath, "doc.md", "markdown")
	service.CreateFile(wsPath, "note.markdown", "markdown2")
	os.WriteFile(filepath.Join(wsPath, "image.png"), []byte("png"), 0644)
	os.WriteFile(filepath.Join(wsPath, "data.txt"), []byte("txt"), 0644)

	tree, err := service.GetFileTree(wsPath)
	if err != nil {
		t.Fatalf("GetFileTree failed: %v", err)
	}

	// 应该只有 .md 和 .markdown 文件
	if len(tree) != 2 {
		t.Fatalf("expected 2 markdown files, got %d", len(tree))
	}
	for _, node := range tree {
		ext := strings.ToLower(filepath.Ext(node.Name))
		if ext != ".md" && ext != ".markdown" {
			t.Errorf("unexpected file type: %s", node.Name)
		}
	}
}

func TestGetFileTreeSorting(t *testing.T) {
	service, wsPath := createTestFileService(t)

	// 创建乱序的文件和文件夹
	service.CreateFile(wsPath, "zebra.md", "z")
	service.CreateFile(wsPath, "apple.md", "a")
	service.CreateFolder(wsPath, "zfolder")
	service.CreateFolder(wsPath, "afolder")

	tree, err := service.GetFileTree(wsPath)
	if err != nil {
		t.Fatalf("GetFileTree failed: %v", err)
	}

	// 文件夹应该在前，且按名称排序
	if len(tree) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(tree))
	}
	if !tree[0].IsDir || tree[0].Name != "afolder" {
		t.Errorf("expected first node 'afolder' (dir), got '%s'", tree[0].Name)
	}
	if !tree[1].IsDir || tree[1].Name != "zfolder" {
		t.Errorf("expected second node 'zfolder' (dir), got '%s'", tree[1].Name)
	}
	if tree[2].IsDir || tree[2].Name != "apple.md" {
		t.Errorf("expected third node 'apple.md' (file), got '%s'", tree[2].Name)
	}
	if tree[3].IsDir || tree[3].Name != "zebra.md" {
		t.Errorf("expected fourth node 'zebra.md' (file), got '%s'", tree[3].Name)
	}
}

func TestSaveImage(t *testing.T) {
	service, wsPath := createTestFileService(t)

	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG 魔数

	relPath, err := service.SaveImage(wsPath, "screenshot.png", imageData)
	if err != nil {
		t.Fatalf("SaveImage failed: %v", err)
	}

	// 验证返回路径格式
	if !strings.HasPrefix(relPath, "assets/") {
		t.Errorf("expected path to start with 'assets/', got '%s'", relPath)
	}
	if !strings.HasSuffix(relPath, ".png") {
		t.Errorf("expected path to end with '.png', got '%s'", relPath)
	}

	// 验证文件实际存在
	fullPath := filepath.Join(wsPath, relPath)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read saved image: %v", err)
	}
	if len(data) != len(imageData) {
		t.Errorf("expected %d bytes, got %d", len(imageData), len(data))
	}
}

func TestSaveImageGeneratesUniqueNames(t *testing.T) {
	service, wsPath := createTestFileService(t)

	imageData := []byte{0x89, 0x50, 0x4E, 0x47}

	path1, err := service.SaveImage(wsPath, "image.png", imageData)
	if err != nil {
		t.Fatalf("first SaveImage failed: %v", err)
	}

	path2, err := service.SaveImage(wsPath, "image.png", imageData)
	if err != nil {
		t.Fatalf("second SaveImage failed: %v", err)
	}

	// 两个路径应该不同（因为时间戳）
	if path1 == path2 {
		t.Error("expected unique paths for same filename, got same path")
	}
}
