package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanOrphanAssets(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "assets"), 0750)
	os.MkdirAll(filepath.Join(ws, "notes"), 0750)
	// 引用中的图片 + 孤立图片 + 隐藏文件（不算资产）
	os.WriteFile(filepath.Join(ws, "assets", "used.png"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(ws, "assets", "orphan.png"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(ws, "assets", ".hidden"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(ws, "notes", "note.md"), []byte("![引用](assets/used.png)"), 0644)

	s := NewFileService()
	orphans, err := s.ScanOrphanAssets(ws)
	if err != nil {
		t.Fatalf("ScanOrphanAssets: %v", err)
	}
	if len(orphans) != 1 || !strings.Contains(orphans[0], "orphan.png") {
		t.Errorf("应只报 orphan.png, got %v", orphans)
	}
}

func TestScanOrphanAssets_NoAssetsDir(t *testing.T) {
	s := NewFileService()
	orphans, err := s.ScanOrphanAssets(t.TempDir())
	if err != nil {
		t.Fatalf("无 assets 目录不应报错: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("应返回空列表, got %v", orphans)
	}
}

func TestMoveOrphansToTrash(t *testing.T) {
	ws := t.TempDir()
	os.MkdirAll(filepath.Join(ws, "assets"), 0750)
	os.WriteFile(filepath.Join(ws, "assets", "orphan.png"), []byte("x"), 0644)

	s := NewFileService()
	moved, err := s.MoveOrphansToTrash(ws, []string{"assets/orphan.png"})
	if err != nil || moved != 1 {
		t.Fatalf("moved=%d err=%v", moved, err)
	}
	if _, serr := os.Stat(filepath.Join(ws, "assets", "orphan.png")); !os.IsNotExist(serr) {
		t.Error("原文件应已移走")
	}
	if _, serr := os.Stat(filepath.Join(ws, ".trash")); serr != nil {
		t.Error("应进入 .trash 可恢复")
	}
}
