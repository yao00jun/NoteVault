package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type workbenchTaskWriter interface {
	UpdateWorkbenchTask(string, string, int, string, string, string, string) error
}

func workbenchWriter(t *testing.T, service *TodoService) workbenchTaskWriter {
	t.Helper()
	writer, ok := any(service).(workbenchTaskWriter)
	if !ok {
		t.Fatal("TodoService must provide stale-checked task mutations")
	}
	return writer
}

func TestWorkbenchProgressIsReadableAndReconstructableFromItsProject(t *testing.T) {
	root := t.TempDir()
	relative := "Projects/商城/Tasks.md"
	source := "\ufeff# Tasks\r\n\r\n- [ ] [US] 订单接口 #date/2026-09-08  \r\n  - 保留已有细节\r\n\r\n- [ ] Next\r\n尾注"
	daily := "# 私人日记\r\n保留流水账\r\n\r\n## 工作进展\r\n- 08:00 · 历史镜像 <!-- workbench-mirror: {\"id\":\"legacy\",\"taskId\":\"Projects/商城/Tasks.md:2\",\"filePath\":\"Projects/商城/Tasks.md\",\"taskTitle\":\"订单接口\",\"project\":\"商城\",\"note\":\"历史镜像\",\"at\":\"2026-09-08T08:00:00+08:00\"} -->\r\n"
	workbenchWrite(t, root, relative, source)
	workbenchWrite(t, root, "Daily/2026-09-08.md", daily)
	service := NewTodoService()
	task := workbenchSnapshot(t, service, root, "2026-09-08").Tasks[0]
	note := "接口已联调，等待验证 <5ms>"
	if err := workbenchWriter(t, service).UpdateWorkbenchTask(root, relative, task.LineIndex, task.SourceLine, "progress", note, "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	updated := workbenchRead(t, root, relative)
	if !strings.HasPrefix(updated, "\ufeff# Tasks\r\n\r\n"+task.SourceLine+"\r\n") || !strings.HasSuffix(updated, "  - 保留已有细节\r\n\r\n- [ ] Next\r\n尾注") || !strings.Contains(updated, "  - 2026-09-08 ") {
		t.Fatalf("task progress must preserve unrelated bytes and remain readable: %q", updated)
	}
	if strings.Count(updated, "\n") != strings.Count(updated, "\r\n") {
		t.Fatal("progress changed the note's CRLF endings")
	}
	if got := workbenchRead(t, root, "Daily/2026-09-08.md"); got != daily {
		t.Fatalf("project progress must leave the existing diary unchanged: %q", got)
	}
	rebuilt := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(rebuilt.Progress) != 1 || len(rebuilt.Tasks[0].Progress) != 1 || rebuilt.Progress[0].Note != note || rebuilt.Progress[0].ID == "" || !strings.HasPrefix(rebuilt.Progress[0].At, "2026-09-08T") {
		t.Fatalf("progress reconstructs once from the original task without counting a legacy diary mirror: %#v", rebuilt.Progress)
	}
	if rebuilt.Progress[0].TaskID != rebuilt.Tasks[0].ID || rebuilt.Progress[0].FilePath != relative {
		t.Fatalf("progress belongs to its current Markdown task: %#v", rebuilt.Progress[0])
	}
}

func TestWorkbenchProgressOnDailyTaskDoesNotDuplicateItself(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Daily/2026-09-08.md", "- [ ] 自己的日记任务")
	service := NewTodoService()
	writer := workbenchWriter(t, service)
	if err := writer.UpdateWorkbenchTask(root, "Daily/2026-09-08.md", 0, "- [ ] 自己的日记任务", "progress", "第一条", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Tasks) != 1 || len(snapshot.Progress) != 1 || strings.Count(workbenchRead(t, root, "Daily/2026-09-08.md"), "第一条") > 2 {
		t.Fatalf("a task already in today's diary needs only one source entry: %#v", snapshot)
	}
}

func TestWorkbenchTaskCompletionAndBlockerClearPreserveMarkdown(t *testing.T) {
	root := t.TempDir()
	line := "  * [ ] [DTS] 修复问题 #custom/保留 #status/blocked (卡点: 缺权限)  "
	source := "# Work\r\n" + line + "\r\n正文保持不变"
	workbenchWrite(t, root, "Tasks.md", source)
	service := NewTodoService()
	writer := workbenchWriter(t, service)
	if err := writer.UpdateWorkbenchTask(root, "Tasks.md", 1, line, "toggle", "", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	completed := workbenchSnapshot(t, service, root, "2026-09-08").Tasks[0]
	if !completed.Completed || completed.CompletedAt != "2026-09-08" || completed.Status != "done" || !strings.Contains(completed.SourceLine, "#custom/保留") || !strings.HasSuffix(completed.SourceLine, "  ") {
		t.Fatalf("completion metadata: %#v", completed)
	}
	if err := writer.UpdateWorkbenchTask(root, "Tasks.md", 1, completed.SourceLine, "toggle", "", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	if got := workbenchRead(t, root, "Tasks.md"); got != source {
		t.Fatalf("toggle twice restores the exact original Markdown: %q", got)
	}
	if err := writer.UpdateWorkbenchTask(root, "Tasks.md", 1, line, "blocker", "", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	cleared := workbenchSnapshot(t, service, root, "2026-09-08").Tasks[0]
	if cleared.Status == "blocked" || cleared.Blocker != "" || strings.Contains(cleared.SourceLine, "#status/blocked") || strings.Contains(cleared.SourceLine, "卡点") {
		t.Fatalf("clear removes both blocker sources: %#v", cleared)
	}
	if err := writer.UpdateWorkbenchTask(root, "Tasks.md", 1, cleared.SourceLine, "blocker", "环境（测试）权限缺失", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	blocked := workbenchSnapshot(t, service, root, "2026-09-08").Tasks[0]
	if blocked.Blocker != "环境（测试）权限缺失" || blocked.Status != "blocked" || !strings.HasSuffix(workbenchRead(t, root, "Tasks.md"), "\r\n正文保持不变") {
		t.Fatalf("blockers with parentheses round-trip without changing surrounding text: %#v", blocked)
	}
}

func TestWorkbenchTaskRejectsStaleUnsafeAndInvalidMutations(t *testing.T) {
	root := t.TempDir()
	source := "- [ ] Changed in editor\n```md\n- [ ] example\n```\n"
	workbenchWrite(t, root, "Tasks.md", source)
	writer := workbenchWriter(t, NewTodoService())
	cases := []struct {
		name, relative, expected, action, value, date string
		line                                          int
	}{
		{"stale", "Tasks.md", "- [ ] Original", "toggle", "", "2026-09-08", 0},
		{"out of range", "Tasks.md", "- [ ] Changed in editor", "toggle", "", "2026-09-08", 90},
		{"fence", "Tasks.md", "- [ ] example", "toggle", "", "2026-09-08", 2},
		{"traversal", "../outside.md", "anything", "toggle", "", "2026-09-08", 0},
		{"backslash traversal", "..\\outside.md", "anything", "toggle", "", "2026-09-08", 0},
		{"invalid date", "Tasks.md", "- [ ] Changed in editor", "toggle", "", "2026-02-30", 0},
		{"unknown action", "Tasks.md", "- [ ] Changed in editor", "erase", "", "2026-09-08", 0},
		{"multiline", "Tasks.md", "- [ ] Changed in editor", "progress", "injected\n- [x] task", "2026-09-08", 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := writer.UpdateWorkbenchTask(root, test.relative, test.line, test.expected, test.action, test.value, test.date); err == nil {
				t.Fatal("unsafe mutation must fail")
			}
			if got := workbenchRead(t, root, "Tasks.md"); got != source {
				t.Fatalf("rejected mutation changed user text: %q", got)
			}
		})
	}
}

func TestWorkbenchProgressDoesNotDependOnDailyStorage(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/Tasks.md", "- [ ] Keep me\n")
	workbenchWrite(t, root, "Daily", "this is a file, not a folder")
	err := workbenchWriter(t, NewTodoService()).UpdateWorkbenchTask(root, "Projects/A/Tasks.md", 0, "- [ ] Keep me", "progress", "source only", "2026-09-08")
	if err != nil {
		t.Fatalf("recording project progress must not depend on a Daily folder: %v", err)
	}
	if got := workbenchRead(t, root, "Daily"); got != "this is a file, not a folder" {
		t.Fatalf("progress changed unrelated Daily storage: %q", got)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Progress) != 1 || snapshot.Progress[0].Note != "source only" || snapshot.Progress[0].FilePath != "Projects/A/Tasks.md" {
		t.Fatalf("the timeline must use the project's task progress: %#v", snapshot.Progress)
	}
}

func TestWorkbenchProgressDoesNotCreateADailyDiary(t *testing.T) {
	for _, relative := range []string{"Projects/A/Tasks.md", "Daily/2020-01-01.md"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, relative, "- [ ] Carryover\n")
			if err := NewTodoService().UpdateWorkbenchTask(root, relative, 0, "- [ ] Carryover", "progress", "advanced today", "2026-09-08"); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "Daily", "2026-09-08.md")); !os.IsNotExist(err) {
				t.Fatalf("progress must not create another daily diary: %v", err)
			}
			snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
			if len(snapshot.Progress) != 1 || snapshot.Progress[0].FilePath != relative {
				t.Fatalf("project and legacy task progress must remain editable at their source: %#v", snapshot.Progress)
			}
		})
	}
}

