package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

// Hand-positioned text objects reproduce font switches inside one visible line.
// The fixture is generated locally and contains no third-party book content.
func sourceTestLayoutPDF(t *testing.T, pages ...string) []byte {
	t.Helper()
	widths := strings.Repeat("600 ", 256)
	font := func(name string) string {
		return fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding /FirstChar 0 /LastChar 255 /Widths [%s] >>", name, widths)
	}
	fonts := "/F1 " + font("Helvetica") + " /FB " + font("Helvetica-Bold") + " /FM " + font("Courier") +
		" /FC << /Type /Font /Subtype /Type0 /BaseFont /NotoSansCJK /Encoding /UniGB-UCS2-H /DescendantFonts [<< /Type /Font /Subtype /CIDFontType0 /BaseFont /NotoSansCJK /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 5 >> /DW 1000 >>] >>"
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	var kids []string
	for _, content := range pages {
		page := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Resources << /Font << %s >> >> /MediaBox [0 0 612 792] /Contents %d 0 R >>", fonts, page+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages))
	return sourceTestPDFObjects(t, objects)
}

func sourceTestPDFObjects(t *testing.T, objects []string) []byte {
	t.Helper()
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for index, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}

func sourceTestPDFCJK(text string) string {
	var out strings.Builder
	out.WriteByte('<')
	for _, value := range utf16.Encode([]rune(text)) {
		fmt.Fprintf(&out, "%04X", value)
	}
	out.WriteByte('>')
	return out.String()
}

func TestSourcePDFLayoutRejoinsFontRunsAndChineseWraps(t *testing.T) {
	page := fmt.Sprintf(`
BT /FB 22 Tf 72 740 Td (Collections) Tj ET
BT /FB 17 Tf 72 700 Td (1. ArrayList) Tj ET
BT /FC 12 Tf 72 660 Td %s Tj ET
BT /F1 12 Tf 192 660.5 Td (10) Tj ET
BT /FC 12 Tf 207 660 Td %s Tj ET
BT /FC 12 Tf 72 642 Td %s Tj ET
BT /F1 12 Tf 72 594 Td (A wrapped English) Tj ET
BT /F1 12 Tf 72 576 Td (paragraph keeps words.) Tj ET
`, sourceTestPDFCJK("数组默认初始容量为"), sourceTestPDFCJK("，并且支持动"), sourceTestPDFCJK("态扩容。"))
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Collections", "### 1. ArrayList", "数组默认初始容量为10，并且支持动态扩容。", "A wrapped English paragraph keeps words."} {
		if !strings.Contains(text, want) {
			t.Errorf("missing reconstructed block %q in:\n%s", want, text)
		}
	}
}

func TestSourcePDFLayoutPreservesListsCodeAndLiteralMarkup(t *testing.T) {
	page := `
BT /FB 20 Tf 72 740 Td (Examples) Tj ET
BT /F1 12 Tf 72 700 Td (- Keep item one.) Tj ET
BT /F1 12 Tf 72 680 Td (- Keep item two with) Tj ET
BT /F1 12 Tf 86.4 662 Td (continued details.) Tj ET
BT /FM 10 Tf 80 620 Td (function demo\(\) {) Tj ET
BT /FM 10 Tf 104 605 Td (return 42;) Tj ET
BT /FM 10 Tf 80 590 Td (}) Tj ET
BT /F1 12 Tf 72 545 Td (Use ) Tj ET
BT /FM 12 Tf 100.8 545 Td (<script>) Tj ET
BT /F1 12 Tf 158.4 545 Td ( as literal text.) Tj ET
BT /F1 12 Tf 72 509 Td (<div>must remain visible</div>) Tj ET
`
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"- Keep item one.\n- Keep item two with continued details.",
		"```\nfunction demo() {\n    return 42;\n}\n```",
		"Use `<script>` as literal text.",
		"&lt;div&gt;must remain visible&lt;/div&gt;",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing preserved structure %q in:\n%s", want, text)
		}
	}
}

func TestSourcePDFLayoutKeepsParagraphsAcrossPagesAndRemovesRunningMargins(t *testing.T) {
	page := func(number int, body string) string {
		return fmt.Sprintf("BT /F1 9 Tf 72 772 Td (Running book title) Tj ET\n%s\nBT /F1 9 Tf 300 20 Td (%d) Tj ET", body, number)
	}
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t,
		page(1, "BT /FB 18 Tf 72 720 Td (First section) Tj ET\nBT /F1 12 Tf 72 55 Td (This sentence continues) Tj ET"),
		page(2, "BT /F1 12 Tf 72 720 Td (on the next page.) Tj ET\nBT /F1 12 Tf 72 682 Td (A separate paragraph.) Tj ET"),
		page(3, "BT /FB 18 Tf 72 720 Td (Last section) Tj ET\nBT /F1 12 Tf 72 682 Td (The end.) Tj ET")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "This sentence continues on the next page.\n\nA separate paragraph.") {
		t.Fatalf("lost paragraph boundaries across pages:\n%s", text)
	}
	if strings.Contains(text, "Running book title") || strings.Contains(text, "\n2\n") || strings.HasSuffix(text, "\n3") {
		t.Fatalf("running margins leaked into the chapter:\n%s", text)
	}
}

