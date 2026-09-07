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

// TestEnsureWorkspaceScaffold_MigratesLegacyFolders 遗留目录迁移（2026-09-07 用户指令）：
// 根目录游离的主题目录（Java、SQL）收进 Learning/；保留名、点前缀目录与文件不动；
// Learning 下已有同名目录时绝不合并/覆盖；重跑幂等。
func TestEnsureWorkspaceScaffold_MigratesLegacyFolders(t *testing.T) {
	root := t.TempDir()
	mkdir := func(rel string) {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o750); err != nil {
			t.Fatalf("mkdir %s failed: %v", rel, err)
		}
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s failed: %v", rel, err)
		}
	}

	// 根目录的遗留主题目录 + 应当原地不动的条目
	mkdir("Java")
	mkdir("SQL")
	write("Java/基础.md", "# Java")
	mkdir("Learning")
	mkdir("Learning/SQL") // 目标已存在 → 不得覆盖
	mkdir(".trash")
	mkdir("assets")
	write("根目录文件.md", "# keep")

	if err := EnsureWorkspaceScaffold(root); err != nil {
		t.Fatalf("EnsureWorkspaceScaffold failed: %v", err)
	}

	// Java 整体迁入 Learning/Java，原位置消失
	if _, err := os.Stat(filepath.Join(root, "Learning", "Java", "基础.md")); err != nil {
		t.Errorf("Java should be migrated into Learning/Java with content: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Java")); !os.IsNotExist(err) {
		t.Errorf("root Java should be gone after migration, stat err: %v", err)
	}
	// SQL：Learning/SQL 已存在 → 跳过，根目录 SQL 保留原样
	if _, err := os.Stat(filepath.Join(root, "SQL")); err != nil {
		t.Errorf("root SQL must stay when Learning/SQL already exists: %v", err)
	}
	// 保留名与系统目录原地不动
	for _, keep := range []string{"assets", ".trash"} {
		if info, err := os.Stat(filepath.Join(root, keep)); err != nil || !info.IsDir() {
			t.Errorf("%s must stay at root", keep)
		}
	}
	// 文件不动
	if _, err := os.Stat(filepath.Join(root, "根目录文件.md")); err != nil {
		t.Errorf("root files must never be touched: %v", err)
	}

	// 幂等：重跑不报错、不再有新动作
	if err := EnsureWorkspaceScaffold(root); err != nil {
		t.Fatalf("rerun failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Learning", "Java", "基础.md")); err != nil {
		t.Errorf("migrated content must survive rerun: %v", err)
	}
}
