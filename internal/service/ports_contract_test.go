package service

import (
	"reflect"
	"sort"
	"testing"
)

// portType 返回接口类型 T 本身（通过 *T 的 Elem() 获取）。
func portType[T any]() reflect.Type {
	var p *T
	return reflect.TypeOf(p).Elem()
}

// assertMethodsMatch 断言 impl 的导出方法集与端口接口完全一致（双向）。
//
// 为什么要这个测试：ports.go 里的编译期断言（var _ FileOperator = (*FileService)(nil)）
// 只能证明"实现包含了接口方法"，无法发现——
//  1. 实现上新增了接口未声明的导出方法：它们会被 Wails 反射绑定成新的前端 API，
//     但 ports.go 契约与前端 TS 类型未同步（契约漂移）；
//  2. （防御性）实现与接口签名漂移导致的静默失配。
func assertMethodsMatch(t *testing.T, port reflect.Type, impl any) {
	t.Helper()
	got := reflect.TypeOf(impl)

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
		t.Errorf("%s 缺少端口方法 %s（编译期断言应已拦截——出现说明断言缺失或接口漂移）", got, name)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Errorf("%s 存在端口未声明的导出方法 %v——这些方法会被 Wails 绑定为前端 API。\n"+
			"请二选一：同步 ports.go 端口定义与前端 TS；或改为非导出（小写）方法。", got, extra)
	}
}

// TestPortMethodSets 验证全部业务 Service 的导出方法集与 ports.go 端口一致。
// 注意：Reporter（*ErrorMonitor，internal/infra/monitor）与 PluginOperator
// （*PluginService，internal/plugin）的契约测试分别放在各自包内，
// 以维持 service 不反向依赖它们的分层约束。
func TestPortMethodSets(t *testing.T) {
	cases := []struct {
		name string
		port reflect.Type
		impl any
	}{
		{"FileService ↔ FileOperator", portType[FileOperator](), &FileService{}},
		{"SearchService ↔ Searcher", portType[Searcher](), &SearchService{}},
		{"TagService ↔ Tagger", portType[Tagger](), &TagService{}},
		{"GraphService ↔ Grapher", portType[Grapher](), &GraphService{}},
		{"TodoService ↔ TodoOperator", portType[TodoOperator](), &TodoService{}},
		{"ReminderService ↔ ReminderOperator", portType[ReminderOperator](), &ReminderService{}},
		{"ArchiveService ↔ ArchiveOperator", portType[ArchiveOperator](), &ArchiveService{}},
		{"TrashService ↔ TrashOperator", portType[TrashOperator](), &TrashService{}},
		{"SnapshotService ↔ SnapshotOperator", portType[SnapshotOperator](), &SnapshotService{}},
		{"LLMConfigService ↔ LLMConfigurator", portType[LLMConfigurator](), &LLMConfigService{}},
		{"CredentialService ↔ CredentialKeeper", portType[CredentialKeeper](), &CredentialService{}},
		{"WorkspaceService ↔ WorkspaceOperator", portType[WorkspaceOperator](), &WorkspaceService{}},
		{"ExportService ↔ Exporter", portType[Exporter](), &ExportService{}},
		{"SummarizeService ↔ Summarizer", portType[Summarizer](), &SummarizeService{}},
		{"QnAService ↔ QnAProvider", portType[QnAProvider](), &QnAService{}},
		{"ImportService ↔ Importer", portType[Importer](), &ImportService{}},
		{"TaskService ↔ TaskOperator", portType[TaskOperator](), &TaskService{}},
		{"GitService ↔ Gitter", portType[Gitter](), &GitService{}},
		{"TemplateService ↔ Templater", portType[Templater](), &TemplateService{}},
		{"CompileService ↔ CompileOperator", portType[CompileOperator](), &CompileService{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertMethodsMatch(t, c.port, c.impl)
		})
	}
}
