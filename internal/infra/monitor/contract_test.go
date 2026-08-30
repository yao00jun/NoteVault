package monitor

import (
	"reflect"
	"sort"
	"testing"

	"github.com/notevault/notevault/internal/service"
)

// TestErrorMonitor_PortMethodSet 断言 ErrorMonitor 的导出方法集与
// service.Reporter 完全一致（双向）。
//
// 历史背景（本测试防回归的两类真实漂移）：
//  1. SetHTTPClient（测试专用）曾因导出而被 Wails 绑定为前端 API，
//     且参数是本包未导出的 httpClientDoer 接口，生成的 TS 签名无意义
//     → 已改为非导出 setHTTPClient；
//  2. UpdateConfig（前端真实调用）曾不在 Reporter 端口内 → 已纳入端口。
func TestErrorMonitor_PortMethodSet(t *testing.T) {
	port := reflect.TypeOf((*service.Reporter)(nil)).Elem()
	got := reflect.TypeOf(&ErrorMonitor{})

	want := make(map[string]bool, port.NumMethod())
	for i := 0; i < port.NumMethod(); i++ {
		want[port.Method(i).Name] = true
	}

	var extra []string
	for i := 0; i < got.NumMethod(); i++ {
		name := got.Method(i).Name
		if want[name] {
			delete(want, name)
			continue
		}
		extra = append(extra, name)
	}
	for name := range want {
		t.Errorf("ErrorMonitor 缺少端口方法 %s", name)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("ErrorMonitor 存在端口未声明的导出方法 %v——它们会被绑定为前端 API；请同步端口定义或改为非导出", extra)
	}
}
