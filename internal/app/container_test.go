package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// infraFields 是容器持有但不注册给 Wails 的基础设施字段名。
//
// 与服务的区别：服务是「前端要调用的 API」，基础设施是「服务之间共享的对象」。
// 目前只有索引注册表（E-4）—— 它归 Workspace/Search/QnA 三方共用，本身不暴露 API。
// 新增同类字段时必须加到这里，否则下面两条测试会因为数量不匹配而误报。
var infraFields = map[string]bool{
	"Indexes": true,
}

func testConfig(t *testing.T) ContainerConfig {
	t.Helper()
	cfg := DefaultContainerConfig()
	cfg.ErrorLogDir = filepath.Join(t.TempDir(), "logs")
	cfg.PluginsDir = filepath.Join(t.TempDir(), "plugins")
	return cfg
}

// TestNewContainer_AllFieldsWired 是这个包最重要的一条测试：
// 新增服务时只要忘记在 NewContainer 里赋值，这里立刻红。
func TestNewContainer_AllFieldsWired(t *testing.T) {
	c := NewContainer(testConfig(t))
	if c == nil {
		t.Fatal("NewContainer returned nil")
	}

	v := reflect.ValueOf(*c)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() != reflect.Ptr {
			t.Fatalf("field %s: expected a pointer field, got %s", typ.Field(i).Name, field.Kind())
		}
		if field.IsNil() {
			t.Errorf("field %s is nil — NewContainer forgot to wire it", typ.Field(i).Name)
		}
	}
}

// TestWailsServices_CoversEveryField 防止「加了服务但没注册给 Wails」这种
// 只有跑起来才发现的问题：前端 bindings 会静默缺一个 Service。
func TestWailsServices_CoversEveryField(t *testing.T) {
	c := NewContainer(testConfig(t))
	typ := reflect.ValueOf(*c).Type()
	fields := 0
	for i := 0; i < typ.NumField(); i++ {
		if infraFields[typ.Field(i).Name] {
			continue // 基础设施对象不注册给 Wails
		}
		fields++
	}
	services := c.WailsServices()
	if len(services) != fields {
		t.Fatalf("WailsServices() returned %d entries but Container has %d fields; "+
			"每新增一个服务字段都要同步注册给 Wails，否则前端 bindings 会漏掉它", len(services), fields)
	}
}

// TestWailsServices_RegistersEveryContainerInstance 比数量检查更严：
// 每个字段的类型必须在注册列表里出现且只出现一次，且注册进去的必须是容器里那一个实例，
// 而不是顺手 New 出来的第二份（那会导致前端调到一个没接线的服务，且不会有任何报错）。
//
// 注意不能只按指针去重：ArchiveService / TrashService / GraphService 等是零尺寸结构体
// （`struct{}`），Go 对所有零尺寸分配返回同一个 runtime.zerobase 地址，
// 指针相等在它们身上不代表"同一个实例"，因此身份校验只对有状态的类型生效。
func TestWailsServices_RegistersEveryContainerInstance(t *testing.T) {
	c := NewContainer(testConfig(t))

	registered := map[reflect.Type]uintptr{}
	for _, svc := range c.WailsServices() {
		inst := reflect.ValueOf(svc.Instance())
		if inst.Kind() != reflect.Ptr {
			t.Fatalf("registered a non-pointer instance %s", inst.Type())
		}
		if _, dup := registered[inst.Type()]; dup {
			t.Fatalf("type %s is registered twice — Wails would generate duplicate bindings", inst.Type())
		}
		registered[inst.Type()] = inst.Pointer()
	}

	v := reflect.ValueOf(*c)
	typ := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := typ.Field(i).Name
		if infraFields[name] {
			continue
		}

		ptr, ok := registered[field.Type()]
		if !ok {
			t.Errorf("field %s (%s) is never registered to Wails — 前端 bindings 会静默缺这个服务",
				name, field.Type())
			continue
		}
		// 零尺寸结构体的地址全局共享，跳过身份比对
		if field.Type().Elem().Size() == 0 {
			continue
		}
		if ptr != field.Pointer() {
			t.Errorf("field %s is registered as a different instance than Container.%s", name, name)
		}
	}
}

// TestNewContainer_SnapshotIsWiredIntoFileWrites 锁住装配顺序里唯一有真实副作用的约束：
// Snapshot 必须挂在 File 的写入链路上，否则保存前不会留旧版本，而这不会有任何报错。
func TestNewContainer_SnapshotIsWiredIntoFileWrites(t *testing.T) {
	c := NewContainer(testConfig(t))

	ws := t.TempDir()
	rel := "note.md"
	abs := filepath.Join(ws, rel)
	if err := os.WriteFile(abs, []byte("v1\n"), 0600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := c.File.SaveFile(ws, rel, "v2\n"); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	snaps, err := c.Snapshot.ListSnapshots(ws, rel)
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected exactly 1 snapshot of the overwritten content, got %d", len(snaps))
	}

	content, err := c.Snapshot.GetSnapshotContent(ws, snaps[0].ID)
	if err != nil {
		t.Fatalf("GetSnapshotContent: %v", err)
	}
	if content != "v1\n" {
		t.Fatalf("snapshot should hold the pre-write content %q, got %q", "v1\n", content)
	}
}

// TestNewContainer_SearchUsesTheSameFileService 搜索必须复用同一个 FileService 实例，
// 否则将来 FileService 加缓存 / 加历史时，搜索会读到另一套状态。
func TestNewContainer_SearchUsesTheSameFileService(t *testing.T) {
	c := NewContainer(testConfig(t))

	// SearchService 的 fileService 字段未导出，用反射比对指针身份。
	sv := reflect.ValueOf(c.Search).Elem()
	field := sv.FieldByName("fileService")
	if !field.IsValid() {
		t.Skip("SearchService no longer holds a fileService field; update this test alongside that change")
	}
	if field.Pointer() != reflect.ValueOf(c.File).Pointer() {
		t.Fatal("SearchService holds a different FileService instance than Container.File")
	}
}

func TestDefaultContainerConfig_EnablesLocalLogOnly(t *testing.T) {
	cfg := DefaultContainerConfig()
	if !cfg.EnableLocalErrorLog {
		t.Error("local error log should be on by default — silent failures are worse than a log file")
	}
	if cfg.SentryDSN != "" {
		t.Error("default config must not carry a Sentry DSN")
	}
	if cfg.AppVersion == "" {
		t.Error("default config should carry the built-in app version")
	}
}
