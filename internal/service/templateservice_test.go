package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemplate(t *testing.T, workspace, name, content string) {
	t.Helper()
	dir := filepath.Join(workspace, "Templates")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTemplateService_ListTemplates(t *testing.T) {
	t.Run("目录不存在返回空列表", func(t *testing.T) {
		svc := NewTemplateService(NewFileService())
		list, err := svc.ListTemplates(t.TempDir())
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("应返回空列表, got %v", list)
		}
	})

	t.Run("列出 md 模板并解析自定义变量", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", "# {{title}}\n日期 {{date}}\n项目 {{project}}\n参与人 {{people}}\n")
		writeTemplate(t, ws, "读书笔记", "书名 {{book}}\n评分 {{book}}\n")
		// 非 md 文件与子目录不应出现
		dir := filepath.Join(ws, "Templates")
		os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644)
		os.MkdirAll(filepath.Join(dir, "子目录"), 0o750)

		svc := NewTemplateService(NewFileService())
		list, err := svc.ListTemplates(ws)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("应只有 2 个模板, got %d", len(list))
		}
		if list[0].Name != "会议" || list[1].Name != "读书笔记" {
			t.Fatalf("应按名称排序: %v", list)
		}
		// date 是内置变量不应出现；people/project 应出现且排序
		if strings.Join(list[0].Variables, ",") != "people,project" {
			t.Fatalf("会议模板变量应为 people,project, got %v", list[0].Variables)
		}
		// 重复变量去重
		if len(list[1].Variables) != 1 || list[1].Variables[0] != "book" {
			t.Fatalf("读书笔记变量应只有 book, got %v", list[1].Variables)
		}
	})
}

func TestTemplateService_GetTemplateContent(t *testing.T) {
	t.Run("返回模板原文", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "t", "# {{title}}")
		svc := NewTemplateService(NewFileService())
		content, err := svc.GetTemplateContent(ws, "t")
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if content != "# {{title}}" {
			t.Fatalf("内容不符: %q", content)
		}
	})

	t.Run("模板名含路径分隔符被拒绝", func(t *testing.T) {
		svc := NewTemplateService(NewFileService())
		if _, err := svc.GetTemplateContent(t.TempDir(), "../secret"); err == nil {
			t.Fatal("路径穿越应被拒绝")
		}
		if _, err := svc.GetTemplateContent(t.TempDir(), `a\b`); err == nil {
			t.Fatal("反斜杠路径应被拒绝")
		}
	})

	t.Run("不存在的模板报错", func(t *testing.T) {
		svc := NewTemplateService(NewFileService())
		if _, err := svc.GetTemplateContent(t.TempDir(), "不存在"); err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestTemplateService_CreateFromTemplate(t *testing.T) {
	const tpl = "# {{title}}\n\n- 日期：{{date}}\n- 项目：{{project}}\n- 未提供：{{missing}}\n"

	newSvc := func() *TemplateService {
		return NewTemplateService(NewFileService())
	}

	t.Run("渲染内置与自定义变量并创建文件", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", tpl)
		svc := newSvc()

		node, err := svc.CreateFromTemplate(ws, "会议", "Daily/周会.md", map[string]string{"project": "NoteVault"})
		if err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		if node == nil || node.Path != "Daily/周会.md" {
			t.Fatalf("应返回创建的节点, got %+v", node)
		}
		data, err := os.ReadFile(filepath.Join(ws, "Daily", "周会.md"))
		if err != nil {
			t.Fatalf("文件应已落盘: %v", err)
		}
		text := string(data)
		if !strings.Contains(text, "# 周会") {
			t.Fatalf("title 应渲染为文件名, got:\n%s", text)
		}
		if !strings.Contains(text, "项目：NoteVault") {
			t.Fatalf("自定义变量应被替换, got:\n%s", text)
		}
		// date 内置变量渲染为 YYYY-MM-DD
		if !strings.Contains(text, "- 日期：20") {
			t.Fatalf("date 应渲染为日期, got:\n%s", text)
		}
		// 未提供值的变量保留占位符
		if !strings.Contains(text, "{{missing}}") {
			t.Fatalf("未提供变量应保留占位符, got:\n%s", text)
		}
	})

	t.Run("目标路径不能为空", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", tpl)
		if _, err := newSvc().CreateFromTemplate(ws, "会议", "  ", nil); err == nil {
			t.Fatal("空目标应报错")
		}
	})

	t.Run("目标已存在时报错", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", tpl)
		svc := newSvc()
		if _, err := svc.CreateFromTemplate(ws, "会议", "a.md", nil); err != nil {
			t.Fatalf("首次创建应成功: %v", err)
		}
		if _, err := svc.CreateFromTemplate(ws, "会议", "a.md", nil); err == nil {
			t.Fatal("重复创建应报错")
		}
	})

	t.Run("目标路径穿越工作区被拒绝", func(t *testing.T) {
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", tpl)
		_, err := newSvc().CreateFromTemplate(ws, "会议", "../outside.md", nil)
		if err == nil {
			t.Fatal("路径穿越应被 FileService 拒绝")
		}
	})

	t.Run("模板名路径穿越被拒绝", func(t *testing.T) {
		ws := t.TempDir()
		if _, err := newSvc().CreateFromTemplate(ws, "../../etc/passwd", "a.md", nil); err == nil {
			t.Fatal("模板名穿越应被拒绝")
		}
	})

	t.Run("nil 文件服务时明确报错", func(t *testing.T) {
		svc := NewTemplateService(nil)
		ws := t.TempDir()
		writeTemplate(t, ws, "会议", tpl)
		if _, err := svc.CreateFromTemplate(ws, "会议", "a.md", nil); err == nil {
			t.Fatal("nil 文件服务应报错")
		}
	})
}

func TestExtractVariables(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"无占位符", ""},
		{"{{title}} {{date}}", ""},                     // 全内置
		{"{{project}} 与 {{project}}", "project"},       // 去重
		{"{{b}} {{a}} {{a-b}} {{c_d}}", "a,a-b,b,c_d"}, // 排序 + 连字符/下划线
		{"{{ 1number}} {{}} {{中文}}", ""},               // 非字母开头不匹配
	}
	for _, c := range cases {
		got := extractVariables(c.content)
		if strings.Join(got, ",") != c.want {
			t.Errorf("extractVariables(%q) = %v, want [%s]", c.content, got, c.want)
		}
	}
}
