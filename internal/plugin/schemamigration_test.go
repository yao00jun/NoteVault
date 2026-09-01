package plugin

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/notevault/notevault/internal/infra/schema"
)

// E-7 统一 schema version 在插件侧的回归测试。
//
// 插件的两份状态文件原先是裸 map：启用状态 {pluginId: true}、
// 信任授权 {pluginId: {hash, grantedAt}}。加信封后必须仍能读旧文件，
// 否则用户升级一次会发现「插件全变回禁用、所有授权要重新点一遍」。

func writeLegacyJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("序列化旧格式失败: %v", err)
	}
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatalf("写入旧格式失败: %v", err)
	}
}

func readEnvelopeMeta(t *testing.T, path string) (int, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	var meta struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("解析 %s 失败: %v", path, err)
	}
	return meta.SchemaVersion, meta.Kind
}

func TestSchema_PluginStateReadsLegacyBareMap(t *testing.T) {
	s, _ := newPluginTestService(t)

	writeLegacyJSON(t, s.stateFile, map[string]bool{"alpha": true, "beta": false})

	state := s.loadEnabledState()
	if !state["alpha"] {
		t.Error("旧格式启用状态丢失：alpha 应为启用")
	}
	if state["beta"] {
		t.Error("旧格式启用状态读错：beta 应为禁用")
	}

	// 写一次即升级
	if err := s.saveEnabledState(state); err != nil {
		t.Fatalf("saveEnabledState: %v", err)
	}
	version, kind := readEnvelopeMeta(t, s.stateFile)
	if version != schema.PluginState.Version || kind != schema.PluginState.Kind {
		t.Errorf("升级后信封元信息 = (%d, %q)，want (%d, %q)",
			version, kind, schema.PluginState.Version, schema.PluginState.Kind)
	}

	again := s.loadEnabledState()
	if !again["alpha"] || again["beta"] {
		t.Errorf("升级后启用状态不一致: %+v", again)
	}
}

func TestSchema_PluginTrustReadsLegacyBareMap(t *testing.T) {
	s, _ := newPluginTestService(t)
	granted := time.Now().Add(-24 * time.Hour).Truncate(time.Second)

	writeLegacyJSON(t, s.trustFile, map[string]trustRecord{
		"alpha": {Hash: "abc123", GrantedAt: granted},
	})

	trust := s.loadTrustState()
	rec, ok := trust["alpha"]
	if !ok {
		t.Fatal("旧格式信任授权丢失：alpha 应仍被授权（否则用户要重新确认一遍）")
	}
	if rec.Hash != "abc123" {
		t.Errorf("hash = %q, want abc123", rec.Hash)
	}
	if !rec.GrantedAt.Equal(granted) {
		t.Errorf("grantedAt = %v, want %v", rec.GrantedAt, granted)
	}

	if err := s.saveTrustState(trust); err != nil {
		t.Fatalf("saveTrustState: %v", err)
	}
	version, kind := readEnvelopeMeta(t, s.trustFile)
	if version != schema.PluginTrust.Version || kind != schema.PluginTrust.Kind {
		t.Errorf("升级后信封元信息 = (%d, %q)，want (%d, %q)",
			version, kind, schema.PluginTrust.Version, schema.PluginTrust.Kind)
	}

	again := s.loadTrustState()
	if again["alpha"].Hash != "abc123" {
		t.Errorf("升级后授权不一致: %+v", again)
	}
}

// TestSchema_PluginTrustRejectsNewerVersion 安全方向的默认值：
// 高版本可能给授权加了我们读不懂的约束（作用域、有效期），
// 按当前结构解析等于无视这些约束、可能误放行。宁可让用户重新确认。
func TestSchema_PluginTrustRejectsNewerVersion(t *testing.T) {
	s, _ := newPluginTestService(t)

	data, err := schema.Marshal(schema.PluginTrust.Kind, schema.PluginTrust.Version+1,
		map[string]trustRecord{"alpha": {Hash: "abc123", GrantedAt: time.Now()}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(s.trustFile, data, 0640); err != nil {
		t.Fatalf("写入高版本信任状态失败: %v", err)
	}

	if trust := s.loadTrustState(); len(trust) != 0 {
		t.Errorf("高版本信任状态必须退化为「无任何授权」，得到 %+v", trust)
	}
}

// TestSchema_PluginStateKeepsNewerVersionBestEffort 启用状态与信任状态取向不同：
// 启用位读错的代价只是用户重新点一下开关，比整份状态丢弃体验更好。
func TestSchema_PluginStateKeepsNewerVersionBestEffort(t *testing.T) {
	s, _ := newPluginTestService(t)

	data, err := schema.Marshal(schema.PluginState.Kind, schema.PluginState.Version+1,
		map[string]bool{"alpha": true})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(s.stateFile, data, 0640); err != nil {
		t.Fatalf("写入高版本启用状态失败: %v", err)
	}

	if state := s.loadEnabledState(); !state["alpha"] {
		t.Error("高版本启用状态应尽力解析，alpha 期望为启用")
	}
}

func TestSchema_PluginStateTreatsCorruptFileAsEmpty(t *testing.T) {
	s, _ := newPluginTestService(t)

	if err := os.WriteFile(s.stateFile, []byte(`{"alpha":`), 0640); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	if state := s.loadEnabledState(); len(state) != 0 {
		t.Errorf("损坏的启用状态应视为空，得到 %+v", state)
	}

	if err := os.WriteFile(s.trustFile, []byte(`not json`), 0640); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}
	if trust := s.loadTrustState(); len(trust) != 0 {
		t.Errorf("损坏的信任状态应视为无授权，得到 %+v", trust)
	}
}

// TestSchema_TrustStateIsWrittenAtomically 锁住「信任状态走原子写」这条修复：
// 原先用 os.WriteFile 覆盖写，半截写入会让所有授权失效。
func TestSchema_TrustStateIsWrittenAtomically(t *testing.T) {
	s, _ := newPluginTestService(t)

	if err := s.saveTrustState(map[string]trustRecord{"alpha": {Hash: "h"}}); err != nil {
		t.Fatalf("saveTrustState: %v", err)
	}
	if _, err := os.Stat(s.trustFile + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("原子写不应留下 .tmp 残留 (stat err = %v)", err)
	}
	if _, err := os.Stat(s.trustFile); err != nil {
		t.Errorf("信任状态文件应存在: %v", err)
	}
}
