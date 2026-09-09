package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplatesExposePurposeAndRenderOnlyNoteMetadata(t *testing.T) {
	ws := t.TempDir()
	writeTemplate(t, ws, "团队决策", "---\r\ntemplate-category: 协作\r\ntemplate-description: 记录决策和依据\r\ntemplate-destination: Projects/{{project}}/{{title}}.md\r\ntags: [决策]\r\nowner: 团队\r\n---\r\n\r\n# {{title}}\r\n项目：{{project}}\r\n")
	svc := NewTemplateService(NewFileService())
	templates, err := svc.ListTemplates(ws)
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	for _, tpl := range templates {
		if tpl.Name == "团队决策" {
			data, _ := json.Marshal(tpl)
			if err := json.Unmarshal(data, &metadata); err != nil {
				t.Fatal(err)
			}
			if strings.Join(tpl.Variables, ",") != "project" || tpl.Builtin {
				t.Fatalf("custom variables/source lost: %+v", tpl)
			}
		}
	}
	for key, want := range map[string]string{"category": "协作", "description": "记录决策和依据", "destination": "Projects/{{project}}/{{title}}.md"} {
		if metadata[key] != want {
			t.Errorf("%s = %v, want %s", key, metadata[key], want)
		}
	}
	if _, err := svc.CreateFromTemplate(ws, "团队决策", "Projects/NoteVault/索引方案.md", map[string]string{"project": "NoteVault"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(ws, "Projects/NoteVault/索引方案.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\r\ntags: [决策]\r\nowner: 团队\r\n---\r\n\r\n# 索引方案\r\n项目：NoteVault\r\n"
	if string(data) != want {
		t.Fatalf("template hints must not leak into note metadata; user metadata/newlines must survive:\n%q", data)
	}
}

func TestWorkflowTemplatesIntegrateWithWorkbench(t *testing.T) {
	ws := t.TempDir()
	svc := NewTemplateService(NewFileService())
	for _, item := range []struct {
		name, target string
		vars         map[string]string
	}{
		{"项目概览", "Projects/索引优化/project.md", map[string]string{"project": "索引优化"}},
		{"项目任务", "Projects/索引优化/Tasks.md", map[string]string{"project": "索引优化"}},
		{"面试卡片", "Learning/面试宝典/数据库索引.md", map[string]string{"question": "联合索引为什么有最左前缀规则？"}},
		{"工作报告", "Daily/Reports/2026-09-09-工作日报.md", nil},
		{"技术设计", "Projects/索引优化/技术设计.md", map[string]string{"project": "索引优化"}},
		{"排障记录", "Projects/索引优化/排障记录.md", map[string]string{"project": "索引优化"}},
		{"技术笔记", "Learning/索引原理.md", nil},
	} {
		if _, err := svc.CreateFromTemplate(ws, item.name, item.target, item.vars); err != nil {
			t.Fatalf("create %s: %v", item.name, err)
		}
		content, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(item.target)))
		if err != nil {
			t.Fatal(err)
		}
		if variablePattern.Match(content) {
			t.Errorf("%s has unfilled template variables: %s", item.name, content)
		}
	}
	snapshot, err := NewTodoService().GetWorkbench(ws, "2026-09-09")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Projects) != 1 || snapshot.Projects[0].Name != "索引优化" {
		t.Errorf("project template not recognized: %+v", snapshot.Projects)
	}
	if len(snapshot.Cards) != 1 || snapshot.Cards[0].Question != "联合索引为什么有最左前缀规则？" {
		t.Errorf("interview template not recognized: %+v", snapshot.Cards)
	}
	if len(snapshot.Tasks) != 0 {
		t.Errorf("blank templates must not invent actionable tasks: %+v", snapshot.Tasks)
	}
}

func TestTemplateServiceRejectsUnsafeNamesAndLinks(t *testing.T) {
	svc := NewTemplateService(NewFileService())
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "CON", "test:stream", "bad?name", "tail.", "tail ", "bad\x00name"} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.GetTemplateContent(t.TempDir(), name); err == nil || !strings.Contains(err.Error(), "模板名不合法") {
				t.Fatalf("unsafe name must fail validation: %q, %v", name, err)
			}
		})
	}
	t.Run("template directory link", func(t *testing.T) {
		ws, outside := t.TempDir(), t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "外部.md"), []byte("# private"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(ws, "Templates")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := svc.ListTemplates(ws); err == nil {
			t.Fatal("linked Templates must not expose outside notes")
		}
		if _, err := svc.GetTemplateContent(ws, "外部"); err == nil {
			t.Fatal("linked template must not be readable")
		}
	})
	t.Run("target directory link", func(t *testing.T) {
		ws, outside := t.TempDir(), t.TempDir()
		if err := os.Symlink(outside, filepath.Join(ws, "Projects")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		writeTemplate(t, ws, "自定义", "# {{title}}")
		if _, err := svc.CreateFromTemplate(ws, "自定义", "Projects/外部.md", nil); err == nil {
			t.Fatal("template creation must not write through links")
		}
		if _, err := os.Stat(filepath.Join(outside, "外部.md")); !os.IsNotExist(err) {
			t.Fatalf("outside directory changed: %v", err)
		}
	})
}
