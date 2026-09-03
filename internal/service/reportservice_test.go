package service

import (
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
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0750); err != nil {
		t.Fatal(err)
	}
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# 笔记 A\n\n内容", now.Add(-1*time.Hour))
	mustWriteStats(t, filepath.Join(dir, "sub", "b.md"), "- [ ] 未完成\n- [ ] !! 紧急\n", now.Add(-2*time.Hour))

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
