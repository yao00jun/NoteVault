package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workbenchWrite(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func workbenchRead(t *testing.T, root, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWorkbenchTodoMetadataPreservesLegacySyntax(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/商城/Tasks.md", "# Tasks\n- [ ] [DTS] 修复重试 #priority/low #project/物流 #due/2026-09-10 #date/2026-09-08 (卡点: 缺环境)\n  * [X] !!旧格式 #done/2026-09-07\n")
	todos, err := NewTodoService().GetAllTodos(root)
	if err != nil || len(todos) != 2 {
		t.Fatalf("read legacy and typed tasks: len=%d, err=%v", len(todos), err)
	}
	data, err := json.Marshal(todos)
	if err != nil {
		t.Fatal(err)
	}
	var actual []map[string]any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"title": "修复重试", "type": "DTS", "project": "物流", "due": "2026-09-10",
		"date": "2026-09-08", "priority": "low", "blocker": "缺环境", "status": "blocked",
		"lineIndex": float64(1), "id": "Projects/商城/Tasks.md:1",
		"sourceLine": "- [ ] [DTS] 修复重试 #priority/low #project/物流 #due/2026-09-10 #date/2026-09-08 (卡点: 缺环境)",
	}
	for key, expected := range want {
		if actual[0][key] != expected {
			t.Errorf("task %s: got %#v, want %#v", key, actual[0][key], expected)
		}
	}
	if actual[1]["priority"] != "high" || actual[1]["content"] != "旧格式" || actual[1]["completedAt"] != "2026-09-07" {
		t.Errorf("legacy task metadata: %#v", actual[1])
	}
}

func TestWorkbenchTodoIgnoresMarkdownExamples(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Daily/2026-09-07.md", "---\nexample:\n  - [ ] YAML sample\n---\n# Today\n```markdown\n- [ ] fenced example\n````\n~~~markdown\n* [ ] tilde sample\n~~~\n<!--\n- [ ] commented sample\n-->\n- [x] shipped\n- [ ] \n")
	workbenchWrite(t, root, "Templates/Daily.md", "- [ ] Template placeholder\n")
	workbenchWrite(t, root, ".notevault/private.md", "- [ ] Internal data\n")
	todos, err := NewTodoService().GetAllTodos(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(todos) != 1 || todos[0].Content != "shipped" {
		t.Fatalf("only the real task belongs on the workbench: %#v", todos)
	}
}

func TestWorkbenchDocumentsExcludeTemplatesWithoutHidingRealNotes(t *testing.T) {
	for _, directory := range []string{"Templates", "templates", "TEMPLATES"} {
		t.Run(directory, func(t *testing.T) {
			root := t.TempDir()
			const templateContent = "# {{title}}\n\n保留自定义模板正文。\n"
			workbenchWrite(t, root, directory+"/自定义验收.md", templateContent)
			workbenchWrite(t, root, directory+"/分组/嵌套模板.md", templateContent)
			want := map[string]bool{
				"Resources/模板使用指南.md":      false,
				"Projects/Templates/设计.md": false,
				"Templates.md":             false,
				"TemplatesArchive/笔记.md":   false,
			}
			for relative := range want {
				workbenchWrite(t, root, relative, "# 模板设计知识\n\n真正的笔记仍应展示。\n")
			}
			snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-09")
			if len(snapshot.Documents) != len(want) {
				t.Fatalf("only real notes belong in document counts: got %+v", snapshot.Documents)
			}
			for _, document := range snapshot.Documents {
				if _, ok := want[document.Path]; !ok {
					t.Fatalf("template leaked into the document list: %s", document.Path)
				}
				want[document.Path] = true
			}
			for relative, found := range want {
				if !found {
					t.Errorf("ordinary note was hidden: %s", relative)
				}
			}
			for _, relative := range []string{"/自定义验收.md", "/分组/嵌套模板.md"} {
				if content := workbenchRead(t, root, directory+relative); content != templateContent {
					t.Fatalf("template contents changed: %s", relative)
				}
			}
			if directory == "Templates" {
				templates := NewTemplateService(nil)
				content, err := templates.GetTemplateContent(root, "自定义验收")
				if err != nil || content != templateContent {
					t.Fatalf("template management must retain access: content=%q err=%v", content, err)
				}
				available, err := templates.ListTemplates(root)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, template := range available {
					found = found || template.Name == "自定义验收"
				}
				if !found {
					t.Fatal("custom template disappeared from the creation chooser")
				}
			}
		})
	}
}

func TestWorkbenchLegacyEmptyTodosSerializeAsArray(t *testing.T) {
	todos, err := NewTodoService().GetAllTodos(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(todos)
	if strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("empty tasks must be an array, got %s", data)
	}
}

func workbenchSnapshot(t *testing.T, service *TodoService, root, date string) *WorkbenchSnapshot {
	t.Helper()
	reader, ok := any(service).(interface {
		GetWorkbench(string, string) (*WorkbenchSnapshot, error)
	})
	if !ok {
		t.Fatal("TodoService must expose the Markdown workbench snapshot")
	}
	snapshot, err := reader.GetWorkbench(root, date)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil {
		t.Fatal("snapshot must never be null")
	}
	return snapshot
}

func TestWorkbenchSnapshotProjectsBooksAndInterviewCards(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/商城/project.md", "---\n名称: 商城重构\n状态: 进行中\n下一步: 联调订单\n关联技术: [Java, Vue3]\n---\n# 商城\n")
	workbenchWrite(t, root, "Projects/商城/Tasks.md", "- [ ] [US] 订单接口 #date/2026-09-08\n- [x] [DTS] 已修复 #done/2026-09-08 (卡点: 旧环境)\n")
	workbenchWrite(t, root, "Projects/商城/设计.md", "# 订单架构\n正文\n")
	workbenchWrite(t, root, "Projects/物流/project.md", "---\nname: 物流中台\nstatus: paused\nnext_step: 验证 MQ\ntech_stack:\n  - Go\n  - Redis\n---\n")
	workbenchWrite(t, root, "Daily/2026-09-07.md", "- [x] Yesterday shipped\n- [ ] 归属商城 #project/商城重构\n")
	workbenchWrite(t, root, "Learning/Java/book.md", "---\n书名: Java 核心进阶\n状态: 在学\n研读进度: 65%\n关联项目: [商城重构, 物流中台]\n---\n")
	workbenchWrite(t, root, "Learning/Java/sources.md", "# 导入来源\n")
	workbenchWrite(t, root, "Learning/Java/ai-plan.md", "# AI 生成学习建议\n")
	workbenchWrite(t, root, "Learning/Java/01-并发.md", "# 并发基础\n\n### [Q042] 为什么死锁？\n<!-- srs: {\"level\":\"不会\",\"interval\":1,\"due\":\"2026-09-08\",\"reps\":2,\"failures\":2} -->\n\n#### 核心要点\n互斥与循环等待。\n```md\n### 假问题\n<!-- srs: {} -->\n```\n\n### [Q043] 如何预防？\n<!-- srs: {\"level\":\"掌握\",\"interval\":7,\"due\":\"2026-09-15\",\"reps\":3} -->\n固定锁顺序。\n\n### 损坏题卡\n<!-- srs: {broken} -->\n")
	workbenchWrite(t, root, "Learning/Go/book.md", "---\ntitle: Go 并发实战\nstatus: settled\nprogress: 100\nprojects: [物流中台]\n---\n")
	workbenchWrite(t, root, "Learning/技术雷达.md", "# 技术雷达\n## 尝试中\n- SQLite 只作索引\n")
	active := time.Date(2030, 9, 8, 14, 0, 0, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "Projects", "商城", "设计.md"), active, active); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Projects) != 2 || len(snapshot.Books) != 2 || len(snapshot.Cards) != 2 || len(snapshot.Tasks) != 4 {
		t.Fatalf("unexpected Markdown projection sizes: projects=%d books=%d cards=%d tasks=%d", len(snapshot.Projects), len(snapshot.Books), len(snapshot.Cards), len(snapshot.Tasks))
	}
	var project *WorkbenchProject
	for i := range snapshot.Projects {
		if snapshot.Projects[i].Name == "商城重构" {
			project = &snapshot.Projects[i]
		}
	}
	if project == nil || project.Status != "进行中" || project.NextStep != "联调订单" || strings.Join(project.TechStack, ",") != "Java,Vue3" || len(project.TaskIDs) != 3 || len(project.Notes) != 1 || project.Notes[0].Title != "订单架构" || !strings.HasPrefix(project.ModifiedAt, "2030-09-08") {
		t.Fatalf("project aliases, task ownership, notes and activity: %#v", project)
	}
	var book *WorkbenchBook
	for i := range snapshot.Books {
		if snapshot.Books[i].Name == "Java 核心进阶" {
			book = &snapshot.Books[i]
		}
	}
	if book == nil || book.Progress != 65 || book.Status != "在学" || len(book.Projects) != 2 || book.NoteCount != 1 || book.ReviewCount != 1 || len(book.Chapters) != 1 {
		t.Fatalf("book projection: %#v", book)
	}
	card := snapshot.Cards[0]
	if card.Question != "[Q042] 为什么死锁？" || !card.Weak || card.Failures != 2 || !strings.Contains(card.Answer, "#### 核心要点") || strings.Contains(card.Answer, "固定锁顺序") {
		t.Fatalf("card boundary / state: %#v", card)
	}
	if len(snapshot.Warnings) != 1 || !strings.Contains(snapshot.Warnings[0], "01-并发.md") {
		t.Fatalf("invalid card must be visible as a warning: %#v", snapshot.Warnings)
	}
	if snapshot.Radar.Path != "Learning/技术雷达.md" || !strings.Contains(snapshot.Radar.Content, "只作索引") {
		t.Fatalf("radar: %#v", snapshot.Radar)
	}
	for _, task := range snapshot.Tasks {
		if task.Title == "Yesterday shipped" && (task.Date != "2026-09-07" || task.CompletedAt != "2026-09-07") {
			t.Errorf("legacy completed daily task has source-day completion: %#v", task)
		}
		if task.Title == "已修复" && task.Status != "done" {
			t.Errorf("completion overrides the historical blocker: %#v", task)
		}
	}
}

