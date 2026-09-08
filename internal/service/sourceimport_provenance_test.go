package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type sourceProvenanceTestEntry struct {
	Origin         string `json:"origin"`
	Source         string `json:"source"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	SourceSHA256   string `json:"sourceSha256"`
	ExtractionMode string `json:"extractionMode,omitempty"`
	Warning        string `json:"warning,omitempty"`
}

type sourceProvenanceTestGenerated struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Role   string `json:"role"`
}

type sourceProvenanceTestLedger struct {
	Version          int                             `json:"version"`
	Entries          []sourceProvenanceTestEntry     `json:"entries"`
	Generated        []sourceProvenanceTestGenerated `json:"generated,omitempty"`
	PendingGenerated []sourceProvenanceTestGenerated `json:"pendingGenerated,omitempty"`
}

func sourceProvenanceLedger(t *testing.T, workspace, target string) sourceProvenanceTestLedger {
	t.Helper()
	content := sourceRead(t, workspace, path.Join(target, sourceLedgerName))
	start := strings.Index(content, sourceManifestStart)
	if start < 0 {
		t.Fatal("missing durable source ledger")
	}
	content = content[start+len(sourceManifestStart):]
	end := strings.Index(content, "\n-->")
	var ledger sourceProvenanceTestLedger
	if end < 0 || json.Unmarshal([]byte(content[:end]), &ledger) != nil || ledger.Version != 1 {
		t.Fatalf("invalid v1 source ledger: %s", content)
	}
	return ledger
}

func sourceProvenanceWriteLedger(t *testing.T, workspace, target string, ledger sourceProvenanceTestLedger) {
	t.Helper()
	encoded, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	workbenchWrite(t, workspace, path.Join(target, sourceLedgerName), sourceManifestStart+string(encoded)+"\n-->\n")
}

func sourceProvenanceCheckGenerated(t *testing.T, ledger sourceProvenanceTestLedger, relative, content, role string) {
	t.Helper()
	for _, record := range ledger.Generated {
		if record.Path == relative {
			if record.Role != role || record.SHA256 != sourceChecksum([]byte(content)) {
				t.Fatalf("incorrect generated addition: %+v", record)
			}
			return
		}
	}
	t.Fatalf("missing generated addition for %s: %+v", relative, ledger.Generated)
}

func TestSourceImportFinalExtractionModesSurviveRestart(t *testing.T) {
	workspace := t.TempDir()
	target := "Resources/Modes"
	request := SourceImportRequest{SourceType: "files", Kind: "topic", Name: "Modes", Files: []SourceImportFile{
		sourceTestFile("original.md", "\ufeff# Original\r\n"),
		sourceTestFile("plain.txt", "Plain text"),
		sourceTestFile("main.go", "package main"),
		sourceTestFile("page.html", "<h1>HTML text</h1>"),
		sourceTestFile("paper.pdf", string(sourceTestPDF(t, "PDF text"))),
		sourceTestFile("audio.mp3", "\xff\x00audio"),
	}}
	svc := NewImportServiceWithTasks(NewTaskService(nil))
	first := runSourceImport(t, svc, workspace, request)
	if first.Imported != len(request.Files) || first.Skipped != 0 || len(first.Warnings) != 0 {
		t.Fatalf("successful extraction/preservation counts: %+v", first)
	}
	modes := map[string]string{"original.md": "markdown-preserved", "plain.txt": "text", "main.go": "source-code", "page.html": "html", "paper.pdf": "pdf-text", "audio.mp3": "attachment"}
	ledger := sourceProvenanceLedger(t, workspace, target)
	if len(ledger.Entries) != len(modes) {
		t.Fatalf("missing source provenance: %+v", ledger)
	}
	for _, entry := range ledger.Entries {
		if mode, ok := modes[entry.Source]; !ok || entry.ExtractionMode != mode || entry.Warning != "" {
			t.Errorf("incorrect persisted extraction mode: %+v", entry)
		}
		content := sourceRead(t, workspace, entry.Path)
		if entry.SHA256 != sourceChecksum([]byte(content)) {
			t.Errorf("hash does not describe saved output: %+v", entry)
		}
	}
	sourceProvenanceCheckGenerated(t, ledger, target+"/index.md", sourceRead(t, workspace, target+"/index.md"), "collection-metadata")
	if sourceRead(t, workspace, target+"/original.md") != "\ufeff# Original\r\n" || sourceRead(t, workspace, target+"/audio.mp3") != "\xff\x00audio" {
		t.Fatal("preserved sources changed bytes")
	}
	before := sourceRead(t, workspace, target+"/sources.md")
	second := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if second.Imported != 0 || second.Skipped != len(request.Files) || sourceRead(t, workspace, target+"/sources.md") != before {
		t.Fatalf("fresh service did not reuse durable extraction provenance: %+v", second)
	}
}

func TestSourceImportFinalUnreadablePDFsAreDurableAttachments(t *testing.T) {
	scan := sourceTestPDF(t, "")
	encrypted := bytes.Replace(scan, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Encrypt 4 0 R"), 1)
	badStream := sourceTestPDF(t, "", func(objects []string) {
		objects[4] = "<< /Length 8 /Filter /FlateDecode >>\nstream\nnot-zlib\nendstream"
	})
	unsafeReference := sourceTestPDF(t, "", func(objects []string) {
		objects[1] = "<< /Type /Pages /Kids [2 0 R] /Count 1 >>"
	})
	for name, data := range map[string][]byte{"scan": scan, "encrypted": encrypted, "malformed": []byte("%PDF-broken"), "bad-stream": badStream, "unsafe-reference": unsafeReference, "unverified-xref": sourceReviewCyclicPDF()} {
		t.Run(name, func(t *testing.T) {
			workspace := t.TempDir()
			target := "Learning/Fallback"
			request := SourceImportRequest{SourceType: "files", Kind: "book", Name: "Fallback", ConflictStrategy: "copy", Files: []SourceImportFile{sourceTestFile(name+".pdf", string(data))}}
			svc := NewImportServiceWithTasks(NewTaskService(nil))
			preview, err := svc.PreviewSourceImport(workspace, request)
			if err != nil || preview.FileCount != 1 || len(preview.Warnings) == 0 {
				t.Fatalf("PDF attachment must remain visible in preview: %+v (%v)", preview, err)
			}
			first := runSourceImport(t, svc, workspace, request)
			if first.Imported != 1 || first.Skipped != 0 || len(first.Warnings) == 0 || sourceRead(t, workspace, target+"/"+name+".pdf") != string(data) {
				t.Fatalf("unreadable PDF was not preserved exactly: %+v", first)
			}
			if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(target+"/"+name+".md"))); !os.IsNotExist(err) {
				t.Fatal("unreadable PDF fabricated Markdown")
			}
			ledger := sourceProvenanceLedger(t, workspace, target)
			if len(ledger.Entries) != 1 || ledger.Entries[0].ExtractionMode != "attachment" || ledger.Entries[0].Warning == "" || ledger.Entries[0].SHA256 != sourceChecksum(data) {
				t.Fatalf("PDF fallback lacks durable attachment provenance: %+v", ledger)
			}
			if !strings.Contains(strings.Join(first.Warnings, "\n"), ledger.Entries[0].Warning) {
				t.Fatal("persistent warning must explain the actual preservation outcome")
			}
			before := sourceRead(t, workspace, target+"/sources.md")
			second := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if second.Imported != 0 || second.Skipped != 1 || len(second.Files) != 1 || second.Files[0] != first.Files[0] || sourceRead(t, workspace, target+"/sources.md") != before {
				t.Fatalf("restart duplicated a preserved PDF: %+v", second)
			}
		})
	}
}

func TestSourceImportFinalStandaloneImagesExplainOCR(t *testing.T) {
	workspace := t.TempDir()
	const data = "\x89PNG\r\n\x1a\n\x00\xffimage"
	request := SourceImportRequest{SourceType: "files", Kind: "topic", Name: "Image", Files: []SourceImportFile{sourceTestFile("scan.png", data)}}
	first := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if first.Imported != 1 || first.Skipped != 0 || !strings.Contains(strings.Join(first.Warnings, " "), "OCR") || sourceRead(t, workspace, "Resources/Image/scan.png") != data {
		t.Fatalf("standalone image must preserve bytes and explain OCR: %+v", first)
	}
	ledger := sourceProvenanceLedger(t, workspace, "Resources/Image")
	if len(ledger.Entries) != 1 || ledger.Entries[0].ExtractionMode != "attachment" || !strings.Contains(ledger.Entries[0].Warning, "OCR") {
		t.Fatalf("image OCR limitation was not persisted: %+v", ledger)
	}
	second := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if second.Imported != 0 || second.Skipped != 1 || sourceRead(t, workspace, "Resources/Image/scan.png") != data {
		t.Fatalf("image retry changed saved attachment: %+v", second)
	}
}

func TestSourceImportFinalNewMetadataIsRecordedForEmptyAndAdopt(t *testing.T) {
	for _, tc := range []struct{ sourceType, kind, target, metadata string }{
		{"empty", "project", "Projects/New", "project.md"},
		{"empty", "book", "Learning/New", "book.md"},
		{"empty", "topic", "Resources/New", "index.md"},
		{"adopt", "book", "Learning/New", "book.md"},
	} {
		t.Run(tc.sourceType+"-"+tc.kind, func(t *testing.T) {
			workspace := t.TempDir()
			request := SourceImportRequest{SourceType: tc.sourceType, Kind: CollectionKind(tc.kind), Name: "New"}
			if tc.sourceType == "adopt" {
				request.Source = tc.target
				workbenchWrite(t, workspace, tc.target+"/chapter.md", "\ufeff# Authored\r\n")
				workbenchWrite(t, workspace, tc.target+"/sources.md", "# My source notes\r\nKeep me.\r\n")
			}
			first := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			ledger := sourceProvenanceLedger(t, workspace, tc.target)
			if first.Imported != 0 || len(ledger.Entries) != 0 || len(ledger.Generated) != 1 || len(ledger.PendingGenerated) != 0 {
				t.Fatalf("metadata-only initialization provenance: %+v; %+v", first, ledger)
			}
			sourceProvenanceCheckGenerated(t, ledger, tc.target+"/"+tc.metadata, sourceRead(t, workspace, first.MetadataPath), "collection-metadata")
			if tc.sourceType == "adopt" && (sourceRead(t, workspace, tc.target+"/chapter.md") != "\ufeff# Authored\r\n" || !strings.HasPrefix(sourceRead(t, workspace, tc.target+"/sources.md"), "# My source notes\r\nKeep me.\r\n")) {
				t.Fatal("adoption changed original notes")
			}
			before := sourceRead(t, workspace, tc.target+"/sources.md")
			runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if sourceRead(t, workspace, tc.target+"/sources.md") != before {
				t.Fatal("metadata retry rewrote its confirmed generation")
			}
		})
	}
}

func TestSourceImportFinalLegacyLedgerPreservesAuthoredMetadataAndGuidance(t *testing.T) {
	workspace, target := t.TempDir(), "Projects/Legacy"
	request := SourceImportRequest{SourceType: "files", Kind: "project", Name: "Legacy", Enrich: true, Files: []SourceImportFile{sourceTestFile("new.txt", "New source")}, AI: SourceImportAIConfig{BaseURL: "http://localhost:1234", Model: "unused"}}
	metadata := string(sourceCollectionMetadata(request, nil))
	guidance := "# Authored guidance\r\n- [x] Real history\r\n"
	workbenchWrite(t, workspace, target+"/project.md", metadata)
	workbenchWrite(t, workspace, target+"/ai-plan.md", guidance)
	workbenchWrite(t, workspace, target+"/legacy.md", "# User edit\r\n")
	original := sourceProvenanceTestEntry{Origin: "upload:legacy.md", Source: "legacy.md", Path: target + "/legacy.md", SHA256: sourceChecksum([]byte("# Original")), SourceSHA256: sourceChecksum([]byte("# Original"))}
	sourceProvenanceWriteLedger(t, workspace, target, sourceProvenanceTestLedger{Version: 1, Entries: []sourceProvenanceTestEntry{original}})
	block := sourceRead(t, workspace, target+"/sources.md")
	workbenchWrite(t, workspace, target+"/sources.md", "# Authored prefix\r\n"+block+"\r\nAuthored suffix.\r\n")
	runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	ledger := sourceProvenanceLedger(t, workspace, target)
	if len(ledger.Entries) != 2 || ledger.Entries[0] != original || len(ledger.Generated) != 0 || len(ledger.PendingGenerated) != 0 {
		t.Fatalf("legacy/authored files were reclassified: %+v", ledger)
	}
	if sourceRead(t, workspace, target+"/project.md") != metadata || sourceRead(t, workspace, target+"/ai-plan.md") != guidance || sourceRead(t, workspace, target+"/legacy.md") != "# User edit\r\n" {
		t.Fatal("legacy import rewrote authoritative notes")
	}
	after := sourceRead(t, workspace, target+"/sources.md")
	if !strings.HasPrefix(after, "# Authored prefix\r\n") || !strings.HasSuffix(after, "\r\nAuthored suffix.\r\n") {
		t.Fatal("manifest update changed surrounding prose")
	}
}

func TestSourceImportFinalGeneratedRecoveryOnlyConfirmsReservedBytes(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching", true: "edited"}[edited], func(t *testing.T) {
			workspace, target := t.TempDir(), "Projects/Recovery"
			request := SourceImportRequest{SourceType: "empty", Kind: "project", Name: "Recovery", Enrich: true, AI: SourceImportAIConfig{BaseURL: "http://localhost:1234", Model: "unused"}}
			metadata, guidance := string(sourceCollectionMetadata(request, nil)), "# Original generated suggestion\n"
			pending := []sourceProvenanceTestGenerated{{Path: target + "/project.md", SHA256: sourceChecksum([]byte(metadata)), Role: "collection-metadata"}, {Path: target + "/ai-plan.md", SHA256: sourceChecksum([]byte(guidance)), Role: "ai-guidance"}}
			if edited {
				metadata += "\r\nMy own metadata edit.\r\n"
				guidance += "\r\nMy own suggestion edit.\r\n"
			}
			workbenchWrite(t, workspace, target+"/project.md", metadata)
			workbenchWrite(t, workspace, target+"/ai-plan.md", guidance)
			sourceProvenanceWriteLedger(t, workspace, target, sourceProvenanceTestLedger{Version: 1, Entries: []sourceProvenanceTestEntry{}, PendingGenerated: pending})
			result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			ledger := sourceProvenanceLedger(t, workspace, target)
			if len(ledger.PendingGenerated) != 0 || sourceRead(t, workspace, target+"/project.md") != metadata || sourceRead(t, workspace, target+"/ai-plan.md") != guidance {
				t.Fatalf("interrupted generation did not reconcile safely: %+v", ledger)
			}
			if edited {
				if len(ledger.Generated) != 0 || len(result.Warnings) == 0 {
					t.Fatalf("edited pending files were mislabeled as confirmed generation: %+v; %+v", ledger, result)
				}
			} else {
				if len(ledger.Generated) != 2 {
					t.Fatalf("saved generated additions were not recovered: %+v", ledger)
				}
				sourceProvenanceCheckGenerated(t, ledger, target+"/project.md", metadata, "collection-metadata")
				sourceProvenanceCheckGenerated(t, ledger, target+"/ai-plan.md", guidance, "ai-guidance")
				workbenchWrite(t, workspace, target+"/project.md", metadata+"\nLater user edit.\n")
				workbenchWrite(t, workspace, target+"/ai-plan.md", guidance+"\nLater user edit.\n")
				runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
				again := sourceProvenanceLedger(t, workspace, target)
				sourceProvenanceCheckGenerated(t, again, target+"/project.md", metadata, "collection-metadata")
				sourceProvenanceCheckGenerated(t, again, target+"/ai-plan.md", guidance, "ai-guidance")
			}
		})
	}
}

func TestSourceImportFinalAIGenerationLedgerFailureIsNotSuccess(t *testing.T) {
	workspace, target := t.TempDir(), "Projects/AI Failure"
	const secret = "never-store-ledger-failure-key"
	savedLedgers := make(chan string, 1)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			savedLedger := sourceRead(t, workspace, target+"/sources.md")
			workbenchWrite(t, workspace, target+"/sources.md", sourceManifestStart+"broken\n-->\n")
			savedLedgers <- savedLedger
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": "Tentative suggestion\n" + secret}}}})
	}))
	defer server.Close()
	request := SourceImportRequest{SourceType: "files", Kind: "project", Name: "AI Failure", Enrich: true, Files: []SourceImportFile{sourceTestFile("source.md", "# Actual source")}, AI: SourceImportAIConfig{APIKey: secret, BaseURL: server.URL, Model: "test"}}
	svc := NewImportServiceWithTasks(NewTaskService(nil))
	id, err := svc.StartSourceImport(workspace, request)
	if err != nil {
		t.Fatal(err)
	}
	svc.tasks.Wait()
	result, err := svc.GetSourceImportResult(workspace, id)
	if err != nil || svc.tasks.GetTask(id).Status != TaskFailed || result.Imported != 1 || result.MetadataPath != target+"/project.md" || len(result.Warnings) == 0 {
		t.Fatalf("generated ledger failure was reported as success: %+v (%v), task=%+v", result, err, svc.tasks.GetTask(id))
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(target+"/ai-plan.md"))); !os.IsNotExist(err) {
		t.Fatal("AI output was written before ledger preflight")
	}
	if calls.Load() != 1 {
		t.Fatalf("AI fixture did not reach the ledger conflict: %d calls", calls.Load())
	}
	workbenchWrite(t, workspace, target+"/sources.md", <-savedLedgers)
	retry := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	ledger := sourceProvenanceLedger(t, workspace, target)
	if retry.Imported != 0 || retry.Skipped != 1 || len(ledger.Generated) != 2 || len(ledger.PendingGenerated) != 0 {
		t.Fatalf("ledger failure retry was not idempotent: %+v; %+v", retry, ledger)
	}
	guidance := sourceRead(t, workspace, target+"/ai-plan.md")
	sourceProvenanceCheckGenerated(t, ledger, target+"/ai-plan.md", guidance, "ai-guidance")
	if strings.Contains(guidance, secret) || strings.Contains(sourceRead(t, workspace, target+"/sources.md"), secret) {
		t.Fatal("AI credential was persisted")
	}
	callsBefore := calls.Load()
	workbenchWrite(t, workspace, target+"/ai-plan.md", guidance+"\r\nUser edit.\r\n")
	runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if calls.Load() != callsBefore || sourceRead(t, workspace, target+"/ai-plan.md") != guidance+"\r\nUser edit.\r\n" {
		t.Fatal("confirmed AI note edit triggered regeneration")
	}
}

func TestSourceImportFinalPDFModeChangesPreservePriorOutputs(t *testing.T) {
	for _, firstReadable := range []bool{false, true} {
		t.Run(map[bool]string{false: "attachment-to-text", true: "text-to-attachment"}[firstReadable], func(t *testing.T) {
			workspace, target := t.TempDir(), "Learning/Changed PDF"
			original, updated := sourceTestPDF(t, ""), sourceTestPDF(t, "Now readable")
			wantExtension, wantMode := ".md", "pdf-text"
			if firstReadable {
				original, updated = updated, original
				wantExtension, wantMode = ".pdf", "attachment"
			}
			request := SourceImportRequest{SourceType: "files", Kind: "book", Name: "Changed PDF", ConflictStrategy: "update", Files: []SourceImportFile{sourceTestFile("paper.pdf", string(original))}}
			first := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			saved := sourceRead(t, workspace, first.Files[0])
			request.Files = []SourceImportFile{sourceTestFile("paper.pdf", string(updated))}
			second := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if second.Imported != 1 || second.Updated != 0 || len(second.Files) != 1 || second.Files[0] != target+"/paper"+wantExtension || sourceRead(t, workspace, first.Files[0]) != saved {
				t.Fatalf("a PDF mode change must keep each representation usable: first=%+v second=%+v", first, second)
			}
			ledger := sourceProvenanceLedger(t, workspace, target)
			if len(ledger.Entries) != 1 || ledger.Entries[0].ExtractionMode != wantMode || ledger.Entries[0].Path != second.Files[0] {
				t.Fatalf("mode transition provenance did not match the output: %+v", ledger)
			}
			retry := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if retry.Imported != 0 || retry.Skipped != 1 || retry.Files[0] != second.Files[0] {
				t.Fatalf("mode transition retry was not idempotent: %+v", retry)
			}
		})
	}
}

func TestSourceImportFinalGeneratedRecordsCannotAuthorizeSourceWrites(t *testing.T) {
	workspace, target := t.TempDir(), "Projects/Protected Generated"
	request := SourceImportRequest{SourceType: "empty", Kind: "project", Name: "Protected Generated"}
	first := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	metadata := sourceRead(t, workspace, first.MetadataPath)
	guidance := "# Previously generated guidance\n"
	workbenchWrite(t, workspace, target+"/ai-plan.md", guidance)
	ledger := sourceProvenanceLedger(t, workspace, target)
	ledger.Generated = append(ledger.Generated, sourceProvenanceTestGenerated{Path: target + "/ai-plan.md", SHA256: sourceChecksum([]byte(guidance)), Role: "ai-guidance"})
	sourceProvenanceWriteLedger(t, workspace, target, ledger)
	request.SourceType, request.ConflictStrategy = "files", "update"
	request.Files = []SourceImportFile{sourceTestFile("project.md", "# Incoming metadata name"), sourceTestFile("ai-plan.md", "# Incoming guidance name")}
	imported := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if imported.Imported != 2 || sourceRead(t, workspace, target+"/project.md") != metadata || sourceRead(t, workspace, target+"/ai-plan.md") != guidance || sourceRead(t, workspace, target+"/source-project.md") != "# Incoming metadata name" {
		t.Fatalf("generated records authorized an ordinary protected write: %+v", imported)
	}
	ledger = sourceProvenanceLedger(t, workspace, target)
	ledger.Entries[0].Path = target + "/project.md"
	ledger.Entries[0].SHA256 = sourceChecksum([]byte(metadata))
	sourceProvenanceWriteLedger(t, workspace, target, ledger)
	if _, err := NewImportService().PreviewSourceImport(workspace, request); err == nil {
		t.Fatal("generated collection relaxed protected ordinary-entry validation")
	}
	if sourceRead(t, workspace, target+"/project.md") != metadata || sourceRead(t, workspace, target+"/ai-plan.md") != guidance {
		t.Fatal("protected notes changed")
	}
}

func TestSourceImportFinalGeneratedLedgerValidation(t *testing.T) {
	valid := sourceProvenanceTestGenerated{Path: "Projects/Validated/project.md", SHA256: strings.Repeat("0", 64), Role: "collection-metadata"}
	for name, records := range map[string][]sourceProvenanceTestGenerated{
		"backslash-alias":  {{Path: "Projects\\Validated\\project.md", SHA256: valid.SHA256, Role: valid.Role}},
		"outside":          {{Path: "../outside.md", SHA256: valid.SHA256, Role: valid.Role}},
		"other-collection": {{Path: "Projects/Other/project.md", SHA256: valid.SHA256, Role: valid.Role}},
		"source-file":      {{Path: "Projects/Validated/source.md", SHA256: valid.SHA256, Role: valid.Role}},
		"ledger":           {{Path: "Projects/Validated/sources.md", SHA256: valid.SHA256, Role: valid.Role}},
		"wrong-role":       {{Path: valid.Path, SHA256: valid.SHA256, Role: "ai-guidance"}},
		"unknown-role":     {{Path: valid.Path, SHA256: valid.SHA256, Role: "model-supplied"}},
		"bad-hash":         {{Path: valid.Path, SHA256: strings.Repeat("z", 64), Role: valid.Role}},
		"duplicate":        {valid, valid},
		"too-many":         {valid, valid, valid},
	} {
		for _, pending := range []bool{false, true} {
			suffix := "/confirmed"
			if pending {
				suffix = "/pending"
			}
			t.Run(name+suffix, func(t *testing.T) {
				workspace := t.TempDir()
				ledger := sourceProvenanceTestLedger{Version: 1, Entries: []sourceProvenanceTestEntry{}, Generated: records}
				if pending {
					ledger.Generated, ledger.PendingGenerated = nil, records
				}
				sourceProvenanceWriteLedger(t, workspace, "Projects/Validated", ledger)
				root, err := os.OpenRoot(workspace)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				if _, _, _, err := sourceLoadManifest(root, "Projects/Validated"); err == nil {
					t.Fatal("unsafe generated record was accepted")
				}
			})
		}
	}
}

func TestSourceImportFinalGeneratedFileRecoversAfterConfirmationConflict(t *testing.T) {
	for _, role := range []string{"collection-metadata", "ai-guidance"} {
		t.Run(role, func(t *testing.T) {
			workspace, target := t.TempDir(), "Projects/Interrupted"
			request := SourceImportRequest{SourceType: "empty", Kind: "project", Name: "Interrupted"}
			relative, content := target+"/project.md", sourceCollectionMetadata(request, nil)
			if role == "ai-guidance" {
				runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
				relative, content = target+"/ai-plan.md", []byte("# Saved AI suggestion\n")
			}
			root, err := os.OpenRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			write, err := sourcePrepareGenerated(root, target, relative, role, content)
			if err != nil {
				t.Fatal(err)
			}
			// Reproduce interruption after the actual file write, with another
			// editor changing ledger prose before the prepared confirmation.
			if err := write.reserve.save(root); err != nil {
				t.Fatal(err)
			}
			if err := sourceWriteRoot(root, relative, content, nil, false); err != nil {
				t.Fatal(err)
			}
			reservation := sourceRead(t, workspace, target+"/sources.md")
			workbenchWrite(t, workspace, target+"/sources.md", reservation+"\nConcurrent authored prose.\n")
			if err := write.confirm.save(root); err == nil {
				t.Fatal("stale confirmation replaced an editor change")
			}
			interrupted := sourceProvenanceLedger(t, workspace, target)
			if len(interrupted.PendingGenerated) != 1 || interrupted.PendingGenerated[0].Path != relative {
				t.Fatal("failed confirmation lost its durable recovery evidence")
			}
			result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			recovered := sourceProvenanceLedger(t, workspace, target)
			if result.Imported != 0 || len(recovered.PendingGenerated) != 0 || sourceRead(t, workspace, relative) != string(content) || !strings.HasSuffix(sourceRead(t, workspace, target+"/sources.md"), "\nConcurrent authored prose.\n") {
				t.Fatalf("restart did not safely recover a saved generated file: %+v; %+v", result, recovered)
			}
			sourceProvenanceCheckGenerated(t, recovered, relative, string(content), role)
		})
	}
}

func TestSourceImportFinalGeneratedLedgerCapacityPrecedesMetadataWrite(t *testing.T) {
	workspace, target := t.TempDir(), "Resources/Full Ledger"
	original := strings.Repeat("x", (4<<20)-100) + "\n"
	workbenchWrite(t, workspace, target+"/sources.md", original)
	svc := NewImportServiceWithTasks(NewTaskService(nil))
	id, err := svc.StartSourceImport(workspace, SourceImportRequest{SourceType: "empty", Kind: "topic", Name: "Full Ledger"})
	if err != nil {
		t.Fatal(err)
	}
	svc.tasks.Wait()
	result, err := svc.GetSourceImportResult(workspace, id)
	if err != nil || svc.tasks.GetTask(id).Status != TaskFailed || result.MetadataPath != "" || result.Imported != 0 {
		t.Fatalf("oversized generated ledger did not fail before metadata write: %+v (%v)", result, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(target+"/index.md"))); !os.IsNotExist(err) {
		t.Fatal("ledger capacity failure left untracked generated metadata")
	}
	if sourceRead(t, workspace, target+"/sources.md") != original {
		t.Fatal("failed generated preflight changed authored ledger prose")
	}
}

func TestSourceImportFinalAttachmentExtractionPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, name := range []string{"scan.pdf", "image.png"} {
		outputs, warnings, err := sourceExtractInput(ctx, sourceImportInput{name: name, data: sourceTestPDF(t, "")}, &sourceImportBudget{}, 0)
		if err != context.Canceled || len(outputs) != 0 || len(warnings) != 0 {
			t.Fatalf("cancelled extraction returned an attachment: %s %+v %v %v", name, outputs, warnings, err)
		}
	}
}
