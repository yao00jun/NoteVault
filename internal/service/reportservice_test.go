package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReport_GenerateWeeklyReport_WithoutAI(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	// mtime 相对「本周一 0 点」构造：跨周界（周日夜→周一晨）跑测试时，
	// now.Add(-1h) 会落到上周窗口外，Notes 断言时间炸弹式失败
	weekday := (int(now.Weekday()) + 6) % 7 // 周一=0 … 周日=6
	weekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -weekday)
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0750); err != nil {
		t.Fatal(err)
	}
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# 笔记 A\n\n内容", weekStart.Add(2*time.Hour))
	mustWriteStats(t, filepath.Join(dir, "sub", "b.md"), "- [ ] 未完成\n- [ ] !! 紧急\n", weekStart.Add(3*time.Hour))

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), NewTodoService())
	result, err := s.GenerateWeeklyReport(dir, WeeklyReportAIConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AIUsed {
		t.Fatal("AI should not be used without config")
	}
	if result.Message == "" {
		t.Fatal("expected degrade message when AI not configured")
	}
	if result.Notes != 2 {
		t.Fatalf("expected Notes=2, got %d", result.Notes)
	}
	if result.Todos != 2 {
		t.Fatalf("expected Todos=2, got %d", result.Todos)
	}

	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatalf("report file should exist: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "知识周报") || !strings.Contains(text, "a.md") {
		t.Fatalf("unexpected report content:\n%s", text)
	}
	if strings.Contains(text, "## AI 回顾") {
		t.Fatal("AI section should be absent without AI config")
	}
	if !strings.Contains(text, "未完成 2") {
		t.Fatalf("report should contain todo stats, got:\n%s", text)
	}
	// 产物是普通 Markdown 笔记：wiki 链接语法可直接跳转
	if !strings.Contains(text, "[[sub/b.md|b.md]]") {
		t.Fatalf("changed notes should be wiki-linked, got:\n%s", text)
	}
}

func TestReport_GenerateWeeklyReport_WithAI(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# 笔记 A\n\n本周主要在研究并发编程。", time.Now().Add(-1*time.Hour))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"本周围绕并发编程展开，建议回顾 goroutine 泄漏检测。"}}]}`))
	}))
	defer server.Close()

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	result, err := s.GenerateWeeklyReport(dir, WeeklyReportAIConfig{
		BaseURL:  server.URL,
		Model:    "test-model",
		APIKey:   "test-key",
		Protocol: "openai-chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.AIUsed {
		t.Fatalf("expected AIUsed=true, message: %s", result.Message)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "goroutine 泄漏检测") {
		t.Fatalf("AI section missing from report:\n%s", data)
	}
}

func TestReport_GenerateWeeklyReport_AIFailureDegrades(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a", time.Now())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	result, err := s.GenerateWeeklyReport(dir, WeeklyReportAIConfig{
		BaseURL:  server.URL,
		Model:    "test-model",
		Protocol: "openai-chat",
	})
	if err != nil {
		t.Fatalf("AI failure must not fail the whole report: %v", err)
	}
	if result.AIUsed {
		t.Fatal("AIUsed should be false after AI failure")
	}
	if result.Message == "" {
		t.Fatal("expected degrade message")
	}
	// 纯统计版仍然落盘
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(result.Path))); err != nil {
		t.Fatalf("degraded report should still be written: %v", err)
	}
}

func TestReport_GenerateWeeklyReport_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a", time.Now())

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	r1, err := s.GenerateWeeklyReport(dir, WeeklyReportAIConfig{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.GenerateWeeklyReport(dir, WeeklyReportAIConfig{})
	if err != nil {
		t.Fatalf("regenerate should not fail on existing report: %v", err)
	}
	if r1.Path != r2.Path {
		t.Fatalf("same week should map to same path: %s vs %s", r1.Path, r2.Path)
	}
}

func TestReport_GenerateDailyBriefing_RequiresAIConfig(t *testing.T) {
	dir := t.TempDir()
	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), NewTodoService())
	if _, err := s.GenerateDailyBriefing(dir, "2026-11-15", WeeklyReportAIConfig{}); err == nil {
		t.Fatal("expected error when AI endpoint is not configured")
	}
}

