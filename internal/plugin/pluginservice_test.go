package plugin

import (
	"github.com/notevault/notevault/internal/core"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newPluginTestService(t *testing.T) (*PluginService, string) {
	t.Helper()
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	s := &PluginService{
		pluginsDir: pluginsDir,
		stateFile:  filepath.Join(dir, "plugins-state.json"),
		trustFile:  filepath.Join(dir, "plugins-trust.json"),
	}
	if err := os.MkdirAll(pluginsDir, 0750); err != nil {
		t.Fatal(err)
	}
	return s, pluginsDir
}

func writePluginFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPlugin_ListEmpty(t *testing.T) {
	s, _ := newPluginTestService(t)
	plugins, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins failed: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected empty list, got %d", len(plugins))
	}
}

func TestPlugin_FrontMatterParse(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "hello.js"), `/*---
id: hello-world
name: Hello World
version: 1.2.0
author: tester
description: A demo plugin
---*/
console.log("hello");
`)
	plugins, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins failed: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Manifest.ID != "hello-world" {
		t.Errorf("ID = %q, want hello-world", p.Manifest.ID)
	}
	if p.Manifest.Name != "Hello World" {
		t.Errorf("Name = %q", p.Manifest.Name)
	}
	if p.Manifest.Version != "1.2.0" {
		t.Errorf("Version = %q", p.Manifest.Version)
	}
	if p.Manifest.Author != "tester" {
		t.Errorf("Author = %q", p.Manifest.Author)
	}
	if p.Source != "console.log(\"hello\");\n" {
		t.Errorf("Source wrong: %q", p.Source)
	}
	if p.HasError {
		t.Errorf("HasError should be false, err: %s", p.LoadError)
	}
}

func TestPlugin_KVCommentParse(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "kv.js"), `// @plugin id=kv-plugin name="KV Plugin" version=2.0.0
export function activate() {}
`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	if p.Manifest.ID != "kv-plugin" {
		t.Errorf("ID = %q", p.Manifest.ID)
	}
	if p.Manifest.Name != "KV Plugin" {
		t.Errorf("Name = %q (quotes should be stripped)", p.Manifest.Name)
	}
	if p.Manifest.Version != "2.0.0" {
		t.Errorf("Version = %q", p.Manifest.Version)
	}
}

func TestPlugin_BareScriptNoManifest(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "bare.js"), `// just a script
console.log("no manifest");
`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	p := plugins[0]
	// 默认 ID 用文件名（去后缀）
	if p.Manifest.ID != "bare" {
		t.Errorf("default ID = %q, want bare", p.Manifest.ID)
	}
	if p.HasError {
		t.Errorf("bare script should not be an error: %s", p.LoadError)
	}
}

func TestPlugin_NonPluginFileIgnored(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "readme.md"), "# Readme")
	writePluginFile(t, filepath.Join(pluginsDir, "subdir"), "subdir")
	writePluginFile(t, filepath.Join(pluginsDir, "real.js"), `// @plugin id=real name=Real`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 1 {
		t.Errorf("expected only 1 .js plugin, got %d", len(plugins))
	}
}

func TestPlugin_EnableDisablePersist(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "p1.js"), `// @plugin id=p1 name=P1`)
	writePluginFile(t, filepath.Join(pluginsDir, "p2.js"), `// @plugin id=p2 name=P2`)

	// 默认全 false
	plugins, _ := s.ListPlugins()
	for _, p := range plugins {
		if p.Enabled {
			t.Errorf("%s should be disabled by default", p.Manifest.ID)
		}
	}

	// 启用 p1
	if err := s.EnablePlugin("p1"); err != nil {
		t.Fatalf("EnablePlugin failed: %v", err)
	}
	// 重新加载验证
	plugins, _ = s.ListPlugins()
	enabled := map[string]bool{}
	for _, p := range plugins {
		enabled[p.Manifest.ID] = p.Enabled
	}
	if !enabled["p1"] {
		t.Error("p1 should be enabled after EnablePlugin")
	}
	if enabled["p2"] {
		t.Error("p2 should remain disabled")
	}

	// 禁用 p1
	if err := s.DisablePlugin("p1"); err != nil {
		t.Fatalf("DisablePlugin failed: %v", err)
	}
	plugins, _ = s.ListPlugins()
	for _, p := range plugins {
		if p.Enabled {
			t.Errorf("%s should be disabled after DisablePlugin", p.Manifest.ID)
		}
	}
}

func TestPlugin_EnableNonExistentReturnsError(t *testing.T) {
	s, _ := newPluginTestService(t)
	if err := s.EnablePlugin("doesnotexist"); !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("expected core.ErrNotFound, got %v", err)
	}
}

func TestPlugin_GetPluginByID(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "alpha.js"), `// @plugin id=alpha name=Alpha`)
	writePluginFile(t, filepath.Join(pluginsDir, "beta.js"), `// @plugin id=beta name=Beta`)
	p, err := s.GetPlugin("beta")
	if err != nil {
		t.Fatalf("GetPlugin failed: %v", err)
	}
	if p.Manifest.ID != "beta" {
		t.Errorf("got wrong plugin ID: %s", p.Manifest.ID)
	}
	// GetPlugin 必含 FilePath（非空字符串）；单行注释文件的 Source 可能为空
	if p.FilePath == "" {
		t.Error("FilePath should be populated")
	}
}

