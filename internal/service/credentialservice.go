package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/notevault/notevault/internal/platform"
)

// credentialKeyPattern 限制可写入凭据库的 key 命名。
//
// 这是应用对系统凭据库的自律：只允许白名单前缀下的逻辑名（如 ai.apiKey），
// 防止前端任意字符串被写进系统级存储，污染用户机器的凭据列表，
// 也避免被当作"通用加密 KV"滥用。每开放一个新密钥，都要在这里手动放行。
var credentialKeyPattern = regexp.MustCompile(`^ai\.[a-z][a-zA-Z0-9]*$`)

// CredentialService 把 API Key 等敏感凭据从 localStorage 迁到系统凭据库（P2-5）。
//
// 为什么做成 Wails 服务而不是继续留在前端：凭据库只能由宿主进程访问，
// 前端拿到的只是"读写这个 key 的能力"，而且 value 不再落进 WebView 的
// localStorage——渲染进程被攻破也读不到明文。
type CredentialService struct {
	store platform.CredentialStore
}

// NewCredentialService 创建凭据服务。store 为 nil 时所有方法返回错误
// （零值实例可用，且行为可预期地失败而不是 panic）。
func NewCredentialService(store platform.CredentialStore) *CredentialService {
	return &CredentialService{store: store}
}

// SaveCredential 保存密钥。value 为空串等价于删除——"清空输入框"是前端
// 最自然的表达，不该强迫调用方记住还要再调一次 Delete。
func (s *CredentialService) SaveCredential(key, value string) error {
	if err := s.validateKey(key); err != nil {
		return err
	}
	if s.store == nil {
		return fmt.Errorf("密钥存储不可用")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return s.store.Delete(key)
	}
	return s.store.Save(key, value)
}

// GetCredential 读取密钥。不存在时返回 ("", nil)。
func (s *CredentialService) GetCredential(key string) (string, error) {
	if err := s.validateKey(key); err != nil {
		return "", err
	}
	if s.store == nil {
		return "", fmt.Errorf("密钥存储不可用")
	}
	return s.store.Load(key)
}

// DeleteCredential 删除密钥。不存在时幂等成功。
func (s *CredentialService) DeleteCredential(key string) error {
	if err := s.validateKey(key); err != nil {
		return err
	}
	if s.store == nil {
		return fmt.Errorf("密钥存储不可用")
	}
	return s.store.Delete(key)
}

func (s *CredentialService) validateKey(key string) error {
	if !credentialKeyPattern.MatchString(key) {
		return fmt.Errorf("不支持的密钥名 %q", key)
	}
	return nil
}