func TestWorkbenchReconstructsFromMarkdownAndNeverReturnsNullArrays(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/Empty/project.md", "# Empty\n")
	workbenchWrite(t, root, "Learning/Empty/book.md", "# Empty\n")
	first := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	data, err := json.Marshal(first)
	if err != nil || strings.Contains(string(data), ":null") {
		t.Fatalf("all snapshot collections, including nested collections, are arrays: %s (%v)", data, err)
	}
	workbenchWrite(t, root, "Daily/2026-09-08.md", "- [ ] external editor update\n")
	rebuilt := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(first.Tasks) != 0 || len(rebuilt.Tasks) != 1 || rebuilt.Tasks[0].Title != "external editor update" {
		t.Fatalf("fresh service must reconstruct from current Markdown: %#v", rebuilt.Tasks)
	}
	if _, err := os.Stat(filepath.Join(root, ".notevault")); !os.IsNotExist(err) {
		t.Fatalf("snapshot reads must not create an authoritative metadata store: %v", err)
	}
}

func TestWorkbenchMarkdownVisibilityHandlesBOMAndCommentedFences(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Tasks.md", "\ufeff- [ ] First task\n<!-- example\n```md\n- [ ] commented task\n-->\n- [ ] Last task\n")
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Tasks) != 2 || snapshot.Tasks[0].Title != "First task" || snapshot.Tasks[1].Title != "Last task" {
		t.Fatalf("BOM and syntax inside an HTML comment must not hide real tasks: %#v", snapshot.Tasks)
	}
}

func TestWorkbenchEnglishExtraAliasAndArbitraryMetadataOrder(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Tasks.md", "- [ ] [Extra] 排查 #due/2026-09-10 #project/物流 #priority/high status: blocked\n")
	task := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08").Tasks[0]
	if task.Type != "额外" || task.Title != "排查" || task.Due != "2026-09-10" || task.Project != "物流" || task.Priority != "high" || task.Status != "blocked" {
		t.Fatalf("the English alias and reordered metadata share the canonical task model: %#v", task)
	}
}

func TestWorkbenchNestedBlockerLabelRemainsLiteralText(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Tasks.md", "- [ ] Task (卡点: 文档里有 (卡点: 权限) 示例)\n")
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("a blocker containing an example label must not crash the Markdown scanner: %v", recovered)
		}
	}()
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Title != "Task" || snapshot.Tasks[0].Blocker != "文档里有 (卡点: 权限) 示例" {
		t.Fatalf("nested blocker label is part of the reason: %#v", snapshot.Tasks)
	}
}

func TestWorkbenchBookReviewCountsDoNotMatchUnrelatedWordFragments(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Learning/Go/book.md", "# Go\n")
	workbenchWrite(t, root, "Learning/面试宝典/MongoDB.md", "### 存储引擎？\n<!-- srs: {\"due\":\"2026-09-08\"} -->\n答案\n")
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if snapshot.Books[0].ReviewCount != 0 {
		t.Fatalf("MongoDB is not a Go review merely because its name contains 'go': %#v", snapshot.Books[0])
	}
}
