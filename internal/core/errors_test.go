package core

import (
	"errors"
	"os"
	"testing"
)

func TestNVError_Error(t *testing.T) {
	e := NewError(ErrNotFound, "file missing")
	if e.Error() != "[NOT_FOUND] file missing" {
		t.Fatalf("unexpected error string: %q", e.Error())
	}

	wrapped := WrapError(ErrPermission, "denied", errors.New("root cause"))
	if wrapped.Error() != "[PERMISSION_DENIED] denied: root cause" {
		t.Fatalf("unexpected wrapped error string: %q", wrapped.Error())
	}
}

func TestNVError_Unwrap(t *testing.T) {
	cause := errors.New("root")
	wrapped := WrapError(ErrInternal, "ctx", cause)
	if !errors.Is(wrapped, cause) {
		t.Fatal("Unwrap should return cause")
	}
}

func TestIsCode(t *testing.T) {
	e := NewError(ErrNotFound, "x")
	if !IsCode(e, ErrNotFound) {
		t.Fatal("IsCode should match ErrNotFound")
	}
	if IsCode(e, ErrPermission) {
		t.Fatal("IsCode should not match ErrPermission")
	}
	if IsCode(errors.New("plain"), ErrNotFound) {
		t.Fatal("plain error should not match any code")
	}
}

func TestOsToNVError_NotFound(t *testing.T) {
	err := OsToNVError(os.ErrNotExist, "read config")
	if !IsCode(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOsToNVError_Permission(t *testing.T) {
	err := OsToNVError(os.ErrPermission, "write file")
	if !IsCode(err, ErrPermission) {
		t.Fatalf("expected ErrPermission, got %v", err)
	}
}

func TestOsToNVError_Nil(t *testing.T) {
	if OsToNVError(nil, "ctx") != nil {
		t.Fatal("nil error should return nil")
	}
}
