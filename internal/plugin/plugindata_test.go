package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// 插件私有数据持久化（#29）
//
// 数据存在「应用数据目录 / plugins-data」而不是工作区：
// 它属于插件自己的配置，不是用户的笔记。
// ---------------------------------------------------------------------------

func TestPluginData_RoundTrip(t *testing.T) {
	s, _ := newPluginTestService(t)

	if err := s.SavePluginData("demo", `{"count":3}`); err != nil {
		t.Fatalf("SavePluginData failed: %v", err)
	}
	got, err := s.LoadPluginData("demo")
	if err != nil {
		t.Fatalf("LoadPluginData failed: %v", err)
	}
	if got != `{"count":3}` {
		t.Errorf("data = %q, want %q", got, `{"count":3}`)
	}

	// 覆盖写
	if err := s.SavePluginData("demo", `{"count":4}`); err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}
	if got, _ := s.LoadPluginData("demo"); got != `{"count":4}` {
		t.Errorf("after overwrite data = %q, want %q", got, `{"count":4}`)
	}
}

func TestPluginData_MissingReturnsEmptyNotError(t *testing.T) {
	s, _ := newPluginTestService(t)

	// 插件首次运行本来就没有数据，不应报错
	got, err := s.LoadPluginData("never-used")
	if err != nil {
		t.Fatalf("missing data should not be an error, got %v", err)
	}
	if got != "" {
		t.Errorf("missing data = %q, want empty string", got)
	}
}

// TestPluginData_RejectsPathTraversal 是最关键的一条。
//
// 插件 ID 来自前端调用，却被直接拼进文件路径（plugins-data/<id>.json）。
// 不做校验的话，传 "../../evil" 就能读写应用数据目录之外的任意文件。
func TestPluginData_RejectsPathTraversal(t *testing.T) {
	s, dir := newPluginTestService(t)
	// tempdir 结构是 <dir>/plugins，所以 <dir> 的上一级就是逃逸目标
	escaped := filepath.Join(filepath.Dir(dir), "pwned.json")

	for _, id := range []string{
		"../pwned",
		"..\\pwned",
		"../../pwned",
		"a/b",
		".",
		"..",
		"",
		"demo/../../pwned",
	} {
		if err := s.SavePluginData(id, "malicious"); err == nil {
			t.Errorf("SavePluginData(%q) should be rejected", id)
		}
		if _, err := s.LoadPluginData(id); err == nil {
			t.Errorf("LoadPluginData(%q) should be rejected", id)
		}
	}

	if _, err := os.Stat(escaped); err == nil {
		t.Fatalf("SECURITY: plugin data escaped the plugins-data directory (%s was created)", escaped)
	}
}