func TestWorkbenchDailyReportsPersistExactDraftAndPropagateErrors(t *testing.T) {
	service := NewTodoService()
	reports, ok := any(service).(interface {
		ReadDailyReport(string, string) (string, error)
		SaveDailyReport(string, string, string, string) (string, error)
	})
	if !ok {
		t.Fatal("TodoService must expose Markdown daily reports")
	}
	root := t.TempDir()
	if content, err := reports.ReadDailyReport(root, "2026-09-08"); err != nil || content != "" {
		t.Fatalf("a missing report is an empty draft: %q, %v", content, err)
	}
	draft := "主题：【日报】测试\r\n\r\n今日完成：\r\n1. 保留文字  \r\n"
	if relative, err := reports.SaveDailyReport(root, "2026-09-08", draft, ""); err != nil || relative != "Daily/Reports/2026-09-08-日报.md" {
		t.Fatalf("save report: %q, %v", relative, err)
	}
	if content, err := reports.ReadDailyReport(root, "2026-09-08"); err != nil || content != draft {
		t.Fatalf("report draft must round-trip verbatim: %q, %v", content, err)
	}
	if _, err := reports.SaveDailyReport(root, "../outside", draft, ""); err == nil {
		t.Fatal("report date must reject traversal")
	}
	if err := os.MkdirAll(filepath.Join(root, "Daily", "Reports", "2026-09-09-日报.md"), 0750); err != nil {
		t.Fatal(err)
	}
	if _, err := reports.ReadDailyReport(root, "2026-09-09"); err == nil {
		t.Fatal("non-missing read errors must be surfaced")
	}
}

