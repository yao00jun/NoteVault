package platform

import (
	"testing"
)

// TestCredentialStore_RoundTrip 对当前平台的实现做真实往返。
//
// Windows 上这会写进真实凭据管理器：用的是 NoteVault 前缀下的专用测试 key，
// 结束时 Delete 清理，不会残留。真刀真枪地验证 syscall 链路比 mock 有价值得多——
// 结构体字段错一个，这里第一个就炸。
func TestCredentialStore_RoundTrip(t *testing.T) {
	store := NewCredentialStore("NoteVault")
	key := "ai.testRoundtrip"
	t.Cleanup(func() {
		_ = store.Delete(key)
	})

	// 初始不存在：读出空串而不是错误
	got, err := store.Load(key)
	if err != nil {
		t.Fatalf("Load(不存在) 返回错误: %v", err)
	}
	if got != "" {
		t.Fatalf("Load(不存在) = %q, want 空串", got)
	}

	// 写入 → 读出
	if err := store.Save(key, "sk-test-value-123"); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err = store.Load(key)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if got != "sk-test-value-123" {
		t.Fatalf("往返值不一致: got %q", got)
	}

	// 覆盖写入
	if err := store.Save(key, "sk-updated"); err != nil {
		t.Fatalf("覆盖 Save 失败: %v", err)
	}
	got, err = store.Load(key)
	if err != nil {
		t.Fatalf("二次 Load 失败: %v", err)
	}
	if got != "sk-updated" {
		t.Fatalf("覆盖后值不一致: got %q", got)
	}

	// 删除 → 回到"不存在"状态；重复删除幂等
	if err := store.Delete(key); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if err := store.Delete(key); err != nil {
		t.Fatalf("重复 Delete 应幂等成功: %v", err)
	}
	got, err = store.Load(key)
	if err != nil || got != "" {
		t.Fatalf("删除后 Load = (%q, %v), want (空, nil)", got, err)
	}
}

func TestCredentialStore_UnicodeValue(t *testing.T) {
	store := NewCredentialStore("NoteVault")
	key := "ai.testUnicode"
	t.Cleanup(func() {
		_ = store.Delete(key)
	})

	// API Key 理论上都是 ASCII，但 UTF-16 编解码链路值得锁住——
	// blob 按字节截断之类的实现错误在这里会现形。
	value := "sk-密钥-🔐"
	if err := store.Save(key, value); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := store.Load(key)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if got != value {
		t.Fatalf("Unicode 往返不一致: got %q, want %q", got, value)
	}
}
