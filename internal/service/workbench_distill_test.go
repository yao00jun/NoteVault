package service

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDistillBookCreatesDiscoverableLinkedChapter(t *testing.T) {
	root := t.TempDir()
	sourcePath := "Projects/物流中台/Kafka排查记录.md"
	source := "\ufeff---\r\ntitle: 原始排查记录\r\ntags: [Go, 并发, 排错复盘]\r\ncustom: 保留\r\n---\r\n# 原始记录\r\n\r\n原始内容  \r\n未加换行的尾注"
	workbenchWrite(t, root, sourcePath, source)
	req := DistillRequest{SourceFile: sourcePath, TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "Kafka消息积压.md", Summary: "## 定位\n\n使用 pprof 定位阻塞，保留换行。\n\n```go\nctx, cancel := context.WithTimeout(ctx, timeout)\n```"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	chapterPath := "Learning/Go/Kafka消息积压.md"
	chapter := workbenchRead(t, root, chapterPath)
	fm, body, found := SplitFrontMatter(chapter)
	if !found {
		t.Fatalf("a distilled chapter needs portable frontmatter: %q", chapter)
	}
	props := ParseFrontMatter(fm)
	if props["type"].StringValue() != "tech-note" || props["sourceProject"].StringValue() != "[[Projects/物流中台/project.md|物流中台]]" || props["distilledAt"].StringValue() != time.Now().Format("2006-01-02") {
		t.Fatalf("chapter metadata must point back to the project and record its distillation date: %#v", props)
	}
	if !reflect.DeepEqual(props["tags"].List, []string{"Go", "并发", "排错复盘"}) {
		t.Fatalf("source tags must travel with the chapter: %#v", props["tags"])
	}
	if !strings.Contains(body, "[[Projects/物流中台/Kafka排查记录.md|物流中台项目实战]]") || !strings.Contains(strings.ReplaceAll(body, "\r\n", "\n"), req.Summary) {
		t.Fatalf("the chapter must retain the requested summary and a source-file backlink: %q", body)
	}
	updatedSource := workbenchRead(t, root, sourcePath)
	if !strings.HasPrefix(updatedSource, source+"\r\n") || !strings.Contains(updatedSource[len(source):], "[[Learning/Go/Kafka消息积压.md|") || !strings.Contains(updatedSource[len(source):], "技术资产沉淀") {
		t.Fatalf("append the source badge without changing original bytes: %q", updatedSource)
	}
	if strings.Count(updatedSource, "\n") != strings.Count(updatedSource, "\r\n") {
		t.Fatalf("source newline style changed: %q", updatedSource)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Books) != 1 || snapshot.Books[0].Folder != "Learning/Go" || snapshot.Books[0].Name != "Go" || len(snapshot.Books[0].Chapters) != 1 || snapshot.Books[0].Chapters[0].Path != chapterPath {
		t.Fatalf("a new book must be discoverable from Markdown alone: %#v", snapshot.Books)
	}
}

func TestDistillInterviewAppendsReviewableCard(t *testing.T) {
	root := t.TempDir()
	sourcePath := "Projects/物流中台/Kafka排查记录.md"
	workbenchWrite(t, root, sourcePath, "# 项目排错\n保留原稿\n")
	topicPath := "Learning/面试宝典/Go面试真题.md"
	topic := "---\ntitle: 自己的题库\ncustom: [keep, this]\n---\n# Go 面试\n\n### [Q1] 旧问题\n<!-- srs: {\"level\":\"模糊\",\"interval\":12,\"due\":\"2027-01-01\",\"reps\":5,\"custom\":{\"keep\":true}} -->\n旧答案和手写笔记。"
	workbenchWrite(t, root, topicPath, topic)
	req := DistillRequest{SourceFile: sourcePath, TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go面试真题", Question: "如何定位 Kafka 消息堆积？", Answer: "1. 用 pprof 排查阻塞。\n2. 限制 WorkerPool 并发。"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	updated := workbenchRead(t, root, topicPath)
	if !strings.HasPrefix(updated, topic+"\n") {
		t.Fatalf("existing topic frontmatter, SRS and answer must remain byte-exact: %q", updated)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	var added *InterviewCard
	for i := range snapshot.Cards {
		if strings.Contains(snapshot.Cards[i].Question, req.Question) {
			added = &snapshot.Cards[i]
		}
	}
	if len(snapshot.Cards) != 2 || added == nil || len(snapshot.Warnings) != 0 {
		t.Fatalf("the appended card must be parsed immediately: cards=%#v warnings=%#v", snapshot.Cards, snapshot.Warnings)
	}
	if !strings.HasPrefix(added.Question, "[Q") || added.Level != "掌握" || added.Interval != 7 || added.Reps != 1 || added.Due != time.Now().AddDate(0, 0, 7).Format("2006-01-02") {
		t.Fatalf("new practical knowledge starts with the required SRS schedule: %#v", added)
	}
	var srs map[string]any
	match := workbenchSRSRE.FindStringSubmatch(added.Comment)
	if len(match) < 2 || json.Unmarshal([]byte(match[1]), &srs) != nil || srs["source"] != "Projects/物流中台" {
		t.Fatalf("SRS metadata must be valid JSON with the project directory: %q", added.Comment)
	}
	if !strings.Contains(added.Answer, req.Answer) || !strings.Contains(added.Answer, "[[Projects/物流中台/Kafka排查记录.md|") || !strings.Contains(workbenchRead(t, root, sourcePath), "[[Learning/面试宝典/Go面试真题.md|") {
		t.Fatalf("the card answer and source need mutual links: %#v", added)
	}
	if err := NewTodoService().ReviewInterviewCard(root, added.FilePath, added.LineIndex, added.Comment, "不会", time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("a distilled card must work with the existing review API: %v", err)
	}
}

func TestDistillBookRetryPreservesLaterEdits(t *testing.T) {
	root := t.TempDir()
	sourcePath := "Projects/A/debug.md"
	workbenchWrite(t, root, sourcePath, "# 原稿\n第一版\n")
	req := DistillRequest{SourceFile: sourcePath, TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "最初沉淀的正文"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	chapterPath := "Learning/Go/排错.md"
	chapter := strings.Replace(workbenchRead(t, root, chapterPath), req.Summary, "后来手工完善的正文", 1)
	source := workbenchRead(t, root, sourcePath) + "\n新增项目进展。\n"
	book := "---\ntitle: 我手工改名的分册\nstatus: settled\ncustom: untouched\n---\n# 保留分册说明\n"
	workbenchWrite(t, root, chapterPath, chapter)
	workbenchWrite(t, root, sourcePath, source)
	workbenchWrite(t, root, "Learning/Go/book.md", book)
	oldTime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.Local)
	for _, relative := range []string{sourcePath, chapterPath, "Learning/Go/book.md"} {
		if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(relative)), oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	req.SourceFile = "Projects\\A\\debug.md"
	req.TargetTitle = "排错.MD"
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatalf("the same normalized request must be idempotent after a service restart: %v", err)
	}
	for relative, expected := range map[string]string{sourcePath: source, chapterPath: chapter, "Learning/Go/book.md": book} {
		if got := workbenchRead(t, root, relative); got != expected {
			t.Errorf("retry rewrote edited document %s: %q", relative, got)
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !info.ModTime().Equal(oldTime) {
			t.Errorf("a complete retry should not write %s again: info=%v err=%v", relative, info, err)
		}
	}
}

func TestDistillInterviewRetryPreservesReviewAndRepairsBadge(t *testing.T) {
	root := t.TempDir()
	sourcePath := "Projects/A/debug.md"
	source := "# 实战\n原始记录\n"
	workbenchWrite(t, root, sourcePath, source)
	req := DistillRequest{SourceFile: sourcePath, TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "如何取消？", Answer: "使用 context。"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Cards) != 1 {
		t.Fatalf("expected one new card: %#v", snapshot.Cards)
	}
	card := snapshot.Cards[0]
	if err := NewTodoService().ReviewInterviewCard(root, card.FilePath, card.LineIndex, card.Comment, "模糊", "2026-10-01"); err != nil {
		t.Fatal(err)
	}
	updatedTopic := workbenchRead(t, root, card.FilePath) + "\n个人补充，不要覆盖。\n"
	workbenchWrite(t, root, card.FilePath, updatedTopic)
	// A recovered source snapshot can lose the backlink after the target saved.
	workbenchWrite(t, root, sourcePath, source+"\n恢复后新增的进展。\n")
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	if got := workbenchRead(t, root, card.FilePath); got != updatedTopic {
		t.Fatalf("retry must not duplicate or reset a reviewed card: %q", got)
	}
	updatedSource := workbenchRead(t, root, sourcePath)
	if !strings.HasPrefix(updatedSource, source+"\n恢复后新增的进展。\n") || strings.Count(updatedSource, "技术资产沉淀") != 1 {
		t.Fatalf("retry must recover only the missing badge: %q", updatedSource)
	}
	rebuilt := workbenchSnapshot(t, NewTodoService(), root, "2026-10-01")
	if len(rebuilt.Cards) != 1 || rebuilt.Cards[0].Level != "模糊" || rebuilt.Cards[0].Reps != 2 || rebuilt.Cards[0].Due != "2026-10-04" {
		t.Fatalf("recovered retry must retain the later review state: %#v", rebuilt.Cards)
	}
}

func TestDistillRetryRecognizesPortableMarkdownMarker(t *testing.T) {
	root := t.TempDir()
	// Frozen v1 Markdown from another machine. Its identity must not depend on
	// whether the current host has a case-sensitive filesystem.
	marker := "<!-- notevault-distill: v1:098d369d9ccfacc7f7f2c313d6d5b4c3a88be333b3bb2a4651c065270b82151d -->"
	source := "# 原稿\n\n> 💡 **技术资产沉淀**：已沉淀至 [[Learning/面试宝典/Go.md|面试宝典 - Go]] " + marker + "\n"
	topic := "# Go\n\n" + marker + "\n### [Q098d369d9ccfacc7] 为什么？\n<!-- srs: {\"level\":\"模糊\",\"interval\":3,\"due\":\"2027-01-03\",\"reps\":4,\"source\":\"Projects/A\"} -->\n\n用户已补充的答案。\n"
	workbenchWrite(t, root, "Projects/A/debug.md", source)
	workbenchWrite(t, root, "Learning/面试宝典/Go.md", topic)
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "为什么？", Answer: "原始答案"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	if workbenchRead(t, root, "Projects/A/debug.md") != source || workbenchRead(t, root, "Learning/面试宝典/Go.md") != topic {
		t.Fatal("moving the Markdown workspace to another host must not duplicate a completed request")
	}
}

func TestDistillSourceBadgeExampleDoesNotSuppressMissingLink(t *testing.T) {
	root := t.TempDir()
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "正文"}
	workbenchWrite(t, root, req.SourceFile, "# 原稿\n")
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	badge := regexp.MustCompile(`(?m)^> 💡 .*`).FindString(workbenchRead(t, root, req.SourceFile))
	if badge == "" {
		t.Fatal("missing source badge")
	}
	source := "# 原稿\n\n```md\n" + badge + "\n```\n\n用户保留的示例。\n"
	target := workbenchRead(t, root, "Learning/Go/排错.md")
	workbenchWrite(t, root, req.SourceFile, source)
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	if got := workbenchRead(t, root, req.SourceFile); got != source+"\n"+badge+"\n" {
		t.Fatalf("a code example must remain intact while the actual missing source link is repaired: %q", got)
	}
	if workbenchRead(t, root, "Learning/Go/排错.md") != target {
		t.Fatal("repairing the badge rewrote the chapter")
	}
}

func TestDistillBookRejectsChapterCollision(t *testing.T) {
	for _, existing := range []string{"", "---\ncustom: 保留\n---\n# 手写的章节\n不要覆盖。"} {
		t.Run(existing, func(t *testing.T) {
			root := t.TempDir()
			source := "# 原稿\n"
			workbenchWrite(t, root, "Projects/A/debug.md", source)
			workbenchWrite(t, root, "Learning/Go/排错.md", existing)
			err := NewTodoService().DistillKnowledge(root, DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "新的正文"})
			if err == nil {
				t.Fatal("an existing chapter, including an empty file, must not be overwritten")
			}
			if workbenchRead(t, root, "Learning/Go/排错.md") != existing || workbenchRead(t, root, "Projects/A/debug.md") != source {
				t.Fatal("a collision changed the existing chapter or source")
			}
			if _, err := os.Stat(filepath.Join(root, "Learning", "Go", "book.md")); !os.IsNotExist(err) {
				t.Fatalf("a rejected request must not leave a new book behind: %v", err)
			}
		})
	}
}

func TestDistillRejectsUnsafeOrIncompleteRequestsWithoutWriting(t *testing.T) {
	base := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "完整技术结论。"}
	tests := []struct {
		name, field, value string
	}{
		{"missing source", "source", ""},
		{"missing source file", "source", "Projects/A/missing.md"},
		{"outside project", "source", "Resources/debug.md"},
		{"no project directory", "source", "Projects/debug.md"},
		{"non Markdown source", "source", "Projects/A/debug.txt"},
		{"source traversal", "source", "Projects/A/../../outside.md"},
		{"backslash traversal", "source", "Projects\\A\\..\\..\\outside.md"},
		{"source dot segment", "source", "Projects/A/./debug.md"},
		{"source duplicate separator", "source", "Projects//A/debug.md"},
		{"source POSIX absolute", "source", "/Projects/A/debug.md"},
		{"source Windows absolute", "source", "C:\\Projects\\A\\debug.md"},
		{"source UNC", "source", "\\\\server\\Projects\\A\\debug.md"},
		{"source alternate stream", "source", "Projects/A/debug.md:stream"},
		{"hidden project", "source", "Projects/.private/debug.md"},
		{"source wiki anchor", "source", "Projects/A/debug#anchor.md"},
		{"unknown mode", "mode", "replace"},
		{"missing folder", "folder", ""},
		{"target root", "folder", "Learning"},
		{"wrong target root", "folder", "Projects/Go"},
		{"nested book", "folder", "Learning/Go/advanced"},
		{"target traversal", "folder", "Learning/../outside"},
		{"target alternate stream", "folder", "Learning/Go:stream"},
		{"target hidden directory", "folder", "Learning/.Go"},
		{"target reserved directory", "folder", "Learning/NUL"},
		{"target trailing directory space", "folder", "Learning/Go "},
		{"blank title", "title", "  "},
		{"title traversal", "title", ".."},
		{"title forward slash", "title", "chapter/one"},
		{"title backslash", "title", "chapter\\one"},
		{"title absolute path", "title", "C:\\outside.md"},
		{"title stream", "title", "note:stream"},
		{"title wiki pipe", "title", "note|outside"},
		{"title wiki close", "title", "note]]"},
		{"title wiki open", "title", "[[note"},
		{"title anchor", "title", "note#heading"},
		{"title newline", "title", "note\nother"},
		{"title control", "title", "note\x00other"},
		{"title trailing dot", "title", "note."},
		{"title hidden file", "title", ".note"},
		{"reserved book metadata", "title", "BOOK.MD"},
		{"reserved import metadata", "title", "sources"},
		{"reserved learning plan", "title", "ai-plan"},
		{"reserved Windows device", "title", "CON.notes"},
		{"reserved serial port", "title", "COM1"},
		{"reserved printer port", "title", "LPT²"},
		{"blank summary", "summary", " \n "},
		{"NUL summary", "summary", "bad\x00text"},
		{"invalid UTF8 summary", "summary", "bad\xfftext"},
		{"wrong interview folder", "interview folder", "Learning/Go"},
		{"blank question", "question", " "},
		{"multiline question", "question", "问题\n### 伪造题卡"},
		{"comment question", "question", "问题<!-- 隐藏答案"},
		{"control question", "question", "问题\v隐藏"},
		{"Unicode multiline question", "question", "问题\u2028第二行"},
		{"empty answer", "answer", " \n "},
		{"NUL answer", "answer", "bad\x00text"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, base.SourceFile, "# 用户原稿\r\n不可覆盖  \r\n")
			workbenchWrite(t, root, "Projects/A/debug.txt", "not Markdown")
			workbenchWrite(t, root, "Resources/debug.md", "other note")
			req := base
			if strings.HasPrefix(test.field, "interview") || test.field == "question" || test.field == "answer" {
				req.TargetMode, req.TargetFolder, req.Question, req.Answer = "interview", "Learning/面试宝典", "为什么？", "因为有实战依据。"
			}
			switch test.field {
			case "source":
				req.SourceFile = test.value
			case "mode":
				req.TargetMode = test.value
			case "folder", "interview folder":
				req.TargetFolder = test.value
			case "title":
				req.TargetTitle = test.value
			case "summary":
				req.Summary = test.value
			case "question":
				req.Question = test.value
			case "answer":
				req.Answer = test.value
			}
			before := distillFiles(t, root)
			if err := NewTodoService().DistillKnowledge(root, req); err == nil {
				t.Fatalf("invalid request was accepted: %#v", req)
			}
			if got := distillFiles(t, root); !reflect.DeepEqual(got, before) {
				t.Fatalf("rejected request changed workspace files: before=%#v after=%#v", before, got)
			}
		})
	}
}

