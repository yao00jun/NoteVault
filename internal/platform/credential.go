package platform

// CredentialStore 是系统级密钥存储的抽象（P2-5）。
//
// 背景：LLM API Key 此前整份存在前端 localStorage——它是 WebView 的
// localStorage，任何拿到渲染进程执行权的代码（包括未来的恶意插件路径）
// 都能直接读走明文。2026 的基线是存系统凭据库：
//
//   - Windows → 凭据管理器（Credential Manager，advapi32 Cred*W 系列 API）；
//   - 其他平台 → 配置目录下的 0600 权限文件（降级方案：至少不再与 WebView
//     存储混用，也方便将来替换成 macOS Keychain / libsecret）。
//
// key 是应用内的逻辑名（如 "ai.apiKey"），由调用方保证命名空间；
// 平台实现负责拼出系统侧的存储标识（如 Windows 的 TargetName）。
type CredentialStore interface {
	// Save 写入或覆盖一个密钥。
	Save(key, value string) error
	// Load 读取密钥。不存在时返回 ("", nil)——调用方无需区分"空"与"没有"。
	Load(key string) (string, error)
	// Delete 删除密钥。不存在时静默成功（幂等）。
	Delete(key string) error
}

// credentialStorePrefix 是所有平台实现共用的存储标识前缀，
// 避免与用户机器上其他应用的凭据串台。
const credentialStorePrefix = "NoteVault"

// NewCredentialStore 返回当前平台的密钥存储实现。
// prefix 是应用侧的存储标识（拼进 Windows TargetName / 文件名），防串台。
func NewCredentialStore(prefix string) CredentialStore {
	return newCredentialStore(prefix)
}
