package schema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const kindItems = "test-items"

func TestMarshal_ProducesVersionedEnvelope(t *testing.T) {
	data := []item{{ID: "a", Name: "A"}}
	raw, err := Marshal(kindItems, 3, data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// 断言磁盘形态，而不只是能 round-trip：磁盘格式是对外契约，
	// 字段名改了会让已装机用户的文件变成 legacy。
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if onDisk["schemaVersion"] != float64(3) {
		t.Errorf("schemaVersion = %v, want 3", onDisk["schemaVersion"])
	}
	if onDisk["kind"] != kindItems {
		t.Errorf("kind = %v, want %q", onDisk["kind"], kindItems)
	}
	if _, ok := onDisk["updatedAt"]; !ok {
		t.Error("updatedAt is missing")
	}
	if _, ok := onDisk["data"]; !ok {
		t.Error("data is missing")
	}
	if !strings.Contains(string(raw), "\n  \"kind\"") {
		t.Error("expected 2-space indented output for human inspection")
	}
}

func TestMarshal_RejectsInvalidMeta(t *testing.T) {
	if _, err := Marshal("", 1, []item{}); err == nil {
		t.Error("empty kind should be rejected")
	}
	if _, err := Marshal(kindItems, 0, []item{}); err == nil {
		t.Error("version 0 should be rejected")
	}
	if _, err := Marshal(kindItems, -1, []item{}); err == nil {
		t.Error("negative version should be rejected")
	}
}

func TestUnmarshal_RoundTripsExactVersion(t *testing.T) {
	want := []item{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
	raw, err := Marshal(kindItems, 2, want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, res, err := Unmarshal[[]item](raw, kindItems, 2)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if res.Compat != CompatExact {
		t.Errorf("Compat = %v, want exact", res.Compat)
	}
	if res.FileVersion != 2 {
		t.Errorf("FileVersion = %d, want 2", res.FileVersion)
	}
	if res.Kind != kindItems {
		t.Errorf("Kind = %q, want %q", res.Kind, kindItems)
	}
	if res.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be populated")
	}
	if time.Since(res.UpdatedAt) > time.Minute {
		t.Errorf("UpdatedAt looks wrong: %v", res.UpdatedAt)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].Name != "B" {
		t.Errorf("payload round-trip broken: %+v", got)
	}
	if res.NeedsRewrite() {
		t.Error("exact version should not need a rewrite")
	}
	if !res.Usable() {
		t.Error("exact version must be usable")
	}
}

// TestUnmarshal_LegacyBareArray 是这个包存在的首要理由：
// 已装机用户磁盘上的 trash.json / reminders.json / workspaces.json 就是裸数组，
// 读不出来等于丢数据。
func TestUnmarshal_LegacyBareArray(t *testing.T) {
	raw := []byte(`[{"id":"a","name":"A"},{"id":"b","name":"B"}]`)

	got, res, err := Unmarshal[[]item](raw, kindItems, 1)
	if err != nil {
		t.Fatalf("legacy bare array must still parse: %v", err)
	}
	if res.Compat != CompatLegacy {
		t.Errorf("Compat = %v, want legacy", res.Compat)
	}
	if res.FileVersion != 0 {
		t.Errorf("FileVersion = %d, want 0 for legacy", res.FileVersion)
	}
	if len(got) != 2 || got[0].ID != "a" {
		t.Errorf("legacy payload lost: %+v", got)
	}
	if !res.NeedsRewrite() {
		t.Error("legacy data should be flagged for rewrite")
	}
	if !res.Usable() {
		t.Error("legacy data must be usable")
	}
}

// TestUnmarshal_LegacyBareMap 覆盖插件启用状态 / 信任状态的旧格式。
func TestUnmarshal_LegacyBareMap(t *testing.T) {
	raw := []byte(`{"plugin-a":true,"plugin-b":false}`)

	got, res, err := Unmarshal[map[string]bool](raw, "plugin-state", 1)
	if err != nil {
		t.Fatalf("legacy bare map must still parse: %v", err)
	}
	if res.Compat != CompatLegacy {
		t.Errorf("Compat = %v, want legacy", res.Compat)
	}
	if !got["plugin-a"] || got["plugin-b"] {
		t.Errorf("legacy map payload wrong: %+v", got)
	}
}

// TestUnmarshal_LegacyBareMapWithConflictingKey 是探测逻辑的边界：
// 裸 map 里恰好有个叫 schemaVersion 的键（插件 ID 理论上可以是任意字符串），
// 探测结构会解析失败，必须回退直接解析而不是报错。
func TestUnmarshal_LegacyBareMapWithConflictingKey(t *testing.T) {
	raw := []byte(`{"schemaVersion":true,"plugin-a":true}`)

	got, res, err := Unmarshal[map[string]bool](raw, "plugin-state", 1)
	if err != nil {
		t.Fatalf("conflicting key must not break the legacy fallback: %v", err)
	}
	if res.Compat != CompatLegacy {
		t.Errorf("Compat = %v, want legacy", res.Compat)
	}
	if !got["plugin-a"] || !got["schemaVersion"] {
		t.Errorf("payload wrong: %+v", got)
	}
}

// TestUnmarshal_LegacyTopLevelVersionField 覆盖搜索摘要的旧格式：
// 它有 version 字段但没有 schemaVersion，也没有 data 包装，属于裸格式。
func TestUnmarshal_LegacyTopLevelVersionField(t *testing.T) {
	raw := []byte(`{"version":2,"entries":[{"id":"a","name":"A"}]}`)

	type oldSummary struct {
		Entries []item `json:"entries"`
	}
	got, res, err := Unmarshal[oldSummary](raw, "search-summary", 1)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if res.Compat != CompatLegacy {
		t.Errorf("Compat = %v, want legacy (version != schemaVersion)", res.Compat)
	}
	if len(got.Entries) != 1 {
		t.Errorf("payload lost: %+v", got)
	}
}

func TestUnmarshal_OlderVersion(t *testing.T) {
	raw, err := Marshal(kindItems, 1, []item{{ID: "a"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, res, err := Unmarshal[[]item](raw, kindItems, 3)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if res.Compat != CompatOlder {
		t.Errorf("Compat = %v, want older", res.Compat)
	}
	if res.FileVersion != 1 {
		t.Errorf("FileVersion = %d, want 1", res.FileVersion)
	}
	if !res.NeedsRewrite() {
		t.Error("older version should be flagged for rewrite")
	}
	if !res.Usable() {
		t.Error("older version is still usable by convention (additive changes)")
	}
	if len(got) != 1 {
		t.Errorf("payload lost: %+v", got)
	}
}

// TestUnmarshal_NewerVersion 覆盖降级安装 / 多版本并存：
// 高版本写的文件不能当成损坏（那会丢数据），但也不能默认可用（载荷结构可能变了）。
func TestUnmarshal_NewerVersion(t *testing.T) {
	raw, err := Marshal(kindItems, 9, []item{{ID: "a"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, res, err := Unmarshal[[]item](raw, kindItems, 2)
	if err != nil {
		t.Fatalf("newer version must not be reported as a parse error: %v", err)
	}
	if res.Compat != CompatNewer {
		t.Errorf("Compat = %v, want newer", res.Compat)
	}
	if res.Usable() {
		t.Error("newer version must not be blindly marked usable")
	}
	if res.NeedsRewrite() {
		t.Error("newer version must not be silently downgraded on rewrite")
	}
	// 载荷仍然解析出来，供调用方按数据性质决定是否尽力使用
	if len(got) != 1 {
		t.Errorf("payload should still be decoded for best-effort use: %+v", got)
	}
}

func TestUnmarshal_KindMismatch(t *testing.T) {
	raw, err := Marshal("reminders", 1, []item{{ID: "a"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	_, res, err := Unmarshal[[]item](raw, "trash-index", 1)
	if !errors.Is(err, ErrKindMismatch) {
		t.Fatalf("expected ErrKindMismatch, got %v", err)
	}
	if res.Kind != "reminders" {
		t.Errorf("Result should report the kind found on disk, got %q", res.Kind)
	}
}

func TestUnmarshal_CorruptInput(t *testing.T) {
	cases := map[string]string{
		"truncated":  `[{"id":"a"`,
		"empty":      ``,
		"whitespace": "  \n\t",
		"garbage":    `not json at all`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Unmarshal[[]item]([]byte(raw), kindItems, 1); err == nil {
				t.Error("expected an error for corrupt input")
			}
		})
	}
}

func TestCompat_String(t *testing.T) {
	cases := map[Compat]string{
		CompatExact:  "exact",
		CompatLegacy: "legacy",
		CompatOlder:  "older",
		CompatNewer:  "newer",
		Compat(42):   "compat(42)",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("Compat(%d).String() = %q, want %q", int(c), got, want)
		}
	}
}
