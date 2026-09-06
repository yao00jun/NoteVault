package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureWorkspaceScaffold_CreatesAll 验证空目录上一次跑齐全部约定目录与引导文件。
func TestEnsureWorkspaceScaffold_CreatesAll(t *testing.T) {
	root := t.TempDir()

	if err := EnsureWorkspaceScaffold(root); err != nil {
		t.Fatalf("EnsureWorkspaceScaffold failed: %v", err)
	}

	for _, dir := range scaffoldDirs {
		info, err := os.Stat(filepath.Join(root, dir))
		if err != nil || !info.IsDir() {
			t.Errorf("directory %s should exist after scaffold", dir)
		}
	}

	for rel := range scaffoldFiles {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("file %s should exist after scaffold: %v", rel, err)
		}
	}
}

// TestEnsureWorkspaceScaffold_IdempotentNeverOverwrites 铁律验证：
// 重复执行幂等；用户改写过的引导文件绝不被覆盖。
func TestEnsureWorkspaceScaffold_IdempotentNeverOverwrites(t *testing.T) {
	root := t.TempDir()

	if err := EnsureWorkspaceScaffold(root); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	// 模拟用户改写引导文件
	welcome := filepath.Join(root, "开始使用.md")
	userContent := "# 我的自定义欢迎页"
	if err := os.WriteFile(welcome, []byte(userContent), 0o644); err != nil {
		t.Fatalf("write user content failed: %v", err)
	}
	// 用户删掉一个目录，重跑应补回
	if err := os.RemoveAll(filepath.Join(root, "Resources")); err != nil {
		t.Fatalf("remove dir failed: %v", err)
	}

	if err := EnsureWorkspaceScaffold(root); err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	got, err := os.ReadFile(welcome)
	if err != nil {
		t.Fatalf("read welcome failed: %v", err)
	}
	if string(got) != userContent {
		t.Errorf("user content must never be overwritten, got: %q", string(got))
	}
	if _, err := os.Stat(filepath.Join(root, "Resources")); err != nil {
		t.Errorf("removed directory should be re-created on rerun")
	}
}

// TestEnsureWorkspaceScaffold_EmptyPath 空路径必须报错而不是 mkdir 到盘根。
func TestEnsureWorkspaceScaffold_EmptyPath(t *testing.T) {
	if err := EnsureWorkspaceScaffold(""); err == nil {
		t.Fatal("empty path should return error")
	} else if !strings.Contains(err.Error(), "路径为空") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCreateWorkspace_RunsScaffold 建区后脚手架应已落地（真实 CreateWorkspace 链路）。
func TestCreateWorkspace_RunsScaffold(t *testing.T) {
	// 隔离 configDir，避免污染真实用户的工作区列表
	svc := &WorkspaceService{configDir: filepath.Join(t.TempDir(), "config")}
	tmp := t.TempDir()
	wsPath := filepath.Join(tmp, "vault")

	if _, err := svc.CreateWorkspace("脚手架测试区", wsPath); err != nil {
		t.Fatalf("CreateWorkspace failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wsPath, "Inbox", "使用引导.md")); err != nil {
		t.Errorf("Inbox guide should exist after CreateWorkspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsPath, "Templates", "会议记录.md")); err != nil {
		t.Errorf("meeting template should exist after CreateWorkspace: %v", err)
	}
}