func TestDistillRejectsUnreadableParticipantsBeforeWriting(t *testing.T) {
	for _, blocked := range []string{"Projects/A/debug.md", "Learning/Go/排错.md", "Learning/Go/book.md", "Learning"} {
		t.Run(blocked, func(t *testing.T) {
			root := t.TempDir()
			if blocked != "Projects/A/debug.md" {
				workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
			}
			if blocked == "Learning" {
				workbenchWrite(t, root, blocked, "a file blocks the learning directory")
			} else if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(blocked)), 0750); err != nil {
				t.Fatal(err)
			}
			before := distillFiles(t, root)
			err := NewTodoService().DistillKnowledge(root, DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "保留结论。"})
			if err == nil || !reflect.DeepEqual(distillFiles(t, root), before) {
				t.Fatalf("a failed participant must leave all files unchanged: %v", err)
			}
		})
	}
}

func TestDistillRejectsSymlinkedPaths(t *testing.T) {
	for _, linked := range []string{"Projects/A", "Projects/A/debug.md", "Learning/Go", "Learning/Go/排错.md", "Learning/Go/book.md"} {
		t.Run(linked, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			workbenchWrite(t, outside, "keep.md", "outside content must stay unchanged")
			workbenchWrite(t, outside, "debug.md", "# outside source")
			if !strings.HasPrefix(linked, "Projects/") {
				workbenchWrite(t, root, "Projects/A/debug.md", "# inside source\n")
			}
			link := filepath.Join(root, filepath.FromSlash(linked))
			if err := os.MkdirAll(filepath.Dir(link), 0750); err != nil {
				t.Fatal(err)
			}
			target := outside
			if strings.HasSuffix(linked, ".md") {
				target = filepath.Join(outside, "keep.md")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("this environment cannot create symlinks: %v", err)
			}
			before := distillFiles(t, root)
			err := NewTodoService().DistillKnowledge(root, DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "结论"})
			if err == nil || !reflect.DeepEqual(distillFiles(t, root), before) || workbenchRead(t, outside, "keep.md") != "outside content must stay unchanged" || workbenchRead(t, outside, "debug.md") != "# outside source" {
				t.Fatalf("symbolic links must not grant write access outside the workspace: %v", err)
			}
		})
	}
}

