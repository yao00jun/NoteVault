package service

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A small PDF 1.7 with the catalog, page tree and font in an object stream.
// No third-party documents are needed for the regression fixture.
func sourceTestCompressedPDF(t *testing.T, corrupt string) []byte {
	t.Helper()
	compress := func(data []byte) []byte {
		var out bytes.Buffer
		w := zlib.NewWriter(&out)
		_, _ = w.Write(data)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var index, body bytes.Buffer
	for i, obj := range objects {
		fmt.Fprintf(&index, "%d %d ", i+1, body.Len())
		body.WriteString(obj + "\n")
	}
	first := index.Len()
	objectData := compress(append(index.Bytes(), body.Bytes()...))
	text := compress([]byte("BT /F1 12 Tf 72 720 Td (Readable compressed PDF chapter.) Tj ET"))
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	contentOffset := out.Len()
	fmt.Fprintf(&out, "5 0 obj\n<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream\nendobj\n", len(text), text)
	objectOffset := out.Len()
	extra := ""
	count := "4"
	if corrupt == "extends" {
		extra = "/Extends 6 0 R"
	}
	if corrupt == "indirect-count" {
		count = "6 0 R"
	}
	fmt.Fprintf(&out, "6 0 obj\n<< /Type /ObjStm /N %s /First %d /Length %d /Filter /FlateDecode %s >>\nstream\n%s\nendstream\nendobj\n", count, first, len(objectData), extra, objectData)
	xrefOffset := out.Len()
	entries := make([]byte, 8*7)
	set := func(id, kind, field, generation int) {
		p := entries[id*7:]
		p[0] = byte(kind)
		binary.BigEndian.PutUint32(p[1:5], uint32(field))
		binary.BigEndian.PutUint16(p[5:7], uint16(generation))
	}
	set(0, 0, 0, 65535)
	for i := 1; i <= 4; i++ {
		set(i, 2, 6, i-1)
	}
	set(5, 1, contentOffset, 0)
	set(6, 1, objectOffset, 0)
	set(7, 1, xrefOffset, 0)
	switch corrupt {
	case "cycle":
		set(6, 2, 6, 0)
	case "wrong-member":
		set(1, 2, 6, 3)
	case "invalid-offset":
		set(5, 1, contentOffset+2, 0)
	case "missing-stream":
		set(1, 2, 5, 0)
	}
	xref := compress(entries)
	size := 8
	if corrupt == "huge-size" {
		size = 1000000000
	}
	fmt.Fprintf(&out, "7 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Index [0 8] /Root 1 0 R /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", size, len(xref), xref, xrefOffset)
	return out.Bytes()
}

func TestSourceImportCompressedPDFExtractsRealText(t *testing.T) {
	data := sourceTestCompressedPDF(t, "")
	text, err := sourceExtractPDF(context.Background(), data)
	if err != nil || !strings.Contains(text, "Readable compressed PDF chapter.") {
		t.Fatalf("valid compressed indices and objects should extract text: %q (%v)", text, err)
	}
}

func TestSourceImportCompressedPDFRejectsSecondStreamInObject(t *testing.T) {
	data := sourceTestCompressedPDF(t, "cycle")
	at := bytes.LastIndex(data, []byte("\nendobj\nstartxref\n"))
	if at < 0 {
		t.Fatal("missing final object boundary")
	}
	extra := []byte("\n<< /Type /ObjStm /Size 8 /W [1 4 2] /Length 56 >>\nstream\n")
	extra = append(extra, make([]byte, 56)...)
	extra = append(extra, []byte("\nendstream")...)
	bad := append([]byte{}, data[:at]...)
	bad = append(bad, extra...)
	bad = append(bad, data[at:]...)
	if err := sourcePDFPreflight(context.Background(), bad); err == nil {
		t.Fatal("second stream replaced the verified cyclic index")
	}
}

func TestSourceImportCompressedPDFRejectsInvalidReferences(t *testing.T) {
	for _, variant := range []string{"cycle", "wrong-member", "invalid-offset", "missing-stream", "huge-size", "extends", "indirect-count"} {
		t.Run(variant, func(t *testing.T) {
			if err := sourcePDFPreflight(context.Background(), sourceTestCompressedPDF(t, variant)); err == nil {
				t.Fatal("unsafe compressed reference passed validation")
			}
		})
	}
}

// Optional local acceptance: fixtures stay outside the repository.
func TestSourceImportPDFLocalSamples(t *testing.T) {
	directory := os.Getenv("NOTEVAULT_QA_PDF_DIR")
	if directory == "" {
		t.Skip("set NOTEVAULT_QA_PDF_DIR for local PDF acceptance")
	}
	files, err := filepath.Glob(filepath.Join(directory, "*.pdf"))
	if err != nil || len(files) == 0 {
		t.Fatal("no PDF samples found", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			text, err := sourceExtractPDF(context.Background(), data)
			if err != nil || len([]rune(text)) < 100 {
				t.Fatalf("PDF did not produce usable text: %v", err)
			}
			t.Logf("extracted %d characters", len([]rune(text)))
		})
	}
}
