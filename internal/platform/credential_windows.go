//go:build windows

package platform

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows 凭据管理器实现（P2-5）。
//
// 直接走 advapi32 的 CredWriteW / CredReadW / CredDeleteW：
// golang.org/x/sys 已是依赖树里的包，而第三方 wincred 封装会引入新依赖，
// 这里总共也就一百来行，不值得为此加一个模块。
//
// 凭据形态选 CRED_TYPE_GENERIC：
//   - TargetName = "NoteVault:<key>"（前缀防串台，冒号是凭据管理器 UI 的惯例分隔）；
//   - Blob = UTF-16 编码的密钥明文（generic 凭据上限约 2.5KB，API Key 绰绰有余）；
//   - Persist = CRED_PERSIST_LOCAL_MACHINE（本机持久化，重启不丢）。

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

// CREDENTIALW 与 wincred.h 的 CREDENTIAL 结构一一对应，字段顺序必须严格一致
// （Flags / Type / TargetName / **Comment / LastWritten** / CredentialBlobSize /
// CredentialBlob / Persist / AttributeCount / Attributes / TargetAlias / UserName）。
// 指针字段一律 *uint16 / unsafe.Pointer，由 syscall.UTF16PtrFromString 与
// Go 内存负责；不要用 string，会触发 Go 指针进 C 内存的检查。
//
// CredentialBlob 刻意用 unsafe.Pointer 而不是 uintptr：
//   - 布局等价（两者都与 C 指针同宽）；
//   - Load 读回 C 分配的缓冲区时不需要 uintptr→unsafe.Pointer 转换，
//     `go vet` 的 unsafeptr 检查对这种转换会报警。
//
// CredentialBlobSize 与 CredentialBlob 之间的对齐缝隙由 Go 编译器自动填充
// （uint32 后接 8 字节对齐的指针），与 MSVC x64 布局一致。
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     unsafe.Pointer
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// windowsCredentialStore 把密钥存进 Windows 凭据管理器。
type windowsCredentialStore struct {
	prefix string
}

// newCredentialStore 返回当前平台的实现（见 credential.go 的平台分发表）。
func newCredentialStore(prefix string) CredentialStore {
	if prefix == "" {
		prefix = credentialStorePrefix
	}
	return windowsCredentialStore{prefix: prefix}
}

func (s windowsCredentialStore) target(key string) string {
	return s.prefix + ":" + key
}

// Save 写入或覆盖凭据。写入前 CredWriteW 会自动替换同 TargetName 的旧值。
func (s windowsCredentialStore) Save(key, value string) error {
	// UTF-16 编码含结尾 null；空串不该走到这里（上层 service 把空值翻译成 Delete），
	// 但仍防御 length=0 时的空指针。
	blob := utf16.Encode([]rune(value))
	var blobPtr unsafe.Pointer
	if len(blob) > 0 {
		blobPtr = unsafe.Pointer(&blob[0])
	}

	target, err := syscall.UTF16PtrFromString(s.target(key))
	if err != nil {
		return fmt.Errorf("凭据名不合法: %w", err)
	}
	user, err := syscall.UTF16PtrFromString(s.prefix)
	if err != nil {
		return fmt.Errorf("凭据用户名不合法: %w", err)
	}

	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob) * 2),
		CredentialBlob:     blobPtr,
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	r1, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	// blob 通过 cred.CredentialBlob 传给了系统调用，Call 期间必须保持存活
	runtime.KeepAlive(blob)
	if r1 == 0 {
		return fmt.Errorf("CredWrite 失败: %w", callErr)
	}
	return nil
}

// Load 读取凭据。系统里不存在时返回 ("", nil)。
func (s windowsCredentialStore) Load(key string) (string, error) {
	target, err := syscall.UTF16PtrFromString(s.target(key))
	if err != nil {
		return "", fmt.Errorf("凭据名不合法: %w", err)
	}

	var cred *credentialW
	r1, _, callErr := procCredRead.Call(
		uintptr(unsafe.Pointer(target)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&cred)),
	)
	if r1 == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return "", nil
		}
		return "", fmt.Errorf("CredRead 失败: %w", callErr)
	}
	// CredRead 成功后必须 CredFree 释放系统分配的缓冲区，否则泄漏。
	defer procCredFree.Call(uintptr(unsafe.Pointer(cred)))

	if cred.CredentialBlob == nil || cred.CredentialBlobSize == 0 {
		return "", nil
	}
	blob := unsafe.Slice((*uint16)(cred.CredentialBlob), cred.CredentialBlobSize/2)
	return string(utf16.Decode(blob)), nil
}

// Delete 删除凭据。目标不存在（ERROR_NOT_FOUND）时视为已删除。
func (s windowsCredentialStore) Delete(key string) error {
	target, err := syscall.UTF16PtrFromString(s.target(key))
	if err != nil {
		return fmt.Errorf("凭据名不合法: %w", err)
	}
	r1, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r1 == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("CredDelete 失败: %w", callErr)
	}
	return nil
}