func TestSourceImportPDFLayoutUpgradesOnlyUneditedChapters(t *testing.T) {
	for _, scenario := range []struct {
		name, strategy     string
		edited             bool
		updated, conflicts int
	}{
		{name: "default stored extraction", updated: 1},
		{name: "explicit update", strategy: "update", updated: 1},
		{name: "explicit skip", strategy: "skip", conflicts: 1},
		{name: "personal edits", strategy: "update", edited: true, conflicts: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			workspace := t.TempDir()
			folder, original := "Learning/Book", "# My book\n"
			data := sourceTestLayoutPDF(t, "BT /FB 20 Tf 72 740 Td (Chapter) Tj ET\nBT /F1 12 Tf 72 700 Td (Useful ) Tj ET\nBT /F1 12 Tf 122.4 700 Td (content.) Tj ET")
			old := "# paper\n\nChapter\nUseful\ncontent.\n"
			workbenchWrite(t, workspace, folder+"/book.md", original)
			workbenchWrite(t, workspace, folder+"/paper.pdf", string(data))
			workbenchWrite(t, workspace, folder+"/paper.md", old)
			root, err := os.OpenRoot(workspace)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			if err := sourceSaveManifest(root, folder, sourceManifestEntry{Origin: "workspace:" + folder + "/paper.pdf", Source: "paper.pdf", Path: folder + "/paper.md", SHA256: sourceChecksum([]byte(old)), SourceSHA256: sourceChecksum(data), ExtractionMode: "pdf-text"}); err != nil {
				t.Fatal(err)
			}
			if scenario.edited {
				old += "\nMy personal annotations.\n"
				workbenchWrite(t, workspace, folder+"/paper.md", old)
			}
			request := SourceImportRequest{SourceType: "attachments", Kind: "book", Source: folder, ConflictStrategy: scenario.strategy}
			result := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
			if result.Updated != scenario.updated || len(result.Conflicts) != scenario.conflicts || result.Imported != 0 {
				t.Fatalf("unexpected upgrade result: %+v", result)
			}
			after := sourceRead(t, workspace, folder+"/paper.md")
			if scenario.updated > 0 {
				if !strings.Contains(after, "## Chapter\n\nUseful content.") {
					t.Fatalf("old text did not upgrade:\n%s", after)
				}
				again := runSourceImport(t, NewImportServiceWithTasks(NewTaskService(nil)), workspace, request)
				if again.Updated != 0 || again.Imported != 0 || len(again.Conflicts) != 0 || sourceRead(t, workspace, folder+"/paper.md") != after {
					t.Fatalf("retry after restart must be idempotent: %+v", again)
				}
			} else if after != old {
				t.Fatal("changed a protected chapter")
			}
			if sourceRead(t, workspace, folder+"/book.md") != original || sourceRead(t, workspace, folder+"/paper.pdf") != string(data) {
				t.Fatal("changed source or metadata")
			}
		})
	}
}

func TestSourcePDFLayoutKeepsWrappedInlineFormattingInsideParagraphs(t *testing.T) {
	page := `
BT /F1 12 Tf 72 720 Td (Use ) Tj ET
BT /FM 11 Tf 100.8 720 Td (<association) Tj ET
BT /FM 11 Tf 72 702 Td (property="detail") Tj ET
BT /FM 11 Tf 72 684 Td (javaType="Detail"/>) Tj ET
BT /F1 12 Tf 190.8 684 Td ( for the mapping.) Tj ET
BT /F1 12 Tf 72 648 Td (This is ) Tj ET
BT /FB 12 Tf 129.6 648 Td (very) Tj ET
BT /FB 12 Tf 72 630 Td (important) Tj ET
BT /F1 12 Tf 136.8 630 Td ( for readers.) Tj ET
BT /F1 12 Tf 72 590 Td (Normal paragraph text is kept with its inline examples.) Tj ET
`
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "```") || !strings.Contains(text, "Use `<association property=\"detail\" javaType=\"Detail\"/>` for the mapping.") || !strings.Contains(text, "This is **very important** for readers.") {
		t.Fatalf("inline examples and emphasis were fragmented into unrelated blocks:\n%s", text)
	}
}

