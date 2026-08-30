package plugin

import (
	"path/filepath"
	"testing"

	"github.com/notevault/notevault/internal/core"
)

// ---------------------------------------------------------------------------
// 插件信任等级（trust=full）与授权状态
//
// 安全模型：插件默认在 Worker 沙箱里跑；只有 manifest 声明 trust=full
// 且用户逐插件显式授权的插件才允许在主进程运行。
// 授权与源码哈希绑定——插件一更新，授权自动失效。
// ---------------------------------------------------------------------------

func mustPluginInfo(t *testing.T, s *PluginService, id string) *core.PluginInfo {
	t.Helper()
	info, err := s.GetPlugin(id)
	if err != nil {
		t.Fatalf("GetPlugin(%q) failed: %v", id, err)
	}
	return info
}

func TestTrust_DefaultsToSandboxWhenUndeclared(t *testing.T) {
	s, dir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(dir, "plain.js"), "/*---\nid: plain\nname: Plain\n---*/\nbody")

	info := mustPluginInfo(t, s, "plain")
	if info.Manifest.Trust != core.TrustSandbox {
		t.Errorf("trust = %q, want %q", info.Manifest.Trust, core.TrustSandbox)
	}
	if info.TrustGranted {
		t.Error("undeclared plugin must never be granted")
	}
}

func TestTrust_FullDeclarationIsParsed(t *testing.T) {
	s, dir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(dir, "full.js"),
		"/*---\nid: full\nname: Full\ntrust: full\n---*/\nbody")

	info := mustPluginInfo(t, s, "full")
	if info.Manifest.Trust != core.TrustFull {
		t.Fatalf("trust = %q, want %q", info.Manifest.Trust, core.TrustFull)
	}
	// 声明了 full 不等于已被授权
	if info.TrustGranted {
		t.Error("declaring trust=full must not auto-grant it")
	}
}

func TestTrust_UnknownLevelRejectsPlugin(t *testing.T) {
	s, dir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(dir, "bad.js"),
		"/*---\nid: bad\nname: Bad\ntrust: root\n---*/\nbody")

	info := mustPluginInfo(t, s, "bad")
	if !info.HasError {
		t.Fatal("unknown trust level should make the plugin fail to load")
	}
	// 非法值不允许静默降级成 sandbox——否则作者会误以为声明生效
	if info.Manifest.Trust == core.TrustSandbox {
		t.Error("unknown trust level must not be silently downgraded to sandbox")
	}
}

func TestTrust_GrantRequiresFullDeclaration(t *testing.T) {
	s, dir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(dir, "plain.js"), "/*---\nid: plain\nname: Plain\n---*/\nbody")

	if err := s.GrantTrust("plain"); err == nil {
		t.Fatal("granting trust to a sandbox plugin should fail")
	}
	if mustPluginInfo(t, s, "plain").TrustGranted {
		t.Error("sandbox plugin must not be granted")
	}
}

func TestTrust_GrantAndRevoke(t *testing.T) {
	s, dir := newPluginTestService(t)
	writePluginFile(t, filepath.Join(dir, "full.js"),
		"/*---\nid: full\nname: Full\ntrust: full\n---*/\nbody")

	if err := s.GrantTrust("full"); err != nil {
		t.Fatalf("GrantTrust failed: %v", err)
	}
	if !mustPluginInfo(t, s, "full").TrustGranted {
		t.Fatal("expected trustGranted after granting")
	}

	if err := s.RevokeTrust("full"); err != nil {
		t.Fatalf("RevokeTrust failed: %v", err)
	}
	if mustPluginInfo(t, s, "full").TrustGranted {
		t.Fatal("trustGranted must be cleared after revoking")
	}
}

// TestTrust_GrantInvalidatedBySourceChange 是最关键的一条：
// 授权与源码哈希绑定，插件更新后必须自动失效，
// 否则「先用无害版本骗取授权、再通过更新投递恶意代码」就可行了。
func TestTrust_GrantInvalidatedBySourceChange(t *testing.T) {
	s, dir := newPluginTestService(t)
	path := filepath.Join(dir, "full.js")
	writePluginFile(t, path, "/*---\nid: full\nname: Full\ntrust: full\n---*/\nversion one")

	if err := s.GrantTrust("full"); err != nil {
		t.Fatalf("GrantTrust failed: %v", err)
	}
	if !mustPluginInfo(t, s, "full").TrustGranted {
		t.Fatal("expected trustGranted right after granting")
	}

	// 插件"更新"：内容变化 → 哈希变化
	writePluginFile(t, path, "/*---\nid: full\nname: Full\ntrust: full\n---*/\nversion two")

	if mustPluginInfo(t, s, "full").TrustGranted {
		t.Fatal("SECURITY: trust must be revoked automatically when the plugin source changes")
	}
}

func TestTrust_RevokeOnMissingPluginIsIdempotent(t *testing.T) {
	s, _ := newPluginTestService(t)
	if err := s.RevokeTrust("never-existed"); err != nil {
		t.Errorf("revoking a plugin without a grant should be a no-op, got %v", err)
	}
	if err := s.GrantTrust(""); err == nil {
		t.Error("empty id should be rejected")
	}
}
