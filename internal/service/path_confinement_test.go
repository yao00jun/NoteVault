package service

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 本文件是路径安全契约测试：任何面向前端的相对路径参数都必须经过
// confineToWorkspace 收敛，否则 filepath.Join 会因 ".." 或绝对路径
// 丢掉工作区前缀，变成读写删任意文件的原语。

// TestPathConfinement_StaticScan 静态 tripwire：扫描 service 包源码，
// 任何 `filepath.Join(workspacePath, x)` / `filepath.Join(<dir>, x)` 中
// x 来自变量的行，必须出现在 confineToWorkspace 的调用行内。
// 新增绕过点时此测试失败，提示改走 confineToWorkspace。
func TestPathConfinement_StaticScan(t *testing.T) {
	joinRe := regexp.MustCompile(`filepath\.Join\((workspacePath|trashDir|archiveDir|fullPath), ([a-zA-Z_][a-zA-Z0-9_.]*)\)`)
	confinRe := regexp.MustCompile(`confineToWorkspace\([^,]+, `)

	// 豁免清单：(文件, 第二参数)。均为"入口已校验"或"包内常量"的安全拼接，
	// 新增豁免必须在此注明理由。
	allow := map[string]string{
		"fileservice.go:clean":                "confineToWorkspace 函数本体",
		"compileservice.go:s.inboxDir":        "inboxDir 为服务内部常量目录名，非用户输入",
		"templateservice.go:templatesDirName": "模板根目录为包级常量，模板名另行经 templatePath 校验",
		"importservice.go:finalRel":           "函数入口已用 confineToWorkspace 校验 targetRel",
		"importservice.go:candidate":          "candidate 由已校验的 finalRel 生成（rename 冲突策略）",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			m := joinRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if strings.Contains(line, "confineToWorkspace") || confinRe.MatchString(line) {
				continue
			}
			// 第二参数是包级常量字符串字面量的调用点（如 "assets"）允许豁免：
			// 正则只匹配变量名，字面量本来就不会命中。
			if reason, ok := allow[name+":"+m[2]]; ok {
				_ = reason
				continue
			}
			t.Errorf("%s:%d: 检测到未经 confineToWorkspace 的路径拼接: %s",
				name, i+1, strings.TrimSpace(line))
		}
	}
}

// TestPathConfinement_TrashRejectsEscape 回收站移动/恢复不得越出工作区
func TestPathConfinement_TrashRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	// 工作区外的"受害者"文件
	outside := filepath.Dir(dir)
	victim := filepath.Join(outside, "victim_trash_test.md")
	if err := os.WriteFile(victim, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(victim)

	s := NewTrashService()
	if _, err := s.MoveToTrash(dir, filepath.Join("..", filepath.Base(dir), "..", "victim_trash_test.md")); err == nil {
		t.Fatal("MoveToTrash 应拒绝越出工作区的相对路径")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("工作区外文件不应被移动")
	}

	// 篡改索引里的 OriginalPath 后恢复，也不得写到工作区外
	if err := os.MkdirAll(filepath.Join(dir, ".notevault"), 0750); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(dir, "staged.md")
	if err := os.WriteFile(inside, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	tr, err := s.MoveToTrash(dir, "staged.md")
	if err != nil {
		t.Fatal(err)
	}
	files, err := s.loadTrashIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	files[0].OriginalPath = "../victim_restore_test.md"
	if err := s.saveTrashIndex(dir, files); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreFromTrash(dir, tr.ID); err == nil {
		t.Fatal("RestoreFromTrash 应拒绝越界恢复目标")
	}
	if _, err := os.Stat(filepath.Join(outside, "victim_restore_test.md")); !os.IsNotExist(err) {
		t.Fatal("越界恢复目标不应被创建")
	}
}

// TestPathConfinement_ArchiveRejectsEscape 归档/取消归档不得越出工作区
func TestPathConfinement_ArchiveRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Dir(dir)
	victim := filepath.Join(outside, "victim_archive_test.md")
	if err := os.WriteFile(victim, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(victim)

	s := NewArchiveService()
	escape := filepath.Join("..", filepath.Base(dir), "..", "victim_archive_test.md")
	if _, err := s.ArchiveFile(dir, escape); err == nil {
		t.Fatal("ArchiveFile 应拒绝越出工作区的相对路径")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("工作区外文件不应被移动")
	}
	if err := s.UnarchiveFile(dir, escape); err == nil {
		t.Fatal("UnarchiveFile 应拒绝越界路径")
	}
	if s.IsArchived(dir, escape) {
		t.Fatal("IsArchived 对越界路径应返回 false")
	}
}

// TestPathConfinement_ExportRejectsEscape 导出不得读出工作区外文件
func TestPathConfinement_ExportRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Dir(dir)
	victim := filepath.Join(outside, "victim_export_test.md")
	if err := os.WriteFile(victim, []byte("top secret"), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(victim)

	s := NewExportService()
	dest := filepath.Join(dir, "exported.md")
	escape := filepath.Join("..", filepath.Base(dir), "..", "victim_export_test.md")
	if err := s.ExportNoteMarkdown(dir, escape, dest); err == nil {
		t.Fatal("ExportNoteMarkdown 应拒绝越界读取")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("越界读取的内容不应被写出")
	}
}

// TestPathConfinement_ImportRejectsEscape 导入目标不得越出工作区
func TestPathConfinement_ImportRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	s := NewImportService()
	result := &ImportResult{}
	err := s.writeToWorkspace(dir, filepath.Join("..", "escape_test.md"), []byte("x"), ConflictSkip, map[string]bool{}, result)
	if err == nil {
		t.Fatal("writeToWorkspace 应拒绝越界目标路径")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape_test.md")); !os.IsNotExist(err) {
		t.Fatal("越界目标不应被创建")
	}
}
