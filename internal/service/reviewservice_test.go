package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReview_GenerateReview_WithoutAI(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mustWriteStats(t, filepath.Join(dir, "recent.md"), "# 近期笔记\n", todayAt(now, time.Minute))
	mustWriteStats(t, filepath.Join(dir, "old.md"), "# 古董笔记\n", daysAgoAt(now, 120))

	s := NewReviewService(NewFileServiceWithHistory(NewSnapshotService()), NewSummarizeService(), nil)
	result, err := s.GenerateReview(dir, WeeklyReportAIConfig{}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.AIUsed {
		t.Fatal("AI should not be used without config")
	}
	if result.Message == "" {
		t.Fatal("expected degrade message")
	}
	if len(result.RecapTitles) != 1 || result.RecapTitles[0] != "old.md" {
		t.Fatalf("old note should be in recap, got %v", result.RecapTitles)
	}

	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatalf("review file should exist: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "定期回顾") || !strings.Contains(text, "[[recent.md|recent]]") {
		t.Fatalf("unexpected review content:\n%s", text)
	}
	if !strings.Contains(text, "[[old.md|old]]") {
		t.Fatalf("recap should be wiki-linked:\n%s", text)
	}
}

func TestReview_GenerateReview_WithAI(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# 笔记 A\n关于 Go 并发的研究笔记。", todayAt(now, time.Minute))
	mustWriteStats(t, filepath.Join(dir, "b.md"), "# 笔记 B\n关于数据库建模的读书笔记。", todayAt(now, 2*time.Minute))

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		if strings.Contains(string(body[:n]), "并发") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"A 的摘要：并发研究"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"B 的摘要：数据库建模"}}]}`))
	}))
	defer srv.Close()

	s := NewReviewService(NewFileServiceWithHistory(NewSnapshotService()), NewSummarizeService(), nil)
	result, err := s.GenerateReview(dir, WeeklyReportAIConfig{
		BaseURL:  srv.URL,
		Model:    "test-model",
		Protocol: "openai-chat",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !result.AIUsed || result.Summarized != 2 {
		t.Fatalf("expected AI summaries for 2 notes, got %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(result.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "并发研究") || !strings.Contains(string(data), "数据库建模") {
		t.Fatalf("per-note summaries missing:\n%s", data)
	}
	_ = requests
	_ = context.Background
}

func TestReview_GenerateReview_AIFailureDegradesPerNote(t *testing.T) {
	dir := t.TempDir()
	mustWriteStats(t, filepath.Join(dir, "a.md"), "# A\n内容", todayAt(time.Now(), time.Minute))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewReviewService(NewFileServiceWithHistory(NewSnapshotService()), NewSummarizeService(), nil)
	result, err := s.GenerateReview(dir, WeeklyReportAIConfig{
		BaseURL:  srv.URL,
		Model:    "m",
		Protocol: "openai-chat",
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Summarized != 0 {
		t.Fatalf("single-note failure should degrade that note only, got %+v", result)
	}
	if result.AIUsed {
		t.Fatal("AIUsed should be false when all summaries failed")
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(result.Path))); err != nil {
		t.Fatalf("review should still be written: %v", err)
	}
}

func TestReview_ScheduleWeeklyReview(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".notevault"), 0750); err != nil {
		t.Fatal(err)
	}
	s := NewReviewService(NewFileServiceWithHistory(NewSnapshotService()), NewSummarizeService(), NewReminderService())

	rem, err := s.ScheduleWeeklyReview(dir)
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339, rem.RemindAt)
	if err != nil {
		t.Fatalf("remindAt should be RFC3339: %v", err)
	}
	if at.Weekday() != time.Monday {
		t.Fatalf("reminder should land on Monday, got %v", at.Weekday())
	}
	if at.Hour() != 9 || at.Minute() != 0 {
		t.Fatalf("reminder should be at 09:00, got %02d:%02d", at.Hour(), at.Minute())
	}
	if !at.After(time.Now()) {
		t.Fatal("reminder must be in the future")
	}

	// 重复预约不报错（生成第二条提醒；到点前用户可自行删除旧的）
	if _, err := s.ScheduleWeeklyReview(dir); err != nil {
		t.Fatalf("re-schedule should not fail: %v", err)
	}
}