func TestDistillMarkersInExamplesDoNotAuthorizeChapterReuse(t *testing.T) {
	for _, wrap := range []string{"```md\n%s\n```", "`%s`", "    %s", "> %s", "<!-- example\n%s\n-->", "---\nexample: '%s'\n---", "example %s suffix"} {
		t.Run(wrap, func(t *testing.T) {
			root := t.TempDir()
			req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "正文"}
			workbenchWrite(t, root, req.SourceFile, "# 原稿\n")
			if err := NewTodoService().DistillKnowledge(root, req); err != nil {
				t.Fatal(err)
			}
			chapterPath := "Learning/Go/排错.md"
			marker := regexp.MustCompile(`(?m)^<!-- notevault-distill: .* -->$`).FindString(workbenchRead(t, root, chapterPath))
			if marker == "" {
				t.Fatal("distillation must leave a portable retry marker")
			}
			manual := strings.Replace(wrap, "%s", marker, 1) + "\n\nA different manual chapter.\n"
			workbenchWrite(t, root, chapterPath, manual)
			before := distillFiles(t, root)
			if err := NewTodoService().DistillKnowledge(root, req); err == nil {
				t.Fatal("a marker copied into an example must not make an unrelated chapter look completed")
			}
			if got := distillFiles(t, root); !reflect.DeepEqual(got, before) {
				t.Fatalf("an accidental marker caused a file write: %#v", got)
			}
		})
	}
}

