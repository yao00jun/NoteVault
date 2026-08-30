package core

import (
	"fmt"
	"os"
	"strings"
)

// ErrorCode 定义统一的错误码，前端可据此做差异化处理
type ErrorCode string

const (
	// ErrNotFound 文件/资源不存在
	ErrNotFound ErrorCode = "NOT_FOUND"
	// ErrPermission 权限不足
	ErrPermission ErrorCode = "PERMISSION_DENIED"
	// ErrInvalidInput 参数非法
	ErrInvalidInput ErrorCode = "INVALID_INPUT"
	// ErrAlreadyExists 资源已存在（如路径冲突）
	ErrAlreadyExists ErrorCode = "ALREADY_EXISTS"
	// ErrIsDirectory 期望文件但遇到目录
	ErrIsDirectory ErrorCode = "IS_DIRECTORY"
	// ErrInternal 内部错误（不可恢复）
	ErrInternal ErrorCode = "INTERNAL"
)

// NVError 是带错误码的 typed error，实现 error 接口
type NVError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *NVError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *NVError) Unwrap() error {
	return e.Cause
}

// NewError 创建一个新的 NVError
func NewError(code ErrorCode, message string) *NVError {
	return &NVError{Code: code, Message: message}
}

// WrapError 包装底层错误为 NVError
func WrapError(code ErrorCode, message string, cause error) *NVError {
	return &NVError{Code: code, Message: message, Cause: cause}
}

// IsCode 检查 error 是否具有指定的错误码
func IsCode(err error, code ErrorCode) bool {
	if nvErr, ok := err.(*NVError); ok {
		return nvErr.Code == code
	}
	return false
}

// OsToNVError 将 os 错误转换为 NVError（常用场景的快捷映射）
func OsToNVError(err error, context string) error {
	if err == nil {
		return nil
	}
	msg := context
	if msg == "" {
		msg = err.Error()
	}
	switch {
	case os.IsNotExist(err):
		return WrapError(ErrNotFound, msg, err)
	case os.IsPermission(err):
		return WrapError(ErrPermission, msg, err)
	case strings.Contains(err.Error(), "is a directory"):
		return WrapError(ErrIsDirectory, msg, err)
	default:
		return WrapError(ErrInternal, msg, err)
	}
}
