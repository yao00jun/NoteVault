package plugin

import (
	"reflect"
	"sort"
	"testing"

	"github.com/notevault/notevault/internal/service"
)

// TestPluginService_PortMethodSet 断言 PluginService 的导出方法集与
// service.PluginOperator 完全一致（双向）。
//
// 为什么放在 plugin 包：Reporter/PluginOperator 的实现方在外包，
// 其编译期断言（var _ service.PluginOperator = (*PluginService)(nil)）随实现
// 下沉到本包，以避免 service 反向依赖 plugin；方法集契约测试同理。
// 编译期断言只保证"实现包含接口方法"，抓不到"实现多出未声明的导出方法"
// ——后者会被 Wails 反射绑定为前端 API 而契约与前端 TS 均未同步。
func TestPluginService_PortMethodSet(t *testing.T) {
	port := reflect.TypeOf((*service.PluginOperator)(nil)).Elem()
	got := reflect.TypeOf(&PluginService{})

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
		t.Errorf("PluginService 缺少端口方法 %s", name)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("PluginService 存在端口未声明的导出方法 %v——它们会被绑定为前端 API；请同步端口定义或改为非导出", extra)
	}
}