func TestDistillRejectsChangedBookRequestAtExistingTitle(t *testing.T) {
	root := t.TempDir()
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "原始结论"}
	workbenchWrite(t, root, req.SourceFile, "# 原稿\n")
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	before := distillFiles(t, root)
	req.Summary = "不同的沉淀内容，不能借重试覆盖已有章节。"
	if err := NewTodoService().DistillKnowledge(root, req); err == nil || !reflect.DeepEqual(distillFiles(t, root), before) {
		t.Fatalf("a changed request is a chapter collision, not permission to overwrite: %v", err)
	}
}

func TestDistillRejectsHiddenOrSplitInterviewContent(t *testing.T) {
	for _, test := range []struct{ name, source, topic, answer string }{
		{"source fence", "# source\n```md\nopen", "# topic\n", "回答"},
		{"source comment", "# source\n<!-- open", "# topic\n", "回答"},
		{"source multiple comments", "# source\n<!-- closed --> <!-- open", "# topic\n", "回答"},
		{"topic fence", "# source\n", "# topic\n~~~md\nopen", "回答"},
		{"topic comment", "# source\n", "# topic\n<!-- open", "回答"},
		{"answer fence", "# source\n", "# topic\n", "回答\n```go\nopen"},
		{"answer comment", "# source\n", "# topic\n", "回答\n<!-- open"},
		{"answer exits question", "# source\n", "# topic\n", "回答\n### 另一个问题\n不能丢失这部分回答"},
		{"answer injects SRS", "# source\n", "# topic\n", "回答\n<!-- srs: {\"level\":\"不会\",\"reps\":7} -->\n额外卡片"},
		{"answer injects invalid SRS", "# source\n", "# topic\n", "回答\n<!-- srs: {broken} -->"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, "Projects/A/debug.md", test.source)
			workbenchWrite(t, root, "Learning/面试宝典/Go.md", test.topic)
			before := distillFiles(t, root)
			err := NewTodoService().DistillKnowledge(root, DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "为什么？", Answer: test.answer})
			if err == nil || !reflect.DeepEqual(distillFiles(t, root), before) {
				t.Fatalf("hidden or ambiguous content must not be reported as a saved card: %v", err)
			}
		})
	}
}

