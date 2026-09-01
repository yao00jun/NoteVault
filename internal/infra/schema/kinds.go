package schema

// Descriptor 描述一类落盘文件的身份与当前版本。
//
// 集中登记而不是让各服务自己写字符串字面量，图的是三件事：
//  1. kind 拼错在编译期就能发现，而不是运行时读出一个 ErrKindMismatch
//  2. 有一张表能回答「这个版本的 NoteVault 会写出哪些格式」——升级排查时唯一的入口
//  3. 版本号与 kind 绑在一起，改版本时必然看到它的语义注释
type Descriptor struct {
	// Kind 写进 JSON 的 kind 字段，全局唯一
	Kind string
	// Version 当前代码写出的 schemaVersion
	Version int
	// Location 落盘位置（相对工作区，或配置目录），仅用于排查
	Location string
}

// 全部落盘文件的登记表。
//
// 版本号语义（改动前先读）：
//   - 加字段、加可选项：不用升版本，旧代码读到会忽略新字段，新代码读旧文件走 CompatOlder
//   - 改字段含义、删字段、改嵌套结构：必须升版本
//   - 派生缓存（搜索摘要）升版本 = 丢弃重建；用户数据（回收站 / 提醒 / 快照）升版本 = 必须能读旧的
var (
	// TrashIndex 回收站索引。用户数据：丢了会留下孤儿文件，必须兼容旧格式。
	// v1：从裸数组 []*TrashedFile 迁到版本信封
	TrashIndex = Descriptor{Kind: "trash-index", Version: 1, Location: ".notevault/trash.json"}

	// Reminders 提醒列表。用户数据。
	// v1：从裸数组 []*Reminder 迁到版本信封
	Reminders = Descriptor{Kind: "reminders", Version: 1, Location: ".notevault/reminders.json"}

	// WorkspaceList 工作区列表。用户数据：丢了等于所有知识库都要重新添加。
	// v1：从裸数组 []Workspace 迁到版本信封
	WorkspaceList = Descriptor{Kind: "workspace-list", Version: 1, Location: "<配置目录>/workspaces.json"}

	// CurrentWorkspace 当前打开的工作区 ID。丢了只是启动后回到未选择状态，影响很小，
	// 纳入统一信封纯粹为了「所有落盘文件都有版本」这条不变量没有例外。
	// v1：从裸 JSON 字符串（"ws_123"）迁到版本信封
	CurrentWorkspace = Descriptor{Kind: "current-workspace", Version: 1, Location: "<配置目录>/current_workspace.json"}

	// PluginState 插件启用状态。丢了退化为全部禁用，可接受。
	// v1：从裸 map[string]bool 迁到版本信封
	PluginState = Descriptor{Kind: "plugin-state", Version: 1, Location: "<插件目录同级>/plugins-state.json"}

	// PluginTrust 插件信任授权。安全相关：读不出来必须退化为「无任何授权」，
	// 绝不能因为兼容性而误放行。
	// v1：从裸 map[string]trustRecord 迁到版本信封
	PluginTrust = Descriptor{Kind: "plugin-trust", Version: 1, Location: "<插件目录同级>/plugins-trust.json"}

	// SearchSummary 搜索索引摘要。纯派生缓存：版本不符直接丢弃重建，
	// 代价只是一次全量扫描，远小于为兼容旧格式在数据层引入分支。
	// v1：仅 token 集合 / v2：token 词频 + 文档长度（BM25 需要）/ v3：迁到版本信封
	SearchSummary = Descriptor{Kind: "search-summary", Version: 3, Location: "<配置目录>/search-index/<sha1>.json"}

	// SnapshotIndex 版本快照索引。用户数据：丢了等于历史版本全部无法定位。
	// v1：顶层内联 schemaVersion + snapshots / v2：迁到统一版本信封
	SnapshotIndex = Descriptor{Kind: "snapshot-index", Version: 2, Location: ".notevault/history/index.json"}

	// BaseDefinition 结构化视图（Bases）定义。用户数据：等同用户手写的查询，
	// 读不出来就是视图凭空消失，必须兼容旧格式。
	// v1：初版（filters + views）
	BaseDefinition = Descriptor{Kind: "base-definition", Version: 1, Location: ".notevault/bases/<name>.nvbase"}
)

// All 返回全部登记项，供测试遍历校验。
func All() []Descriptor {
	return []Descriptor{
		TrashIndex,
		Reminders,
		WorkspaceList,
		CurrentWorkspace,
		PluginState,
		PluginTrust,
		SearchSummary,
		SnapshotIndex,
		BaseDefinition,
	}
}

// MarshalAs 按登记项序列化载荷。
func MarshalAs[T any](d Descriptor, data T) ([]byte, error) {
	return Marshal(d.Kind, d.Version, data)
}

// UnmarshalAs 按登记项解析载荷并判定兼容性。
func UnmarshalAs[T any](raw []byte, d Descriptor) (T, Result, error) {
	return Unmarshal[T](raw, d.Kind, d.Version)
}