func TestSourcePDFLayoutDropsOnlySequentialCodeLineNumberGutters(t *testing.T) {
	page := `
BT /FB 20 Tf 72 740 Td (A code example) Tj ET
BT /F1 12 Tf 72 700 Td (The example has a separate line-number gutter.) Tj ET
BT /FM 10 Tf 60 655 Td (1) Tj ET
BT /FM 10 Tf 86 655 Td (function demo\(\) {) Tj ET
BT /FM 10 Tf 60 640 Td (2) Tj ET
BT /FM 10 Tf 86 640 Td (    return 42;) Tj ET
BT /FM 10 Tf 60 625 Td (3) Tj ET
BT /FM 10 Tf 86 625 Td (    // ) Tj ET
BT /FC 10 Tf 128 625 Td ` + sourceTestPDFCJK("注释正文") + ` Tj ET
BT /FM 10 Tf 60 610 Td (4) Tj ET
BT /FM 10 Tf 86 610 Td (}) Tj ET
BT /F1 12 Tf 72 580 Td (42 is the returned value.) Tj ET
`
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "```\nfunction demo() {\n    return 42;\n    // 注释正文\n}\n```") || !strings.Contains(text, "42 is the returned value.") {
		t.Fatalf("code line numbers must not become source code or hide real numbers:\n%s", text)
	}
}

func TestSourcePDFLayoutJoinsWrappedHeadingsWithoutMergingSeparateQuestions(t *testing.T) {
	page := `
BT /FB 17 Tf 72 720 Td (7. How do we handle a busy thread pool that is) Tj ET
BT /FB 17 Tf 72 698 Td (rejecting work?) Tj ET
BT /F1 12 Tf 72 660 Td (Inspect its queue, workload and rejection policy.) Tj ET
BT /FB 17 Tf 72 615 Td (8. Another question?) Tj ET
BT /FB 17 Tf 72 590 Td (9. A separate question?) Tj ET
BT /F1 12 Tf 72 550 Td (Keep the questions separate.) Tj ET
`
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "## 7. How do we handle a busy thread pool that is rejecting work?\n\n") || !strings.Contains(text, "## 8. Another question?\n\n## 9. A separate question?") {
		t.Fatalf("heading wrapping broke the document outline:\n%s", text)
	}
}

func TestSourcePDFLayoutPreservesWrappedTokensAndEnglishWordBoundaries(t *testing.T) {
	for _, sample := range []struct {
		name, left, right, want string
		marginSpace             bool
	}{
		{"URL scheme", "前端跑在h", "ttp://localhost:5173，直接请求会跨域。", "前端跑在http://localhost:5173", true},
		{"URL port", "接口为http://localhost:300", "0，开启代理。", "http://localhost:3000", false},
		{"annotation", "使用@O", "rder注解指定优先级。", "使用@Order注解", false},
		{"identifier", "选择CompletableF", "uture处理异步调用。", "选择CompletableFuture处理异步调用。", false},
		{"English prose", "A wrapped English", "paragraph keeps words.", "A wrapped English paragraph keeps words.", false},
		{"English words in Chinese", "示例：The quick ", "brown fox。", "示例：The quick brown fox。", false},
		{"leading English space in Chinese", "示例：The quick", " brown fox。", "示例：The quick brown fox。", false},
		{"command option space", "示例：git ", "--version 显示版本。", "示例：git --version 显示版本。", false},
		{"space after punctuation", "Hello, ", "world.", "Hello, world.", false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			page := fmt.Sprintf("BT /FC 12 Tf 72 700 Td %s Tj ET\nBT /FC 12 Tf 72 682 Td %s Tj ET", sourceTestPDFCJK(sample.left), sourceTestPDFCJK(sample.right))
			if sample.marginSpace {
				page += "\nBT /F1 12 Tf 590 700 Td ( ) Tj ET"
			}
			text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, sample.want) {
				t.Fatalf("wrapped text changed: want %q in %q", sample.want, text)
			}
		})
	}
}

func TestSourcePDFLayoutPreservesRotatedText(t *testing.T) {
	page := `
BT /F1 12 Tf 72 700 Td (Normal readable text.) Tj ET
BT /F1 12 Tf 0 1 -1 0 550 120 Tm (Rotated <label> stays visible.) Tj ET
`
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Normal readable text.", "Rotated &lt;label&gt; stays visible."} {
		if !strings.Contains(text, want) {
			t.Errorf("lost visible text %q in %q", want, text)
		}
	}
}