func TestDistillInterviewSupportsCSharpQuestions(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "语言", Question: "如何比较 Go 和 C#", Answer: "按实际项目的并发需求分析。"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Cards) != 1 || !strings.HasSuffix(snapshot.Cards[0].Question, "Go 和 C#") {
		t.Fatalf("a hash within an answer language name is not a closing heading marker: %#v", snapshot.Cards)
	}
}

func TestDistillKeepsSafeDeviceLikeNamesAndUnicodeTitles(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/debug.markdown", "# 原稿\n")
	for _, title := range []string{"COM10", "LPT10", "COMLPT1", "console", "C++ 队列", "① 前言", "1.2-进阶"} {
		t.Run(title, func(t *testing.T) {
			err := NewTodoService().DistillKnowledge(root, DistillRequest{SourceFile: "Projects/A/debug.markdown", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: title + ".markdown", Summary: "应保留合法标题。"})
			if err != nil {
				t.Fatalf("safe filename rejected: %v", err)
			}
			if got := workbenchRead(t, root, "Learning/Go/"+title+".md"); !strings.Contains(got, "应保留合法标题。") {
				t.Fatalf("safe title did not reach its expected chapter: %q", got)
			}
		})
	}
}

func TestDistillInterviewPreservesCRLFAndAllowsFencedSRSExamples(t *testing.T) {
	root := t.TempDir()
	source := "# 项目原稿\n保留源文件换行。\n"
	topic := "\ufeff---\r\ncustom: 保留\r\n---\r\n# 题库\r\n\r\n手写说明  "
	workbenchWrite(t, root, "Projects/A/debug.md", source)
	workbenchWrite(t, root, "Learning/面试宝典/Go.md", topic)
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "如何使用 <-chan 和 context<T>？", Answer: "回答和示例\n\n```md\n### [Qexample] 仅为示例\n<!-- srs: {\"level\":\"不会\"} -->\n```\n\n#### 补充\n保持格式。"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	updated := workbenchRead(t, root, "Learning/面试宝典/Go.md")
	if !strings.HasPrefix(updated, topic+"\r\n") || strings.Count(updated, "\n") != strings.Count(updated, "\r\n") {
		t.Fatalf("new card must match its topic's CRLF while preserving the BOM and old text: %q", updated)
	}
	if updatedSource := workbenchRead(t, root, "Projects/A/debug.md"); !strings.HasPrefix(updatedSource, source) || strings.Contains(updatedSource, "\r") {
		t.Fatalf("target CRLF must not leak into the source: %q", updatedSource)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Cards) != 1 || len(snapshot.Warnings) != 0 || !strings.Contains(strings.ReplaceAll(snapshot.Cards[0].Answer, "\r\n", "\n"), req.Answer) {
		t.Fatalf("SRS code examples are ordinary answer text: cards=%#v warnings=%#v", snapshot.Cards, snapshot.Warnings)
	}
}