func TestReport_GenerateDailyBriefing_SynthesizesWorkbenchSnapshot(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")
	workbenchWrite(t, dir, "Projects/商城/project.md", "---\ntype: project\nstatus: 进行中\n---\n\n# 商城\n")
	workbenchWrite(t, dir, "Projects/商城/Tasks.md", "- [ ] [US] 支付回调联调 #due/"+today+"\n- [ ] [DTS] 权限申请 (卡点：缺少联调账号)\n")
	workbenchWrite(t, dir, "Daily/Agent/"+today+".md", "# Agent 工作日志 "+today+"\n\n- 09:12 [claude-code] 修复导航测试\n")

	promptCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		promptCh <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"- 先做支付回调联调\n- 补齐权限申请避免阻塞\n- 趁热复习 Agent 修的导航改动"}}]}`))
	}))
	defer server.Close()

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), NewTodoService())
	result, err := s.GenerateDailyBriefing(dir, today, WeeklyReportAIConfig{
		BaseURL:  server.URL,
		Model:    "test-model",
		APIKey:   "test-key",
		Protocol: "openai-chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	prompt := <-promptCh
	for _, want := range []string{"支付回调联调", "缺少联调账号", "修复导航测试", "未完成 2"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("briefing prompt should include workbench snapshot (%s):\n%s", want, prompt)
		}
	}
	if !result.AIUsed || result.Date != today {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.HasPrefix(result.Briefing, "- ") || strings.Contains(result.Briefing, "##") {
		t.Fatalf("briefing should be plain bullet lines, got:\n%s", result.Briefing)
	}
}

func TestReport_GenerateDailyBriefing_AIFailureReturnsError(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	s := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), NewTodoService())
	if _, err := s.GenerateDailyBriefing(dir, time.Now().Format("2006-01-02"), WeeklyReportAIConfig{
		BaseURL:  server.URL,
		Model:    "test-model",
		Protocol: "openai-chat",
	}); err == nil {
		t.Fatal("expected error on AI failure: briefing has no degraded form")
	}
}

func TestStats_GetOnThisDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// 往年今日两篇 + 今年今日一篇（不算）+ 往年昨日一篇（不算）
	lastYearSameDay := time.Date(now.Year()-1, now.Month(), now.Day(), 10, 0, 0, 0, time.Local)
	mustWriteStats(t, filepath.Join(dir, "old1.md"), "# 旧笔记一\n", lastYearSameDay)
	mustWriteStats(t, filepath.Join(dir, "old2.md"), "# 旧笔记二\n", lastYearSameDay.Add(-time.Hour))
	mustWriteStats(t, filepath.Join(dir, "recent.md"), "# 今年写的\n", now)
	mustWriteStats(t, filepath.Join(dir, "yesterday.md"), "# 昨天的\n", now.AddDate(0, 0, -1))

	s := NewStatsService(nil, nil)
	hits, err := s.GetOnThisDay(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %v", hits)
	}
	if hits[0] != "old1.md" {
		t.Fatalf("expected newest first (old1.md), got %v", hits)
	}
}

// TestReportService_StartWeeklyReport_AsyncResult 验证异步周报：
// 任务成功结束 → 结果可按任务 ID 取回 → 周报文件确实落盘。
func TestReportService_StartWeeklyReport_AsyncResult(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# 笔记 A\n\n本周主要在研究并发编程。", time.Now().Add(-1*time.Hour))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"本周围绕并发编程展开。"}}]}`))
	}))
	defer server.Close()

	tasks := NewTaskService(nil)
	svc := NewReportServiceWithTasks(NewFileServiceWithHistory(NewSnapshotService()), nil, tasks)
	ai := WeeklyReportAIConfig{BaseURL: server.URL, Model: "test-model", APIKey: "k", Protocol: "openai-chat"}

	id := svc.StartWeeklyReport(dir, ai)
	if id == "" {
		t.Fatal("StartWeeklyReport 应返回任务 ID")
	}
	tasks.Wait()

	info := tasks.GetTask(id)
	if info == nil || info.Status != TaskSucceeded {
		t.Fatalf("任务应成功结束，实际：%+v", info)
	}
	res := svc.WeeklyReportTaskResult(id)
	if res == nil || !res.AIUsed {
		t.Fatalf("结果应为 AIUsed=true，实际：%+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(res.Path))); err != nil {
		t.Fatalf("周报文件应已落盘：%v", err)
	}
}

// TestReportService_StartWeeklyReport_Cancel 验证取消：
// AI 在途时取消 → 终态是 cancelled（不是 failed），纯统计版仍优雅落盘，
// 结果可取回且 AIUsed=false。
func TestReportService_StartWeeklyReport_Cancel(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# a", time.Now())

	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 必须先排干请求 body：连接层的「客户端断开检测」（后台读）只在
		// body 读完（EOF）后才会启动，不读的话客户端取消后 r.Context()
		// 永远不会取消，handler 挂死会连带 server.Close() 挂死。
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done() // 挂住，直到客户端取消请求
	}))
	defer server.Close()

	tasks := NewTaskService(nil)
	svc := NewReportServiceWithTasks(NewFileServiceWithHistory(NewSnapshotService()), nil, tasks)
	ai := WeeklyReportAIConfig{BaseURL: server.URL, Model: "test-model", Protocol: "openai-chat"}

	id := svc.StartWeeklyReport(dir, ai)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("AI 请求未在 5 秒内开始")
	}
	if !tasks.Cancel(id) {
		t.Fatal("取消应被接受")
	}
	tasks.Wait()

	info := tasks.GetTask(id)
	if info == nil || info.Status != TaskCancelled {
		t.Fatalf("终态应为 cancelled，实际：%+v", info)
	}
	res := svc.WeeklyReportTaskResult(id)
	if res == nil || res.AIUsed {
		t.Fatalf("取消后应为降级结果 AIUsed=false，实际：%+v", res)
	}
	// 纯统计骨架仍落盘（与同步降级语义一致）
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(res.Path))); err != nil {
		t.Fatalf("取消后纯统计版应仍落盘：%v", err)
	}
}

// TestReportService_StartWeeklyReport_NoQueueReturnsEmpty 验证未注入任务队列时
// 返回空 ID，调用方据此退回同步路径。
func TestReportService_StartWeeklyReport_NoQueueReturnsEmpty(t *testing.T) {
	svc := NewReportService(NewFileServiceWithHistory(NewSnapshotService()), nil)
	if id := svc.StartWeeklyReport(t.TempDir(), WeeklyReportAIConfig{}); id != "" {
		t.Fatalf("未注入队列应返回空 ID，实际：%q", id)
	}
}
