//go:build !windows

package platform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// 非 Windows 平台的降级实现（P2-5）。
//
// macOS Keychain（Security.framework）与 Linux libsecret 都需要各自平台的
// 绑定，暂时用配置目录下的 0600 权限 JSON 文件兜底：
//   - 权限收紧到仅属主可读写（对比 localStorage 至少不暴露给 WebView 任意 JS）；
//   - 原子写，崩溃不留半截文件；
//   - 接口不变，将来换 Keychain / libsecret 时上层零改动。
//
// 文件内容形如 {"ai.apiKey": "sk-..."}——反序列化失败视为损坏，整体作废重来。

type fileCredentialStore struct {
	path string
}

// newCredentialStore 返回当前平台的实现（见 credential.go 的平台分发表）。
func newCredentialStore(prefix string) CredentialStore {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "NoteVault")
	_ = os.MkdirAll(dir, 0700)
	return fileCredentialStore{
		path: filepath.Join(dir, prefix+"-credentials.json"),
	}
}

func (s fileCredentialStore) loadAll() (map[string]string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		// 损坏文件不阻塞功能：丢掉旧值重新开始（密钥可由用户重填）
		return map[string]string{}, nil
	}
	return out, nil
}

func (s fileCredentialStore) Save(key, value string) error {
	all, err := s.loadAll()
	if err != nil {
		return err
	}
	all[key] = value
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path+".tmp", data, 0600); err != nil {
		return err
	}
	// 原子替换：崩溃时不留半截 JSON
	return os.Rename(s.path+".tmp", s.path)
}

func (s fileCredentialStore) Load(key string) (string, error) {
	all, err := s.loadAll()
	if err != nil {
		return "", err
	}
	return all[key], nil
}

func (s fileCredentialStore) Delete(key string) error {
	all, err := s.loadAll()
	if err != nil {
		return err
	}
	if _, ok := all[key]; !ok {
		return nil
	}
	delete(all, key)
	data, err := json.Marshal(all)
	if err != nil {
		return fmt.Errorf("序列化凭据失败: %w", err)
	}
	if err := os.WriteFile(s.path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(s.path+".tmp", s.path)
}
