package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceReviewHiddenPDF(reference string) []byte {
	var data bytes.Buffer
	data.WriteString("%PDF-1.5\n9 0 obj\n(")
	hidden := data.Len()
	data.WriteString("1 0 obj << /Type /XRef /Size 1000000000 /W [1 4 2] /Length 0 >>\nstream\n\nendstream\nendobj")
	data.WriteString(")\nendobj\n")
	xref := data.Len()
	fmt.Fprintf(&data, "xref\n0 2\n0000000000 65535 f \n%010d 00000 n \ntrailer\n<< /Size 2 /Root 1 0 R >>\n", hidden)
	if reference == "startxref" {
		xref = hidden
	}
	fmt.Fprintf(&data, "startxref\n%d\n%%%%EOF\n", xref)
	return data.Bytes()
}

func sourceReviewCyclicPDF() []byte {
	var data bytes.Buffer
	data.WriteString("%PDF-1.5\n")
	xref := data.Len()
	data.WriteString("1 0 obj\n<< /Type /XRef /Size 2 /W [1 4 2] /Root 1 0 R /Length 14 >>\nstream\n")
	data.Write([]byte{0, 0, 0, 0, 0, 255, 255, 2, 0, 0, 0, 1, 0, 0})
	data.WriteString("\nendstream\nendobj\n")
	fmt.Fprintf(&data, "startxref\n%d\n%%%%EOF\n", xref)
	return data.Bytes()
}

func sourceReviewTrailerPDF(t *testing.T, key string) []byte {
	t.Helper()
	data := sourceTestPDF(t, "Normal text before unsupported incremental index.")
	xref := bytes.Index(data, []byte("\nxref\n")) + 1
	return bytes.Replace(data, []byte("trailer\n<< /Size"), []byte(fmt.Sprintf("trailer\n<< /%s %d /Size", key, xref)), 1)
}

func sourceReviewHiddenFooterPDF(t *testing.T) []byte {
	t.Helper()
	data := sourceTestPDF(t, "Text before a hidden final startxref marker.")
	return append(data, []byte("9 0 obj\n(\nstartxref\n0\n)\nendobj\n%%EOF\n")...)
}

func TestSourceImportReviewPDFRejectsActualUnsafeReferences(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"startxref inside a literal", sourceReviewHiddenPDF("startxref")},
		{"object offset inside a literal", sourceReviewHiddenPDF("object")},
		{"self-referential object stream", sourceReviewCyclicPDF()},
		{"cyclic previous table", sourceReviewTrailerPDF(t, "Prev")},
		{"hybrid table", sourceReviewTrailerPDF(t, "XRefStm")},
		{"footer marker inside a literal", sourceReviewHiddenFooterPDF(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Never execute a known allocation/recursion payload in the PDF
			// dependency until our preflight has proved it will be rejected.
			if err := sourcePDFPreflight(context.Background(), tc.data); err == nil {
				t.Fatal("unsafe actual PDF references passed preflight")
			}
			workspace := t.TempDir()
			request := SourceImportRequest{SourceType: "files", Kind: "book", Name: "Preserved", Files: []SourceImportFile{{Name: "reference.pdf", ContentBase64: base64.StdEncoding.EncodeToString(tc.data)}}}
			result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if result.Imported != 1 || result.Skipped != 0 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "原始 PDF") {
				t.Fatalf("unsupported references must preserve the attachment with an explicit warning: %+v", result)
			}
			if got := sourceRead(t, workspace, "Learning/Preserved/reference.pdf"); got != string(tc.data) {
				t.Fatal("fallback changed original PDF attachment bytes")
			}
		})
	}
}

