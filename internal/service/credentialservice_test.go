package service

import (
	"errors"
	"testing"
)

// fakeCredentialStore 记录调用轨迹，验证 service 层的翻译规则。
type fakeCredentialStore struct {
	saved     map[string]string
	deleted   []string
	saveErr   error
	deleteErr error
}

func newFakeCredentialStore() *fakeCredentialStore {
	return &fakeCredentialStore{saved: map[string]string{}}
}

func (f *fakeCredentialStore) Save(key, value string) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved[key] = value
	return nil
}

func (f *fakeCredentialStore) Load(key string) (string, error) {
	if v, ok := f.saved[key]; ok {
		return v, nil
	}
	return "", nil
}

func (f *fakeCredentialStore) Delete(key string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.saved, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func TestCredentialService_SaveCredential(t *testing.T) {
	store := newFakeCredentialStore()
	svc := NewCredentialService(store)

	if err := svc.SaveCredential("ai.apiKey", "sk-secret"); err != nil {
		t.Fatalf("SaveCredential failed: %v", err)
	}
	if store.saved["ai.apiKey"] != "sk-secret" {
		t.Fatalf("底层应收到明文值, got %q", store.saved["ai.apiKey"])
	}

	// 空值翻译成删除
	if err := svc.SaveCredential("ai.apiKey", "  "); err != nil {
		t.Fatalf("空值 SaveCredential failed: %v", err)
	}
	if _, ok := store.saved["ai.apiKey"]; ok {
		t.Fatal("空值应触发底层 Delete 而不是 Save 空串")
	}
	if len(store.deleted) != 1 || store.deleted[0] != "ai.apiKey" {
		t.Fatalf("删除记录不符: %v", store.deleted)
	}

	// key 白名单：只放行 ai. 前缀下的合法名
	for _, bad := range []string{"", "apiKey", "other.key", "ai.", "ai.UPPER!", "../escape", "ai.apiKey;drop"} {
		if err := svc.SaveCredential(bad, "x"); err == nil {
			t.Errorf("key %q 应被拒绝", bad)
		}
	}
	// 合法名不应被误伤
	for _, good := range []string{"ai.apiKey", "ai.rerankKey"} {
		if err := svc.SaveCredential(good, "x"); err != nil {
			t.Errorf("key %q 应被放行: %v", good, err)
		}
	}
}

func TestCredentialService_GetCredential(t *testing.T) {
	store := newFakeCredentialStore()
	svc := NewCredentialService(store)

	// 不存在 → ("", nil)
	got, err := svc.GetCredential("ai.apiKey")
	if err != nil || got != "" {
		t.Fatalf("GetCredential(不存在) = (%q, %v), want (空, nil)", got, err)
	}

	if err := svc.SaveCredential("ai.apiKey", "sk-1"); err != nil {
		t.Fatalf("SaveCredential failed: %v", err)
	}
	got, err = svc.GetCredential("ai.apiKey")
	if err != nil || got != "sk-1" {
		t.Fatalf("GetCredential = (%q, %v), want (sk-1, nil)", got, err)
	}

	// 非法 key 直接拒绝
	if _, err := svc.GetCredential("bad"); err == nil {
		t.Fatal("非法 key 应被拒绝")
	}
}

func TestCredentialService_DeleteCredential(t *testing.T) {
	store := newFakeCredentialStore()
	svc := NewCredentialService(store)

	if err := svc.DeleteCredential("ai.apiKey"); err != nil {
		t.Fatalf("DeleteCredential(不存在) 应幂等成功: %v", err)
	}
	_ = svc.SaveCredential("ai.apiKey", "sk-1")
	if err := svc.DeleteCredential("ai.apiKey"); err != nil {
		t.Fatalf("DeleteCredential failed: %v", err)
	}
	got, _ := svc.GetCredential("ai.apiKey")
	if got != "" {
		t.Fatalf("删除后应读不到, got %q", got)
	}
}

func TestCredentialService_StoreErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	svc := NewCredentialService(&fakeCredentialStore{saveErr: boom, deleteErr: boom})

	if err := svc.SaveCredential("ai.apiKey", "sk-1"); !errors.Is(err, boom) {
		t.Fatalf("底层错误应透传, got %v", err)
	}
	if err := svc.DeleteCredential("ai.apiKey"); !errors.Is(err, boom) {
		t.Fatalf("底层错误应透传, got %v", err)
	}
}

func TestCredentialService_NilStoreFailsGracefully(t *testing.T) {
	svc := NewCredentialService(nil)

	if err := svc.SaveCredential("ai.apiKey", "sk-1"); err == nil {
		t.Fatal("nil store 应返回错误而不是 panic")
	}
	if _, err := svc.GetCredential("ai.apiKey"); err == nil {
		t.Fatal("nil store 应返回错误而不是 panic")
	}
	if err := svc.DeleteCredential("ai.apiKey"); err == nil {
		t.Fatal("nil store 应返回错误而不是 panic")
	}
}
