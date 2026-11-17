package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchProjectContextDeliverablesAndRetrospective(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/alpha/project.md", `---
name: Alpha
status: 进行中
phase: 执行
coreQuestion: 先做哪个功能？
background: 用户需要更快的工作台。
nextStep: 完成交付物面板
techStack: [Go, Vue]
---

# Alpha
`)
	workbenchWrite(t, root, "Projects/alpha/Deliverables/方案.md", `---
deliverableType: report
stage: final
version: v2
---

# 方案
`)
	workbenchWrite(t, root, "Projects/alpha/复盘.md", "# 复盘\n")

	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-11-17")
	if len(snapshot.Projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(snapshot.Projects))
	}
	project := snapshot.Projects[0]
	if project.Phase != "执行" || project.CoreQuestion != "先做哪个功能？" || project.Background != "用户需要更快的工作台。" {
		t.Fatalf("unexpected project context: %+v", project)
	}
	if len(project.Deliverables) != 1 {
		t.Fatalf("deliverables = %d, want 1", len(project.Deliverables))
	}
	deliverable := project.Deliverables[0]
	if deliverable.Title != "方案" || deliverable.Type != "report" || deliverable.Stage != "final" || deliverable.Version != "v2" {
		t.Fatalf("unexpected deliverable: %+v", deliverable)
	}
	if project.Retrospective != "Projects/alpha/复盘.md" {
		t.Fatalf("retrospective = %q, want Projects/alpha/复盘.md", project.Retrospective)
	}
	for _, note := range project.Notes {
		if note.Path == "Projects/alpha/复盘.md" || note.Path == "Projects/alpha/Deliverables/方案.md" {
			t.Fatalf("deliverable or retrospective leaked into notes: %s", note.Path)
		}
	}
}

func TestApplyWorkspacePresetIsIdempotent(t *testing.T) {
	root := t.TempDir()
	service := NewWorkspaceService()

	if err := service.ApplyWorkspacePreset(root, "content"); err != nil {
		t.Fatal(err)
	}
	projectDoc := filepath.Join(root, "Projects", "内容创作-示例项目", "project.md")
	first, err := os.ReadFile(projectDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "phase: 规划") || !strings.Contains(string(first), "## 核心问题") {
		t.Fatalf("preset project is missing context fields: %s", first)
	}
	if _, err := os.Stat(filepath.Join(root, "Projects", "内容创作-示例项目", "Deliverables")); err != nil {
		t.Fatalf("deliverables directory missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Resources", "选题素材")); err != nil {
		t.Fatalf("preset resource directory missing: %v", err)
	}

	// 第二次应用不得覆盖用户已修改的文件。
	workbenchWrite(t, root, "Projects/内容创作-示例项目/project.md", "custom")
	if err := service.ApplyWorkspacePreset(root, "content"); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(projectDoc)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != "custom" {
		t.Fatalf("preset overwrote an existing file: %q", again)
	}

	if err := service.ApplyWorkspacePreset(root, "nope"); err == nil {
		t.Fatal("unknown preset id must return an error")
	}
	if presets := service.ListWorkspacePresets(); len(presets) != 3 {
		t.Fatalf("presets = %d, want 3", len(presets))
	}
}