func TestSourceImportReviewZIPBoundsActualDirectoryBeforeAllocation(t *testing.T) {
	one := sourceTestZip(t, [][2]string{{"a.md", "a"}})
	eocd := bytes.LastIndex(one, []byte{'P', 'K', 5, 6})
	directory := int(binary.LittleEndian.Uint32(one[eocd+16 : eocd+20]))
	record := one[directory:eocd]
	const count = 65537 // The ZIP reader accepts this count modulo uint16.
	data := append([]byte{}, one[:directory]...)
	data = append(data, bytes.Repeat(record, count)...)
	end := append([]byte{}, one[eocd:]...)
	binary.LittleEndian.PutUint32(end[12:16], uint32(len(record)*count))
	data = append(data, end...)
	allocations := testing.AllocsPerRun(1, func() {
		if _, err := sourceReadArchive(context.Background(), data, &sourceImportBudget{}); err == nil {
			t.Fatal("forged central-directory entry count was accepted")
		}
	})
	if allocations > 1000 {
		t.Fatalf("ZIP allocated %.0f objects before applying its 500-entry budget", allocations)
	}
	t.Logf("forged 65537-entry directory rejected with %.0f allocations", allocations)
}

func TestSourceImportReviewPDFStreamBoundsUseOuterDictionary(t *testing.T) {
	data := sourceTestPDF(t, "Visible bounded PDF text.", func(objects []string) {
		objects[4] = strings.Replace(objects[4], "/Filter /FlateDecode", "/Filter /FlateDecode /Custom << /Length 1 /Filter /Unknown >>", 1)
	})
	text, err := sourceExtractPDF(context.Background(), data)
	if err != nil || !strings.Contains(text, "Visible bounded PDF text.") {
		t.Fatalf("nested dictionary values changed the actual stream boundary: %q, %v", text, err)
	}
}

func TestSourceImportReviewCopyRetryFindsCompletedFileAfterNumberingGap(t *testing.T) {
	workspace := t.TempDir()
	workbenchWrite(t, workspace, "Resources/Manual/guide.md", "# Local\n")
	workbenchWrite(t, workspace, "Resources/Manual/guide (3).md", "# Upstream\n")
	request := SourceImportRequest{SourceType: "files", Kind: "topic", Name: "Manual", ConflictStrategy: "copy", Files: []SourceImportFile{sourceTestFile("guide.md", "# Upstream\n")}}
	result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
	if result.Imported != 0 || result.Skipped != 1 || len(result.Files) != 1 || result.Files[0] != "Resources/Manual/guide (3).md" {
		t.Fatalf("a deleted earlier copy must not hide a completed later copy: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(workspace, "Resources", "Manual", "guide (2).md")); !os.IsNotExist(err) {
		t.Fatal("retry created a duplicate in the numbering gap")
	}
}

func TestSourceImportReviewCopyRetryRecoversCompletedFileWithoutLedger(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprint(edited), func(t *testing.T) {
			workspace := t.TempDir()
			workbenchWrite(t, workspace, "Resources/Manual/guide.md", "# Local\n")
			content := "# Upstream\n"
			if edited {
				content = "# My edited copy\n"
			}
			workbenchWrite(t, workspace, "Resources/Manual/guide (2).md", content)
			request := SourceImportRequest{SourceType: "files", Kind: "topic", Name: "Manual", ConflictStrategy: "copy", Files: []SourceImportFile{sourceTestFile("guide.md", "# Upstream\n")}}
			result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if sourceRead(t, workspace, "Resources/Manual/guide (2).md") != content {
				t.Fatal("retry changed an existing completed copy")
			}
			if edited {
				if result.Imported != 1 || sourceRead(t, workspace, "Resources/Manual/guide (3).md") != "# Upstream\n" {
					t.Fatalf("retry must preserve an edited copy: %+v", result)
				}
			} else {
				if result.Imported != 0 || result.Skipped != 1 || len(result.Files) != 1 || result.Files[0] != "Resources/Manual/guide (2).md" {
					t.Fatalf("retry duplicated a completed copy after lost provenance: %+v", result)
				}
				if _, err := os.Stat(filepath.Join(workspace, "Resources", "Manual", "guide (3).md")); !os.IsNotExist(err) {
					t.Fatal("retry created an unnecessary third copy")
				}
				if !strings.Contains(sourceRead(t, workspace, "Resources/Manual/sources.md"), "Resources/Manual/guide (2).md") {
					t.Fatal("retry did not repair the completed copy's provenance")
				}
			}
		})
	}
}

