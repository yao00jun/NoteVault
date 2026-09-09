package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbenchShowsOriginalPDFsWithoutInventingChapters(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Learning/面试宝典/book.md", "# 面试宝典\n")
	workbenchWrite(t, root, "Learning/面试宝典/paper.pdf", "original PDF bytes")
	workbenchWrite(t, root, "Learning/面试宝典/.hidden/private.pdf", "hidden")
	workbenchWrite(t, root, "Learning/面试宝典/ai-plan.md", "# Suggestions")
	workbenchWrite(t, root, "Learning/面试宝典/sources.md", "# Sources")
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-09")
	data, _ := json.Marshal(snapshot.Books[0])
	var book struct {
		Attachments                      []struct{ Path, Name, NotePath string }
		StudyPlanPath, SourceRecordsPath string
	}
	_ = json.Unmarshal(data, &book)
	if len(book.Attachments) != 1 || book.Attachments[0].Path != "Learning/面试宝典/paper.pdf" || book.Attachments[0].NotePath != "" {
		t.Fatalf("original PDFs must be visible even without Markdown chapters: %s", data)
	}
	if snapshot.Books[0].NoteCount != 0 || book.StudyPlanPath == "" || book.SourceRecordsPath == "" {
		t.Fatalf("auxiliary documents must be accessible separately: %s", data)
	}
}

func TestSourceImportReextractsStoredPDFsWithoutDuplicatingOrOverwriting(t *testing.T) {
	root := t.TempDir()
	data := sourceTestCompressedPDF(t, "")
	workbenchWrite(t, root, "Learning/面试宝典/book.md", "# My existing book\n")
	workbenchWrite(t, root, "Learning/面试宝典/paper.pdf", string(data))
	workbenchWrite(t, root, "Learning/面试宝典/own.md", "# Hand written note")
	request := SourceImportRequest{SourceType: "attachments", Kind: "book", Source: "Learning/面试宝典"}
	service := NewImportServiceWithTasks(NewTaskService(nil))
	result := runSourceImport(t, service, root, request)
	if result.Imported != 1 || !strings.Contains(sourceRead(t, root, "Learning/面试宝典/paper.md"), "Readable compressed PDF chapter.") {
		t.Fatalf("stored PDF must become a readable chapter: %+v", result)
	}
	if sourceRead(t, root, "Learning/面试宝典/paper.pdf") != string(data) || sourceRead(t, root, "Learning/面试宝典/book.md") != "# My existing book\n" {
		t.Fatal("re-extraction changed original files")
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-09")
	encoded, _ := json.Marshal(snapshot.Books[0])
	if !strings.Contains(string(encoded), `"notePath":"Learning/面试宝典/paper.md"`) {
		t.Fatalf("attachment must link to its extracted note: %s", encoded)
	}
	workbenchWrite(t, root, "Learning/面试宝典/paper.md", "# Edited chapter")
	again := runSourceImport(t, service, root, request)
	if again.Imported != 0 || sourceRead(t, root, "Learning/面试宝典/paper.md") != "# Edited chapter" {
		t.Fatal("re-extraction must preserve edits and avoid duplicates")
	}
	for _, invalid := range []string{"../outside", "Projects/demo", "Learning", "Learning/面试宝典/../other"} {
		request.Source = invalid
		if _, err := service.StartSourceImport(root, request); err == nil {
			t.Errorf("accepted invalid stored source %q", invalid)
		}
	}
	request.Source, request.TargetFolder = "Learning/面试宝典", "Learning/Another"
	if _, err := service.StartSourceImport(root, request); err == nil {
		t.Fatal("stored source must remain in its owning book")
	}
}

func TestSourceImportPDFResultsDistinguishTextFromAttachments(t *testing.T) {
	root := t.TempDir()
	request := SourceImportRequest{SourceType: "files", Kind: "book", Name: "PDFs", Files: []SourceImportFile{sourceTestFile("readable.pdf", string(sourceTestCompressedPDF(t, ""))), sourceTestFile("scan.pdf", string(sourceTestPDF(t, "")))}}
	result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), root, request)
	encoded, _ := json.Marshal(result)
	var counts struct{ Readable, Attachments int }
	_ = json.Unmarshal(encoded, &counts)
	if counts.Readable != 1 || counts.Attachments != 1 {
		t.Fatalf("saved attachments are not extracted text: %s", encoded)
	}
}

func TestWorkbenchAttachmentScanSkipsSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	workbenchWrite(t, root, "Learning/Books/book.md", "# Books")
	workbenchWrite(t, outside, "secret.pdf", "outside")
	if err := os.Symlink(filepath.Join(outside, "secret.pdf"), filepath.Join(root, "Learning", "Books", "link.pdf")); err != nil {
		t.Skip("symlinks unavailable")
	}
	snapshot := workbenchSnapshot(t, NewTodoService(), root, "2026-09-09")
	encoded, _ := json.Marshal(snapshot.Books[0])
	if strings.Contains(string(encoded), "link.pdf") {
		t.Fatal("exposed external attachment")
	}
}

func TestResolveWorkspaceAttachmentRestrictsPathsAndFileTypes(t *testing.T) {
	root := t.TempDir()
	workbenchWrite(t, root, "Learning/Books/paper.pdf", "PDF bytes")
	workbenchWrite(t, root, "Learning/Books/run.exe", "executable")
	full, err := ResolveWorkspaceAttachment(root, "Learning/Books/paper.pdf")
	if err != nil || full != filepath.Join(root, "Learning", "Books", "paper.pdf") {
		t.Fatal(full, err)
	}
	for _, invalid := range []string{"../paper.pdf", "Learning/Books/run.exe", "Learning/Books/missing.pdf", "Learning/Books/paper.pdf:other", "Learning/Books"} {
		if _, err := ResolveWorkspaceAttachment(root, invalid); err == nil {
			t.Errorf("accepted invalid attachment path %q", invalid)
		}
	}
}

func TestSourceImportAttachmentOnlyDoesNotGenerateUnfoundedAIPlan(t *testing.T) {
	root := t.TempDir()
	request := SourceImportRequest{SourceType: "files", Kind: "book", Name: "Scans", Enrich: true, Files: []SourceImportFile{sourceTestFile("scan.pdf", string(sourceTestPDF(t, "")))}}
	result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), root, request)
	if !strings.Contains(strings.Join(result.Warnings, " "), "已跳过 AI 整理") {
		t.Fatalf("missing no-text explanation: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "Learning", "Scans", "ai-plan.md")); !os.IsNotExist(err) {
		t.Fatal("attachment-only import invented an AI plan")
	}
}