func TestDistillInterviewUsesDistinctIDsAndIgnoresInactiveFields(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
	service := NewTodoService()
	requests := []DistillRequest{
		{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "怎样取消？", Answer: "用 context。"},
		{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "怎样限制并发？", Answer: "用 WorkerPool。"},
	}
	start := make(chan struct{})
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		req := requests[i%len(requests)]
		req.Summary = strings.Repeat("在另一模式填写的正文", i)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- service.DistillKnowledge(root, req)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Cards) != 2 || snapshot.Cards[0].Question == snapshot.Cards[1].Question || strings.Split(snapshot.Cards[0].Question, "]")[0] == strings.Split(snapshot.Cards[1].Question, "]")[0] {
		t.Fatalf("different questions need unique IDs, while duplicate clicks reuse each card: %#v", snapshot.Cards)
	}
	if got := workbenchRead(t, root, "Projects/A/debug.md"); strings.Count(got, "技术资产沉淀") != 2 {
		t.Fatalf("duplicate requests must not duplicate source badges: %q", got)
	}
}

func TestDistillInterviewRejectsExistingQuestionIDWithoutMarker(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "为什么？", Answer: "原始答案"}
	if err := NewTodoService().DistillKnowledge(root, req); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, time.Now().Format("2006-01-02"))
	if len(snapshot.Cards) != 1 {
		t.Fatalf("expected one card: %#v", snapshot.Cards)
	}
	card := snapshot.Cards[0]
	manual := "# 手工题库\n### " + strings.Split(card.Question, "]")[0] + "] 新的题面\n" + card.Comment + "\n自己改写的答案。\n"
	workbenchWrite(t, root, card.FilePath, manual)
	before := distillFiles(t, root)
	if err := NewTodoService().DistillKnowledge(root, req); err == nil || !reflect.DeepEqual(distillFiles(t, root), before) {
		t.Fatalf("an occupied question ID without its request marker must not be duplicated or overwritten: %v", err)
	}
}

