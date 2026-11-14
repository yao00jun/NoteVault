package service

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestReadImageBinaryReturnsDataURL(t *testing.T) {
	root := t.TempDir()
	svc := NewFileService()
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}
	saved, err := svc.SaveImage(root, "shot.png", png)
	if err != nil {
		t.Fatal(err)
	}
	dataURL, err := svc.ReadImageBinary(root, saved)
	if err != nil {
		t.Fatal(err)
	}
	if dataURL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png) {
		t.Fatalf("unexpected data URL prefix: %.64s", dataURL)
	}
}

func TestReadImageBinaryRejectsTraversalUnknownExtAndMissing(t *testing.T) {
	root := t.TempDir()
	svc := NewFileService()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "note.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReadImageBinary(root, "../../outside.png"); err == nil {
		t.Fatal("路径穿越应被拒绝")
	}
	if _, err := svc.ReadImageBinary(root, "assets/note.txt"); err == nil {
		t.Fatal("非图片扩展名应被拒绝")
	}
	if _, err := svc.ReadImageBinary(root, "assets/missing.png"); err == nil {
		t.Fatal("文件不存在应报错")
	}
}
