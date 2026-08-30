package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 预装插件（出厂预装）
//
// 工具栏插件化之后宿主不再内置任何按钮，所以必须预装一个编辑工具栏，
// 否则新用户第一次打开会看到一个空工具栏。
// 这几条测试守住预装的三个边界：装上、不覆盖、不擅自重新启用。
// ---------------------------------------------------------------------------

func TestBundledPlugin_InstalledAndEnabledOnFirstRun(t *testing.T) {
	s, dir := newPluginTestService(t)
	// helper 直接构造 struct（绕过 NewPluginService），所以这里显式触发预装，
	// 否则这条测试会假通过——生产路径是在 NewPluginService 里调用的。
	s.installBundledPlugins()

	target := filepath.Join(dir, "editing-toolbar.js")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("bundled plugin was not installed: %v", err)
	}

	// 源码里必须带 manifest，否则扫描时会被当成裸脚本
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read bundled plugin: %v", err)
	}
	manifest, _, err := parsePluginManifest(string(data), "editing-toolbar.js")
	if err != nil {
		t.Fatalf("bundled plugin has an invalid manifest: %v", err)
	}
	if manifest.ID != "editing-toolbar" {
		t.Errorf("manifest id = %q, want editing-toolbar", manifest.ID)
	}

	if !s.loadEnabledState()[manifest.ID] {
		t.Error("bundled plugin should be enabled by default")
	}
}

func TestBundledPlugin_DoesNotOverwriteUserEdits(t *testing.T) {
	_, dir := newPluginTestService(t)

	target := filepath.Join(dir, "editing-toolbar.js")
	// 模拟用户改过这个插件——带完整 manifest + 声明 ui 权限（合法修改形式）
	// 这种情况下预装机制应该尊重改动，不覆盖。
	if err := os.WriteFile(target, []byte(
		"/*---\nid: editing-toolbar\nname: my edit\npermissions: ui\n---*/\n// my custom toolbar\n",
	), 0640); err != nil {
		t.Fatalf("seed user edit: %v", err)
	}

	// 重启（重新构造 service 会再跑一次预装）
	NewPluginService(dir)

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "name: my edit") {
		// 保留的是用户版本（带 name: my edit）而非预装版（带 name: 编辑工具栏）
		t.Errorf("bundled plugin overwrote the user's edit: %q", string(data))
	}
}

func TestBundledPlugin_StaysDisabledWhenUserDisabledIt(t *testing.T) {
	s, dir := newPluginTestService(t)

	if err := s.DisablePlugin("editing-toolbar"); err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}

	// 重启后必须保持禁用——预装不能偷偷把用户的选择改回去
	NewPluginService(dir)

	if s.loadEnabledState()["editing-toolbar"] {
		t.Error("bundled plugin must not re-enable itself after the user disabled it")
	}
}

func TestBundledPlugin_UpgradesFileWithUnparseableManifest(t *testing.T) {
	_, dir := newPluginTestService(t)

	// 模拟用户机器上正在发生的状况：旧版文件头有 /// 引用行，
	// 导致 parsePluginManifest 把整份 manifest 丢掉，
	// permissions 解析为空 → 启动时所有 UI 注册都失败。
	target := filepath.Join(dir, "editing-toolbar.js")
	old := "/// <reference path=\"../foo.d.ts\" />\n// 老版本，没有任何 frontmatter\n"
	if err := os.WriteFile(target, []byte(old), 0640); err != nil {
		t.Fatal(err)
	}

	// 同时 enable，让 installBundledPlugins 走升级分支
	NewPluginService(dir)

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "permissions: ui") {
		t.Errorf("bundled plugin should overwrite unhealthy file with a healthy one")
	}
}

func TestBundledPlugin_UpgradesFileWithNoPermissions(t *testing.T) {
	_, dir := newPluginTestService(t)

	target := filepath.Join(dir, "editing-toolbar.js")
	// manifest 能解析（id 等都有）但 permissions 字段缺失——
	// 跟"损坏版"同病：所有 UI 注册都会 throw（虽然新版本里改成 send error），
	// 修起来仍然是用户的负担。预装应当升级。
	old := "/*---\nid: editing-toolbar\nname: 编辑工具栏\n---*/\nnotevault.registerToolbarButton({})\n"
	if err := os.WriteFile(target, []byte(old), 0640); err != nil {
		t.Fatal(err)
	}

	NewPluginService(dir)

	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "permissions: ui") {
		t.Errorf("bundled plugin should overwrite a no-permissions file")
	}
}