func TestDistillRepairsMissingTargetWithoutDuplicatingSourceBadge(t *testing.T) {
	for _, mode := range []string{"book", "interview"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
			req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: mode, TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "书架结论", Question: "为什么？", Answer: "面试答案"}
			if mode == "interview" {
				req.TargetFolder = "Learning/面试宝典"
			}
			if err := NewTodoService().DistillKnowledge(root, req); err != nil {
				t.Fatal(err)
			}
			source := workbenchRead(t, root, req.SourceFile)
			targetPath := req.TargetFolder + "/排错.md"
			target := workbenchRead(t, root, targetPath)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(targetPath))); err != nil {
				t.Fatal(err)
			}
			if err := NewTodoService().DistillKnowledge(root, req); err != nil {
				t.Fatal(err)
			}
			if got := workbenchRead(t, root, req.SourceFile); got != source {
				t.Fatalf("target recovery must leave the existing source badge alone: %q", got)
			}
			if got := workbenchRead(t, root, targetPath); got != target {
				t.Fatalf("the incomplete request must reconstruct its target: %q", got)
			}
		})
	}
}

func TestDistillCommitRejectsConcurrentEdits(t *testing.T) {
	for _, changedPath := range []string{"Projects/A/debug.md", "Learning/Go/排错.md", "Learning/Go/book.md"} {
		t.Run(changedPath, func(t *testing.T) {
			root := t.TempDir()
			workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
			workbenchWrite(t, root, "Learning/Go/book.md", "# Go\n保留分册说明。\n")
			changes, err := prepareWorkbenchDistill(root, DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "book", TargetFolder: "Learning/Go", TargetTitle: "排错", Summary: "沉淀内容"}, time.Date(2026, 9, 9, 12, 0, 0, 0, time.Local))
			if err != nil {
				t.Fatal(err)
			}
			workbenchWrite(t, root, changedPath, "在预备与落盘之间由其他编辑器写入的新内容。\n")
			before := distillFiles(t, root)
			if err := commitWorkbenchDistill(changes); err == nil || !reflect.DeepEqual(distillFiles(t, root), before) {
				t.Fatalf("a concurrent edit must reject the whole prepared mutation without overwriting it: %v", err)
			}
		})
	}
}

