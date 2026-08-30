package app

import (
	"errors"
	"testing"
)

func TestAppService_GetVersion(t *testing.T) {
	s := &AppService{}
	v := s.GetVersion()
	if v == "" {
		t.Fatal("version should not be empty")
	}
}

func TestAppService_GetAppName(t *testing.T) {
	s := &AppService{}
	name := s.GetAppName()
	if name != "NoteVault" {
		t.Fatalf("expected 'NoteVault', got '%s'", name)
	}
}

// TestIsUserCancelled 覆盖对话框"用户取消"判定（按字符串匹配，平台相关且分支脆弱，
// 此前无任何测试覆盖）。
func TestIsUserCancelled(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"cancelled by user", errors.New("dialog cancelled by user"), true},
		{"canceled by user", errors.New("dialog canceled by user"), true},
		{"cancelled", errors.New("operation cancelled"), true},
		{"other error", errors.New("some other error"), false},
		{"empty message", errors.New(""), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isUserCancelled(c.err); got != c.want {
				t.Fatalf("isUserCancelled(%v)=%v want %v", c.err, got, c.want)
			}
		})
	}
}