func TestWorkbenchAddTaskCreatesPortableProjectAndGeneralTasks(t *testing.T) {
	service := NewTodoService()
	adder, ok := any(service).(interface {
		AddWorkbenchTask(string, string, string, string, string, string) error
	})
	if !ok {
		t.Fatal("TodoService must add tasks to Markdown")
	}
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/Tasks.md", "# Existing\r\nKeep this paragraph")
	if err := adder.AddWorkbenchTask(root, "Projects/A", "联调接口", "US", "2026-09-10", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	if err := adder.AddWorkbenchTask(root, "", "临时排查", "额外", "", "2026-09-08"); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	if len(snapshot.Tasks) != 2 {
		t.Fatalf("added tasks reconstruct from Markdown: %#v", snapshot.Tasks)
	}
	for _, task := range snapshot.Tasks {
		if task.Date != "2026-09-08" {
			t.Errorf("new tasks have a scheduling day: %#v", task)
		}
		if task.Type == "US" && (task.Project != "A" || task.Due != "2026-09-10" || task.Title != "联调接口") {
			t.Errorf("project task metadata: %#v", task)
		}
		if task.Type == "额外" && (task.Project != "通用事务" || task.FilePath != "Projects/通用事务/Tasks.md" || task.ProjectPath != "Projects/通用事务/project.md") {
			t.Errorf("a general task must belong to its own project: %#v", task)
		}
	}
	if len(snapshot.Projects) != 1 || snapshot.Projects[0].Name != "通用事务" || len(snapshot.Projects[0].TaskIDs) != 1 {
		t.Errorf("the general project must be discoverable immediately: %#v", snapshot.Projects)
	}
	if _, err := os.Stat(filepath.Join(root, "Daily")); !os.IsNotExist(err) {
		t.Fatalf("adding a task must not create daily storage: %v", err)
	}
	if !strings.HasPrefix(workbenchRead(t, root, "Projects/A/Tasks.md"), "# Existing\r\nKeep this paragraph\r\n") {
		t.Fatal("adding a task must preserve existing Markdown and newline style")
	}
	for _, folder := range []string{"../outside", "Resources/anything", "Projects/../../outside"} {
		if err := adder.AddWorkbenchTask(root, folder, "bad", "todo", "", "2026-09-08"); err == nil {
			t.Errorf("invalid project folder %q was accepted", folder)
		}
	}
	if err := adder.AddWorkbenchTask(root, "", "bad\n- [x] injected", "todo", "", "2026-09-08"); err == nil {
		t.Fatal("new task title must be one line")
	}
}

func TestWorkbenchLegacyToggleKeepsCompletionDatesConsistent(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Daily/2020-01-01.md", "- [ ] carryover\n```md\n- [ ] example\n```\n")
	service := NewTodoService()
	if err := service.ToggleTodo(root, "Daily/2020-01-01.md", 0); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	task := workbenchSnapshot(t, service, root, today).Tasks[0]
	if task.CompletedAt != today {
		t.Fatalf("the legacy entry point must stamp the actual completion day: %#v", task)
	}
	if err := service.ToggleTodo(root, "Daily/2020-01-01.md", 2); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(workbenchRead(t, root, "Daily/2020-01-01.md"), "[x] example") {
		t.Fatal("legacy toggle must honor the same Markdown visibility rules")
	}
}

func TestWorkbenchAddTaskDoesNotWriteIntoAnUnclosedExample(t *testing.T) {
	root := t.TempDir()
	source := "# Tasks\n```md\nexample that is still being edited\n"
	workbenchWrite(t, root, "Projects/通用事务/Tasks.md", source)
	err := NewTodoService().AddWorkbenchTask(root, "", "Real task", "todo", "", "2026-09-08")
	if err == nil || workbenchRead(t, root, "Projects/通用事务/Tasks.md") != source {
		t.Fatalf("an invisible task must not be silently written into a code sample: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Projects", "通用事务", "project.md")); !os.IsNotExist(err) {
		t.Fatalf("an invalid task must not partially create project metadata: %v", err)
	}
}

func TestWorkbenchAddTaskInitializesExplicitGeneralProject(t *testing.T) {
	for _, folder := range []string{"", "Projects/通用事务", "Projects\\通用事务"} {
		t.Run(folder, func(t *testing.T) {
			root := t.TempDir()
			if err := NewTodoService().AddWorkbenchTask(root, folder, "处理报销", "todo", "", "2026-09-08"); err != nil {
				t.Fatal(err)
			}
			snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
			if len(snapshot.Projects) != 1 || snapshot.Projects[0].Folder != "Projects/通用事务" || snapshot.Projects[0].Status != "进行中" || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].FilePath != "Projects/通用事务/Tasks.md" {
				t.Fatalf("empty or explicit general selection must create a discoverable project and task: %#v", snapshot)
			}
		})
	}
}