func TestSourcePDFLayoutDoesNotInventSpacesFromMissingCIDWidths(t *testing.T) {
	page := fmt.Sprintf("BT /FC 12 Tf 72 700 Td %s Tj ET\nBT /FC 12 Tf 90 700 Td %s Tj ET", sourceTestPDFCJK("@O"), sourceTestPDFCJK("rder"))
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil || text != "@Order" {
		t.Fatalf("missing font advances must not split a token: %q, %v", text, err)
	}
}

func TestSourcePDFLayoutIgnoresMarginSpaceInWrappedChineseHeading(t *testing.T) {
	page := fmt.Sprintf("BT /FC 17 Tf 72 720 Td %s Tj ET\nBT /F1 17 Tf 161 720 Td ( ) Tj ET\nBT /FC 17 Tf 72 698 Td %s Tj ET\nBT /F1 12 Tf 72 660 Td (Body text.) Tj ET", sourceTestPDFCJK("标题该怎么"), sourceTestPDFCJK("办？"))
	text, err := sourceExtractPDF(context.Background(), sourceTestLayoutPDF(t, page))
	if err != nil || !strings.Contains(text, "## 标题该怎么办？") {
		t.Fatalf("page-margin whitespace became part of a heading: %q, %v", text, err)
	}
}

func TestSourcePDFLayoutBoundsExpansionBeforeAllocatingGlyphs(t *testing.T) {
	for _, sample := range []struct {
		name, content string
		limit         int
		rejected      bool
	}{
		{"small page", "BT /F1 12 Tf 72 700 Td (Readable text.) Tj ET", 64, false},
		{"long text run", "BT /F1 12 Tf 72 700 Td (" + strings.Repeat("A", 65) + ") Tj ET", 64, true},
		{"many text runs", strings.Repeat("BT /F1 12 Tf 72 700 Td (Small run.) Tj ET\n", 8), 64, true},
		{"positioned text array", "BT /F1 12 Tf [(" + strings.Repeat("A", 40) + ") 0 (" + strings.Repeat("B", 40) + ")] TJ ET", 64, true},
		{"rectangle expansion", strings.Repeat("0 0 1 1 re\n", 65), 64, true},
		{"graphics state depth", strings.Repeat("q\n", 129) + strings.Repeat("Q\n", 129), 1000, true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			data := sourceTestLayoutPDF(t, sample.content)
			reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			err = sourcePDFCheckLayout(context.Background(), reader.Page(1), sample.limit)
			if (err != nil) != sample.rejected {
				t.Fatalf("rejected = %v, want %v: %v", err != nil, sample.rejected, err)
			}
		})
	}
}

func TestSourcePDFLayoutBoundsUnicodeMappingExpansion(t *testing.T) {
	content := "BT /F1 12 Tf 72 700 Td (" + strings.Repeat("\x01", 24) + ") Tj ET"
	cmap := "begincmap\n1 begincodespacerange\n<00> <ff>\nendcodespacerange\n1 beginbfchar\n<01> <006600660069>\nendbfchar\nendcmap"
	data := sourceTestPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /MappedFont /Encoding /Identity-H /ToUnicode 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
	})
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourcePDFCheckLayout(context.Background(), reader.Page(1), 64); err == nil {
		t.Fatal("one source byte can map to several glyphs; expanded count must be bounded")
	}
	if err := sourcePDFCheckLayout(context.Background(), reader.Page(1), 128); err != nil {
		t.Fatal(err)
	}
	text, err := sourceExtractPDF(context.Background(), data)
	if err != nil || !strings.Contains(text, strings.Repeat("ffi", 24)) {
		t.Fatalf("bounded ligatures should remain readable: %q, %v", text, err)
	}
}

func TestSourcePDFLayoutBoundsImplicitArraySeparator(t *testing.T) {
	content := "BT /F1 12 Tf [] TJ ET"
	cmap := "begincmap\n1 begincodespacerange\n<00> <ff>\nendcodespacerange\n1 beginbfchar\n<0A> <" + strings.Repeat("0066", 80) + ">\nendbfchar\nendcmap"
	data := sourceTestPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /MappedFont /Encoding /Identity-H /ToUnicode 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
	})
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sourcePDFCheckLayout(context.Background(), reader.Page(1), 64); err == nil {
		t.Fatal("the reader decodes an implicit newline after TJ, even for an empty array")
	}
	if err := sourcePDFCheckLayout(context.Background(), reader.Page(1), 128); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSourcePDFLayoutCIDRun(b *testing.B) {
	for _, size := range []int{4096, 8192} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			glyphs := make([]pdf.Text, size)
			for i := range glyphs {
				glyphs[i] = pdf.Text{Font: "CJK", FontSize: 12, X: 72, Y: 700, S: "文"}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if line := sourcePDFMakeLine(glyphs, 0, 792); len(line.raw) != size*len("文") {
					b.Fatal("lost characters in a long CID run")
				}
			}
		})
	}
}
