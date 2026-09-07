package service

import (
	"strings"
	"testing"
	"time"
)

type workbenchReviewer interface {
	ReviewInterviewCard(string, string, int, string, string, string) error
}

func workbenchReviewService(t *testing.T, service *TodoService) workbenchReviewer {
	t.Helper()
	reviewer, ok := any(service).(workbenchReviewer)
	if !ok {
		t.Fatal("TodoService must persist interview reviews to the source comment")
	}
	return reviewer
}

func TestWorkbenchSRSUsesFixedCalendarIntervalsAndPreservesUnknownJSON(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	previousLocal := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previousLocal })
	for _, test := range []struct {
		level, due string
		interval   int
	}{{"掌握", "2026-11-08", 7}, {"模糊", "2026-11-04", 3}, {"不会", "2026-11-02", 1}} {
		t.Run(test.level, func(t *testing.T) {
			root := t.TempDir()
			relative := "Learning/面试宝典/Go.md"
			comment := `<!-- srs: {"level": "掌握", "interval": 28, "due": "2026-11-01", "reps": 4, "custom": { "a": [1, 2], "huge": 9007199254740993 } } -->`
			source := "# Topic\r\n### [Q1] 为什么？\r\n  " + comment + "  \r\n#### 答案\r\n保留答案与格式。"
			workbenchWrite(t, root, relative, source)
			service := NewTodoService()
			if err := workbenchReviewService(t, service).ReviewInterviewCard(root, relative, 2, comment, test.level, "2026-11-01"); err != nil {
				t.Fatal(err)
			}
			updated := workbenchRead(t, root, relative)
			if !strings.Contains(updated, `"custom": { "a": [1, 2], "huge": 9007199254740993 }`) || !strings.HasPrefix(updated, "# Topic\r\n### [Q1] 为什么？\r\n  <!-- srs: {") || !strings.Contains(updated, " } -->  \r\n") || !strings.HasSuffix(updated, "  \r\n#### 答案\r\n保留答案与格式。") {
				t.Fatalf("only known SRS values may change, with unknown JSON and surrounding Markdown intact: %q", updated)
			}
			card := workbenchSnapshot(t, NewTodoService(), root, "2026-11-01").Cards[0]
			if card.Interval != test.interval || card.Due != test.due || card.Reps != 5 || card.Level != test.level || card.LastReviewed != "2026-11-01" {
				t.Fatalf("review state (including the 25-hour calendar day): %#v", card)
			}
			if err := workbenchReviewService(t, service).ReviewInterviewCard(root, relative, 2, comment, test.level, "2026-11-01"); err == nil {
				t.Fatal("a stale SRS comment must be rejected")
			}
			if workbenchRead(t, root, relative) != updated {
				t.Fatal("stale review altered Markdown")
			}
		})
	}
}

func TestWorkbenchSRSWeaknessRequiresConsecutiveFailures(t *testing.T) {
	root := t.TempDir()
	relative := "Learning/面试宝典/Java.md"
	workbenchWrite(t, root, relative, "### [Q2] 题目\n<!-- srs: {\"level\":\"不会\",\"interval\":1,\"due\":\"2026-09-08\",\"reps\":2} -->\n答案\n")
	service := NewTodoService()
	reviewer := workbenchReviewService(t, service)
	for _, test := range []struct {
		level    string
		failures int
		weak     bool
	}{{"不会", 2, true}, {"模糊", 0, false}, {"不会", 1, false}, {"不会", 2, true}, {"掌握", 0, false}} {
		card := workbenchSnapshot(t, service, root, "2026-09-08").Cards[0]
		if err := reviewer.ReviewInterviewCard(root, relative, card.LineIndex, card.Comment, test.level, "2026-09-08"); err != nil {
			t.Fatal(err)
		}
		reloaded := workbenchSnapshot(t, NewTodoService(), root, "2026-09-08").Cards[0]
		if reloaded.Failures != test.failures || reloaded.Weak != test.weak {
			t.Fatalf("%s: want failures=%d weak=%v, got %#v", test.level, test.failures, test.weak, reloaded)
		}
	}
}