func TestPlugin_GetPluginInvalid(t *testing.T) {
	s, _ := newPluginTestService(t)
	if _, err := s.GetPlugin(""); !core.IsCode(err, core.ErrInvalidInput) {
		t.Errorf("empty ID should be core.ErrInvalidInput, got %v", err)
	}
	if _, err := s.GetPlugin("nope"); !core.IsCode(err, core.ErrNotFound) {
		t.Errorf("non-existent ID should be core.ErrNotFound, got %v", err)
	}
}

func TestPlugin_GetPluginsDir(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	if got := s.GetPluginsDir(); got != pluginsDir {
		t.Errorf("GetPluginsDir = %q, want %q", got, pluginsDir)
	}
}

func TestPlugin_BrokenFrontMatterHasError(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	// 故意写带起始标记但无结束标记
	writePluginFile(t, filepath.Join(pluginsDir, "broken.js"), `/*---
id: broken
name: Broken
console.log("unclosed front matter");
`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if !plugins[0].HasError {
		t.Error("expected HasError=true for unclosed front matter")
	}
}

func TestPlugin_HashChangesOnContentChange(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	path := filepath.Join(pluginsDir, "ver.js")
	writePluginFile(t, path, `// @plugin id=ver name=V1`)
	p1, _ := s.ListPlugins()
	hash1 := p1[0].Hash
	writePluginFile(t, path, `// @plugin id=ver name=V2
// different content`)
	p2, _ := s.ListPlugins()
	hash2 := p2[0].Hash
	if hash1 == hash2 {
		t.Error("hash should change when content changes")
	}
}

func TestPlugin_SortedByName(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "zeta.js"), `// @plugin id=z name=Zeta`)
	writePluginFile(t, filepath.Join(pluginsDir, "alpha.js"), `// @plugin id=a name=Alpha`)
	writePluginFile(t, filepath.Join(pluginsDir, "mid.js"), `// @plugin id=m name=Mid`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 3 {
		t.Fatalf("expected 3 plugins, got %d", len(plugins))
	}
	if plugins[0].Manifest.Name != "Alpha" || plugins[1].Manifest.Name != "Mid" || plugins[2].Manifest.Name != "Zeta" {
		var names []string
		for _, p := range plugins {
			names = append(names, p.Manifest.Name)
		}
		t.Errorf("expected sorted by name, got %v", names)
	}
}

func TestParsePluginManifest_Defaults(t *testing.T) {
	m, _, err := parsePluginManifest("just plain code", "default.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ID != "default" {
		t.Errorf("ID = %q, want default", m.ID)
	}
	if m.Name != "default" {
		t.Errorf("Name = %q, want default", m.Name)
	}
	if m.Version != "0.0.0" {
		t.Errorf("Version = %q, want 0.0.0", m.Version)
	}
}

func TestParsePluginManifest_QouteStripped(t *testing.T) {
	// 单引号 / 双引号 / 反引号 都应被剥离
	m, _, _ := parsePluginManifest("/*---\nname: `Quoted`\nid: \"with-quotes\"\n---*/", "x.js")
	if m.Name != "Quoted" {
		t.Errorf("Name = %q, want Quoted", m.Name)
	}
	if m.ID != "with-quotes" {
		t.Errorf("ID = %q", m.ID)
	}
}

func TestParsePluginManifest_Permissions(t *testing.T) {
	m, _, err := parsePluginManifest(`/*---
id: perms
name: Perms
permissions: workspace.read, workspace.write ,commands,notifications
---*/notevault.registerCommand({})
`, "perms.js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"workspace.read", "workspace.write", "commands", "notifications"}
	if !slices.Equal(m.Permissions, want) {
		t.Fatalf("Permissions = %#v, want %#v", m.Permissions, want)
	}
}

func TestParsePluginManifest_PermissionDuplicateRejected(t *testing.T) {
	m, _, err := parsePluginManifest(`/*---
id: duplicate
permissions: commands, commands
---*/console.log("x")`, "duplicate.js")
	if err == nil {
		t.Fatal("expected duplicate permission error")
	}
	if !strings.Contains(err.Error(), "重复权限") {
		t.Fatalf("error = %v", err)
	}
	if len(m.Permissions) != 0 {
		t.Fatalf("invalid permissions = %#v", m.Permissions)
	}
}

func TestPlugin_UnknownPermissionRejected(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "bad.js"), `// @plugin id=bad name=Bad permissions=file.system,commands`)
	plugins, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins failed: %v", err)
	}
	if len(plugins) != 1 || !plugins[0].HasError {
		t.Fatalf("expected discovered plugin with HasError=true, got %+v", plugins)
	}
	if !strings.Contains(plugins[0].LoadError, "未知权限") {
		t.Fatalf("LoadError = %q", plugins[0].LoadError)
	}
	p, err := s.GetPlugin("bad")
	if err != nil {
		t.Fatalf("GetPlugin failed: %v", err)
	}
	if slices.Contains(p.Manifest.Permissions, "file.system") {
		t.Fatalf("unknown permission must not be retained: %#v", p.Manifest.Permissions)
	}
}

func TestPlugin_MJSExtension(t *testing.T) {
	s, pluginsDir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(pluginsDir, "mod.mjs"), `// @plugin id=mod name=Mod`)
	plugins, _ := s.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected .mjs to be recognized, got %d plugins", len(plugins))
	}
}
