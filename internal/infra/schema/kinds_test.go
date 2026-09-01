package schema

import (
	"strings"
	"testing"
)

func TestAll_KindsAreUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range All() {
		if d.Kind == "" {
			t.Errorf("descriptor %+v has an empty kind", d)
			continue
		}
		if seen[d.Kind] {
			t.Errorf("kind %q is registered twice — kind 必须全局唯一，否则 ErrKindMismatch 形同虚设", d.Kind)
		}
		seen[d.Kind] = true

		if d.Version <= 0 {
			t.Errorf("kind %q has a non-positive version %d", d.Kind, d.Version)
		}
		if d.Location == "" {
			t.Errorf("kind %q has no Location — 排查升级问题时这是唯一线索", d.Kind)
		}
		// kind 会写进 JSON 并被字符串比较，空格 / 大写都是将来对不上的隐患
		if d.Kind != strings.ToLower(d.Kind) || strings.ContainsAny(d.Kind, " \t") {
			t.Errorf("kind %q should be lowercase and free of whitespace", d.Kind)
		}
	}
}

func TestAll_CoversEveryRegisteredDescriptor(t *testing.T) {
	// 防止新增 Descriptor 变量但忘记加进 All()，那样上面的唯一性校验会漏掉它。
	//
	// 这里刻意逐个列出而不是断言一个数字：数字对不上时只知道"多了/少了一项"，
	// 还得回去数；列出来则直接指名道姓，而且新增描述符时改动只集中在这一处。
	want := []Descriptor{
		TrashIndex, Reminders, WorkspaceList, CurrentWorkspace,
		SnapshotIndex, SearchSummary, PluginState, PluginTrust,
		BaseDefinition,
	}

	registered := map[string]bool{}
	for _, d := range All() {
		registered[d.Kind] = true
	}
	expected := map[string]bool{}
	for _, d := range want {
		expected[d.Kind] = true
		if !registered[d.Kind] {
			t.Errorf("descriptor %q 没有登记进 All()——唯一性与迁移覆盖检查都会漏掉它", d.Kind)
		}
	}
	for kind := range registered {
		if !expected[kind] {
			t.Errorf("All() 里的 %q 没有出现在本测试的清单中——新增落盘文件时请同步这里", kind)
		}
	}
}

func TestMarshalAs_UnmarshalAs_RoundTrip(t *testing.T) {
	type payload struct {
		N int `json:"n"`
	}
	raw, err := MarshalAs(TrashIndex, payload{N: 7})
	if err != nil {
		t.Fatalf("MarshalAs: %v", err)
	}
	if !strings.Contains(string(raw), `"kind": "trash-index"`) {
		t.Errorf("kind 未写入磁盘: %s", raw)
	}

	got, res, err := UnmarshalAs[payload](raw, TrashIndex)
	if err != nil {
		t.Fatalf("UnmarshalAs: %v", err)
	}
	if res.Compat != CompatExact {
		t.Errorf("Compat = %v, want exact", res.Compat)
	}
	if got.N != 7 {
		t.Errorf("payload = %+v", got)
	}
}

func TestUnmarshalAs_RejectsCrossKindRead(t *testing.T) {
	raw, err := MarshalAs(Reminders, []int{1})
	if err != nil {
		t.Fatalf("MarshalAs: %v", err)
	}
	if _, _, err := UnmarshalAs[[]int](raw, TrashIndex); err == nil {
		t.Error("把提醒文件当回收站读应当报错——这类错误只会由路径算错引起")
	}
}