func TestDistillCommitRollsBackAFailedLaterWrite(t *testing.T) {
	root := t.TempDir()
	// The first write makes the second write's parent a file. This induces an
	// actual mid-commit filesystem failure without permission or timing tricks.
	first := filepath.Join(root, "target.md")
	changes := []workbenchChange{
		{full: first, relative: "target.md", after: "new chapter", mode: 0644},
		{full: filepath.Join(first, "source.md"), relative: "target.md/source.md", after: "source badge", mode: 0644},
	}
	err := commitWorkbenchDistill(changes)
	if err == nil || !strings.Contains(err.Error(), "未全部保存") {
		t.Fatalf("partial write failure needs an honest caller-visible error: %v", err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("an unchanged first write must be rolled back after the second fails: %v", err)
	}
}

func TestDistillInterviewDueDateCrossesLeapDay(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Projects/A/debug.md", "# 原稿\n")
	req := DistillRequest{SourceFile: "Projects/A/debug.md", TargetMode: "interview", TargetFolder: "Learning/面试宝典", TargetTitle: "Go", Question: "如何测试日期？", Answer: "覆盖闰年与跨月场景。"}
	changes, err := prepareWorkbenchDistill(root, req, time.Date(2024, 2, 25, 23, 59, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if err := commitWorkbenchDistill(changes); err != nil {
		t.Fatal(err)
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2024-02-25")
	if len(snapshot.Cards) != 1 || snapshot.Cards[0].Due != "2024-03-03" || snapshot.Cards[0].Reps != 1 || snapshot.Cards[0].Interval != 7 {
		t.Fatalf("the initial review is seven calendar days later, including leap day: %#v", snapshot.Cards)
	}
}

func distillFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err == nil {
				files[filepath.ToSlash(relative)] = "symlink:" + target
			}
			return err
		}
		content, err := os.ReadFile(full)
		if err == nil {
			files[filepath.ToSlash(relative)] = string(content)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