func TestSourceImportReviewManifestCapacityPrecedesSourceWrite(t *testing.T) {
	for _, capacity := range []string{"entries", "serialized bytes"} {
		t.Run(capacity, func(t *testing.T) {
			workspace := t.TempDir()
			workbenchWrite(t, workspace, "Resources/Manual/guide.md", "# Local\n")
			manifest := sourceManifest{Version: 1, Entries: []sourceManifestEntry{}}
			if capacity == "entries" {
				for i := 0; i < 10000; i++ {
					name := fmt.Sprintf("old-%04d.md", i)
					manifest.Entries = append(manifest.Entries, sourceManifestEntry{Origin: "upload:" + name, Source: name, Path: "Resources/Manual/" + name, SHA256: strings.Repeat("0", 64), SourceSHA256: strings.Repeat("0", 64)})
				}
			}
			encoded, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			ledger := sourceManifestStart + string(encoded) + "\n-->\n"
			if capacity == "serialized bytes" {
				ledger = strings.Repeat("x", (4<<20)-len(ledger)-100) + "\n" + ledger
			}
			workbenchWrite(t, workspace, "Resources/Manual/sources.md", ledger)
			root, err := os.OpenRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, _, _, err := sourceLoadManifest(root, "Resources/Manual"); err != nil {
				t.Fatalf("fixture must be an accepted existing ledger: %v", err)
			}
			result := &SourceImportResult{}
			err = sourceImportOne(root, SourceImportRequest{TargetFolder: "Resources/Manual", ConflictStrategy: "copy"}, sourceImportOutput{name: "guide.md", origin: "upload:guide.md", source: "guide.md", data: []byte("# Upstream\n")}, result)
			if err == nil || result.Imported != 0 || result.Updated != 0 {
				t.Fatalf("manifest capacity must fail before any source write: result=%+v, err=%v", result, err)
			}
			if _, err := root.Lstat("Resources/Manual/guide (2).md"); !os.IsNotExist(err) {
				t.Fatal("capacity failure left an untracked output file")
			}
			if sourceRead(t, workspace, "Resources/Manual/sources.md") != ledger {
				t.Fatal("capacity failure changed existing ledger bytes")
			}
		})
	}
}

func TestSourceImportReviewHTMLIgnoredContainersWithOmittedEndTags(t *testing.T) {
	for _, markup := range []string{
		"<nav><ul><li>Hidden A<li>Hidden B</ul></nav><main>Visible Body</main>",
		"<aside><p>Hidden A<p>Hidden B</aside><main>Visible Body</main>",
		"<header><nav><ul><li>Hidden A<li>Hidden B</ul></nav></header><main>Visible Body</main>",
		"<head><title>Hidden</title><body><main>Visible Body</main>",
	} {
		got, err := sourceHTMLMarkdown(context.Background(), []byte(markup), func(value string) string { return value })
		if err != nil || !strings.Contains(got, "Visible Body") || strings.Contains(got, "Hidden") {
			t.Errorf("omitted descendant end tags must not swallow article text: %q, %v", got, err)
		}
	}
}

func TestSourceImportReviewHTMLLinksUseFinalRedirectURL(t *testing.T) {
	workspace := t.TempDir()
	svc := NewImportServiceWithTasks(NewTaskService(nil))
	svc.sourceImports.httpClient = &http.Client{Transport: sourceRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/start" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"/docs/index.html"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.URL.Path != "/docs/index.html" {
			t.Errorf("unexpected redirect target %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<main><img src="image.png"><a href="guide.html">Guide</a></main>`)), Request: r}, nil
	})}
	result := runSourceImport(t, svc, workspace, SourceImportRequest{SourceType: "url", Source: "https://93.184.216.34/start", Kind: "topic", Name: "Redirect"})
	if len(result.Files) != 1 {
		t.Fatalf("expected one redirected page: %+v", result)
	}
	content := sourceRead(t, workspace, result.Files[0])
	for _, suffix := range []string{"image.png", "guide.html"} {
		if !strings.Contains(content, "https://93.184.216.34/docs/"+suffix) {
			t.Errorf("relative link lost the final response directory: %s", content)
		}
	}
	ledger := sourceRead(t, workspace, "Resources/Redirect/sources.md")
	if !strings.Contains(ledger, `"origin": "https://93.184.216.34/start"`) {
		t.Fatal("redirect must retain the original user-supplied URL as provenance")
	}
}