func TestWorkbenchAddTaskPreservesExistingGeneralProjectMetadata(t *testing.T) {
	for _, metadata := range []string{"", "\ufeff---\r\ntitle: 自定义杂务\r\nstatus: paused\r\ncustom: keep\r\n---\r\n手写说明  "} {
		t.Run(metadata, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, "Projects/通用事务/project.md", metadata)
			if err := NewTodoService().AddWorkbenchTask(root, "Projects/通用事务", "一般事项", "todo", "", "2026-09-08"); err != nil {
				t.Fatal(err)
			}
			if got := workbenchRead(t, root, "Projects/通用事务/project.md"); got != metadata {
				t.Fatalf("an existing project.md must be preserved verbatim, including an empty file: %q", got)
			}
		})
	}
}

func TestWorkbenchAddTaskPreflightsGeneralProjectMetadata(t *testing.T) {
	root := t.TempDir()
	source := "# Existing tasks\r\n- [ ] Keep me\r\n"
	workbenchWrite(t, root, "Projects/通用事务/Tasks.md", source)
	if err := os.Mkdir(filepath.Join(root, "Projects", "通用事务", "project.md"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := NewTodoService().AddWorkbenchTask(root, "Projects/通用事务", "cannot save", "todo", "", "2026-09-08"); err == nil {
		t.Fatal("invalid project metadata must reject the whole change")
	}
	if got := workbenchRead(t, root, "Projects/通用事务/Tasks.md"); got != source {
		t.Fatalf("metadata preparation failure must leave tasks unchanged: %q", got)
	}
}

func TestWorkbenchTaskProgressEndsWithLatestEntry(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Tasks.md", "- [ ] Work\n")
	service := NewTodoService()
	for _, note := range []string{"Started", "Finished"} {
		if err := service.UpdateWorkbenchTask(root, "Tasks.md", 0, "- [ ] Work", "progress", note, "2026-09-08"); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08")
	progress := snapshot.Tasks[0].Progress
	if len(progress) != 2 || progress[0].Note != "Started" || progress[1].Note != "Finished" || snapshot.Progress[0].Note != "Finished" {
		t.Fatalf("task history is chronological and the global timeline starts with the latest: task=%#v global=%#v", progress, snapshot.Progress)
	}
	if err := service.AddWorkbenchTask(root, "", "临时任务", "Extra", "", "2026-09-08"); err != nil {
		t.Fatalf("adding an English Extra task should use the canonical extra type: %v", err)
	}
}

func TestWorkbenchAddTaskRejectsMissingProject(t *testing.T) {
	root := t.TempDir()
	err := NewTodoService().AddWorkbenchTask(root, "Projects/Deleted", "stale selection", "US", "", "2026-09-08")
	if err == nil {
		t.Fatal("a stale selection must not recreate a missing project")
	}
	if _, statErr := os.Stat(filepath.Join(root, "Projects", "Deleted")); !os.IsNotExist(statErr) {
		t.Fatalf("the missing project directory must remain missing: %v", statErr)
	}
}

func TestWorkbenchDailyReportRejectsStaleDraft(t *testing.T) {
	reports, ok := any(NewTodoService()).(interface {
		SaveDailyReport(string, string, string, string) (string, error)
	})
	if !ok {
		t.Fatal("saving a report must accept the original content for conflict detection")
	}
	root := t.TempDir()
	relative := "Daily/Reports/2026-09-08-日报.md"
	workbenchWrite(t, root, relative, "Edited in another window\r\n")
	if _, err := reports.SaveDailyReport(root, "2026-09-08", "Stale modal draft", "Original draft"); err == nil {
		t.Fatal("a stale report save must fail visibly")
	}
	if got := workbenchRead(t, root, relative); got != "Edited in another window\r\n" {
		t.Fatalf("stale save overwrote the external edit: %q", got)
	}
	if saved, err := reports.SaveDailyReport(root, "2026-09-08", "Merged draft", "Edited in another window\r\n"); err != nil || saved != relative {
		t.Fatalf("saving against the current content should succeed: %q, %v", saved, err)
	}
	if got := workbenchRead(t, root, relative); got != "Merged draft" {
		t.Fatalf("latest report content: %q", got)
	}
}
