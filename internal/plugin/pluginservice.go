package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/fsutil"
	"github.com/notevault/notevault/internal/infra/schema"
	"github.com/notevault/notevault/internal/service"
)

// core.PluginManifest / core.PluginInfo 已上移至 core（见 core/models.go），本包通过 core.PluginInfo 引用。
// 编译时断言：PluginService 实现 service.PluginOperator 端口接口。
var _ service.PluginOperator = (*PluginService)(nil)

// PluginService 插件管理服务：扫描插件目录、读 manifest、启用/禁用
// 注：本轮只做"发现 + 管理"，不真正执行插件代码（执行需要 JS runtime，留 P9+）
type PluginService struct {
	pluginsDir string
	stateFile  string // 启用状态持久化文件（JSON：{pluginId: true}）
	trustFile  string // 信任授权持久化文件（JSON：{pluginId: {hash, grantedAt}}）
}

// trustRecord 一次 full-trust 授权记录
type trustRecord struct {
	// Hash 授权时刻的插件源码哈希。
	// 插件更新后哈希变化 → 授权自动失效，必须重新确认。
	// 这道绑定是为了防「先用无害版本骗取授权、再通过更新投递恶意代码」。
	Hash string `json:"hash"`
	// GrantedAt 授权时间，用于界面展示与审计
	GrantedAt time.Time `json:"grantedAt"`
}

// NewPluginService 创建插件服务
// pluginsDir 是插件目录；为空时回退到 %APPDATA%/NoteVault/plugins
func NewPluginService(pluginsDir string) *PluginService {
	if pluginsDir == "" {
		base := os.Getenv("APPDATA")
		if base == "" {
			base = os.TempDir()
		}
		pluginsDir = filepath.Join(base, "NoteVault", "plugins")
	}
	dir := filepath.Dir(pluginsDir)
	service := &PluginService{
		pluginsDir: pluginsDir,
		stateFile:  filepath.Join(dir, "plugins-state.json"),
		trustFile:  filepath.Join(dir, "plugins-trust.json"),
	}
	// 首次启动时落地预装插件（编辑工具栏）。
	// 工具栏插件化之后宿主不再内置按钮，不预装的话新用户会看到一个空工具栏。
	service.installBundledPlugins()
	return service
}

// ListPlugins 扫描插件目录，列出所有已发现的插件。
//
// 历史签名是 ListPlugins(rescan bool)，但 rescan 是死参数——实现没有任何缓存，
// 传 true/false 行为完全一致，只是在伪装"可跳过扫描"的语义。已移除该参数，
// 让契约如实反映"每次调用都读盘"这一事实。
func (s *PluginService) ListPlugins() ([]core.PluginInfo, error) {
	if err := os.MkdirAll(s.pluginsDir, 0750); err != nil {
		return nil, core.WrapError(core.ErrPermission, "无法创建插件目录: "+s.pluginsDir, err)
	}
	entries, err := os.ReadDir(s.pluginsDir)
	if err != nil {
		return nil, core.OsToNVError(err, "读取插件目录失败")
	}
	enabled := s.loadEnabledState()
	trust := s.loadTrustState()
	plugins := make([]core.PluginInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isPluginFile(name) {
			continue
		}
		full := filepath.Join(s.pluginsDir, name)
		info, perr := s.loadPlugin(full)
		if perr != nil {
			plugins = append(plugins, core.PluginInfo{
				Manifest:  core.PluginManifest{ID: strings.TrimSuffix(name, filepath.Ext(name)), Name: name},
				FilePath:  full,
				HasError:  true,
				LoadError: perr.Error(),
			})
			continue
		}
		info.Enabled = enabled[info.Manifest.ID]
		// 授权与源码哈希绑定：插件一旦更新，哈希对不上，授权自动失效。
		// 这样即使插件作者先发布无害版本骗取授权，后续更新也不会继承信任。
		if rec, ok := trust[info.Manifest.ID]; ok && rec.Hash == info.Hash {
			info.TrustGranted = info.Manifest.Trust == core.TrustFull
		}
		plugins = append(plugins, *info)
	}
	// 按插件名排序保证稳定输出
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Manifest.Name < plugins[j].Manifest.Name
	})
	return plugins, nil
}

// GetPlugin 取单个插件的完整信息（含源码）
func (s *PluginService) GetPlugin(id string) (*core.PluginInfo, error) {
	if id == "" {
		return nil, core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	plugins, err := s.ListPlugins()
	if err != nil {
		return nil, err
	}
	for i := range plugins {
		if plugins[i].Manifest.ID == id {
			return &plugins[i], nil
		}
	}
	return nil, core.NewError(core.ErrNotFound, "插件不存在: "+id)
}

// EnablePlugin 启用插件
func (s *PluginService) EnablePlugin(id string) error {
	if id == "" {
		return core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	if _, err := s.GetPlugin(id); err != nil {
		return err
	}
	enabled := s.loadEnabledState()
	enabled[id] = true
	return s.saveEnabledState(enabled)
}

// DisablePlugin 禁用插件
func (s *PluginService) DisablePlugin(id string) error {
	if id == "" {
		return core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	enabled := s.loadEnabledState()
	enabled[id] = false
	return s.saveEnabledState(enabled)
}

// GetPluginsDir 返回插件目录绝对路径
func (s *PluginService) GetPluginsDir() string {
	return s.pluginsDir
}

// pluginIDPattern 插件 ID 允许的字符。
// 插件 ID 会被拼进数据文件路径（plugins-data/<id>.json），
// 而 ID 来自前端调用，若不校验就能用 "../../xxx" 穿越到任意位置读写文件。
var pluginIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validatePluginID 校验插件 ID：非空、且不含路径分隔符等危险字符
func validatePluginID(id string) error {
	if id == "" {
		return core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	if !pluginIDPattern.MatchString(id) {
		return core.NewError(core.ErrInvalidInput, "插件 ID 含非法字符（只允许字母数字、下划线、连字符）: "+id)
	}
	return nil
}

// stripTSReferenceLines 跳过文件开头的 /// <reference path="..." /> 行。
// 这些是 TypeScript 的 IDE 提示，对运行时的 JS 插件毫无意义，
// 但放在 /*--- frontmatter 前面会阻断解析——见 parsePluginManifest 注释。
func stripTSReferenceLines(src string) string {
	for {
		nl := strings.IndexByte(src, '\n')
		var line string
		if nl < 0 {
			line = src
		} else {
			line = src[:nl]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "///") {
			if nl < 0 {
				return ""
			}
			src = src[nl+1:]
			continue
		}
		return src
	}
}

// pluginDataPath 插件私有数据文件路径（位于插件目录之外，避免被当作插件源码扫描）
func (s *PluginService) pluginDataPath(id string) string {
	return filepath.Join(filepath.Dir(s.pluginsDir), "plugins-data", id+".json")
}

// LoadPluginData 读取插件私有数据。
// 数据不存在时返回空串（不是错误）——插件首次运行时本来就没有数据。
func (s *PluginService) LoadPluginData(id string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	data, err := os.ReadFile(s.pluginDataPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", core.OsToNVError(err, "读取插件数据失败")
	}
	return string(data), nil
}

// SavePluginData 保存插件私有数据（调用方负责序列化，本服务只做透传存储）
func (s *PluginService) SavePluginData(id string, data string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	path := s.pluginDataPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return core.WrapError(core.ErrPermission, "创建插件数据目录失败", err)
	}
	return os.WriteFile(path, []byte(data), 0640)
}

// loadPlugin 从单个 .js 文件加载插件信息
// 文件格式约定：
//
//	首行或多行包含 front matter（`/*---\n * id: xxx\n * name: yyy\n * version: 1.0.0\n---*/`）
//	其余为 JS 源码
func (s *PluginService) loadPlugin(full string) (*core.PluginInfo, error) {
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, core.OsToNVError(err, "读取插件文件失败: "+full)
	}
	stat, err := os.Stat(full)
	if err != nil {
		return nil, core.OsToNVError(err, "stat 插件失败")
	}
	manifest, srcStart, parseErr := parsePluginManifest(string(data), filepath.Base(full))
	info := &core.PluginInfo{
		Manifest: manifest,
		FilePath: full,
		Size:     stat.Size(),
		Hash:     hashShort(data),
		ModTime:  stat.ModTime(),
	}
	if parseErr != nil {
		info.HasError = true
		info.LoadError = parseErr.Error()
	}
	info.Source = string(data[srcStart:])
	return info, nil
}

// parsePluginManifest 从源码中解析 front matter（`/*--- ... ---*/` 或 `//--- ... ---` 风格）
// 返回 manifest 与源码起始偏移量（front matter 后）
func parsePluginManifest(src string, filename string) (core.PluginManifest, int, error) {
	manifest := core.PluginManifest{}
	// 默认 ID 用文件名（去后缀）
	manifest.ID = strings.TrimSuffix(filename, filepath.Ext(filename))
	manifest.Name = manifest.ID
	manifest.Version = "0.0.0"
	manifest.Trust = core.TrustSandbox // 未声明即最低权限

	// 注意：早期版本曾在这里 stripTSReferenceLines 跳过 /// <reference ... /> 行。
	// 那个改动看起来无害（/// 是 JS 合法注释），但它让 `info.Source = string(data[srcStart:])`
	// **这行 caller 出 bug**：srcStart 是 stripped 中的位置，data 是原始文件，
	// 砍掉的 /// 行让 source 偏移错位，最终 `---*/` 里的 `*/` 成了 orphan 块注释结束符，
	// 浏览器 new Function 时抛 "Invalid or unexpected token"。
	//
	// 现在的策略是不 strip：file 头有 /// 的话直接走"manifest 解析失败"路径，
	// 由 installBundledPlugins 升级为预装版（embed 内容已删 ///）。
	// 写插件时应该用 `/// <reference ... />` 之外的标识（参见 plugins-samples/）。

	// 兼容两种 front matter：
	//   1) /*---\n * id: x\n * name: y\n---*/
	//   2) // @plugin id=x name=y version=1.0.0
	start := 0
	if strings.HasPrefix(src, "/*---") {
		// front matter 结束标记 "---*/" 长 5 字符
		end := strings.Index(src, "---*/")
		if end < 0 {
			return manifest, 0, fmt.Errorf("front matter 起始标记 `/*---` 缺少结束 `---*/`")
		}
		// fmBody 跳过起始 `/*---`（5 字符），到 `---*/` 起始前
		fmBody := src[5:end]
		applyFrontMatter(&manifest, fmBody)
		if err := finalizePermissions(&manifest); err != nil {
			return manifest, 0, err
		}
		if err := finalizeTrust(&manifest); err != nil {
			return manifest, 0, err
		}
		start = end + 5 // 跳过 `---*/`（5 字符）
	} else if strings.HasPrefix(src, "// @plugin") {
		// 第一行的注释直接解析 key=value
		// 兼容单行文件（无 \n）：整个 src 都视作注释
		var first string
		if nl := strings.IndexByte(src, '\n'); nl >= 0 {
			first = src[:nl]
			start = nl + 1
		} else {
			first = src
			start = len(src)
		}
		applyKVComment(&manifest, first)
		if err := finalizePermissions(&manifest); err != nil {
			return manifest, 0, err
		}
		if err := finalizeTrust(&manifest); err != nil {
			return manifest, 0, err
		}
	} else {
		// 无 front matter：返回默认值 + 文件名作为 ID，并标记为非错误（允许"裸脚本"插件）
		return manifest, 0, nil
	}
	// 跳过 front matter 之后紧随的换行符（避免源码开头出现多余 \n）
	for start < len(src) && (src[start] == '\n' || src[start] == '\r') {
		start++
	}
	return manifest, start, nil
}

func applyFrontMatter(m *core.PluginManifest, body string) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(strings.ToLower(k))
		v = strings.TrimSpace(v)
		v = strings.Trim(v, "\"'`") // 剥离双引号/单引号/反引号
		applyManifestKV(m, k, v)
	}
}

// applyKVComment 解析 `// @plugin k1=v1 k2="v with space" k3=v3` 形式的注释头
// 支持双引号/单引号/反引号包裹的值含空格
func applyKVComment(m *core.PluginManifest, line string) {
	line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
	line = strings.TrimSpace(strings.TrimPrefix(line, "@plugin"))
	// 自定义扫描器：识别引号包裹的 token，避免空格被切断
	pairs := scanKV(line)
	for _, p := range pairs {
		k := strings.TrimSpace(strings.ToLower(p.key))
		v := strings.Trim(strings.TrimSpace(p.val), "\"'`") // 剥离双引号/单引号/反引号
		applyManifestKV(m, k, v)
	}
}

// kvPair 一个 key=value 键值对
type kvPair struct {
	key string
	val string
}

// scanKV 扫描 `k1=v1 k2="v with space"` 形式的字符串，返回键值对切片
// 引号包裹的值中的空格不被切分；值外侧的引号在扫描阶段保留（让调用方统一剥离）
func scanKV(s string) []kvPair {
	var out []kvPair
	i, n := 0, len(s)
	for i < n {
		// 跳过空白
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		// 读 key 直到 '=' 或空白
		keyStart := i
		for i < n && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		key := s[keyStart:i]
		// 跳过空白（key 后可能直接是 '=' 或空格）
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n || s[i] != '=' {
			// 无值的 key，跳过
			if key != "" {
				out = append(out, kvPair{key: key, val: ""})
			}
			continue
		}
		i++ // 跳过 '='
		// 跳过 = 后空白
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		// 读值：支持引号包裹
		var val string
		if i < n && (s[i] == '"' || s[i] == '\'' || s[i] == '`') {
			q := s[i]
			i++
			start := i
			for i < n && s[i] != q {
				i++
			}
			val = s[start:i] // 引号内的内容（不含引号本身）
			if i < n {
				i++ // 跳过闭合引号
			}
		} else {
			// 无引号：读到下一个空白
			start := i
			for i < n && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			val = s[start:i]
		}
		out = append(out, kvPair{key: key, val: val})
	}
	return out
}

func applyManifestKV(m *core.PluginManifest, k, v string) {
	switch k {
	case "id":
		if v != "" {
			m.ID = v
		}
	case "name":
		if v != "" {
			m.Name = v
		}
	case "version":
		if v != "" {
			m.Version = v
		}
	case "author":
		m.Author = v
	case "description":
		m.Description = v
	case "homepage":
		m.Homepage = v
	case "permissions":
		appendRawPermissions(m, v)
	case "trust":
		m.Trust = core.PluginTrust(strings.ToLower(v))
	}
}

// finalizeTrust 归一化并校验信任等级，非法值一律拒绝加载该插件。
// 宁可让插件加载失败并给出明确错误，也不能悄悄降级——
// 静默降级会让作者以为自己声明的 full 生效了。
func finalizeTrust(m *core.PluginManifest) error {
	switch m.Trust {
	case "", core.TrustSandbox:
		m.Trust = core.TrustSandbox
		return nil
	case core.TrustFull:
		return nil
	default:
		return fmt.Errorf("未知信任等级: %q（只能是 sandbox 或 full）", m.Trust)
	}
}

var allowedPluginPermissions = map[string]struct{}{
	"workspace.read":  {},
	"workspace.write": {},
	"commands":        {},
	"notifications":   {},
	"ui":              {},
	// editor.decorate：声明式编辑器增强（P14）。
	// 刻意与 ui 分开——「往编辑器里加高亮」和「注册工具栏按钮 / 改用户选区」
	// 是不同的能力等级，合并会让授权提示变得含糊而不敢点允许。
	"editor.decorate": {},
}

func appendRawPermissions(m *core.PluginManifest, raw string) {
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(strings.Trim(item, "\"'`"))
		if item == "" {
			continue
		}
		m.Permissions = append(m.Permissions, item)
	}
}

func finalizePermissions(m *core.PluginManifest) error {
	rawItems := m.Permissions
	m.Permissions = nil
	for _, item := range rawItems {
		item = strings.TrimSpace(item)
		if _, ok := allowedPluginPermissions[item]; !ok {
			m.Permissions = nil
			return fmt.Errorf("未知权限: %s", item)
		}
		if slices.Contains(m.Permissions, item) {
			m.Permissions = nil
			return fmt.Errorf("重复权限: %s", item)
		}
		m.Permissions = append(m.Permissions, item)
	}
	return nil
}

// loadEnabledState 读取启用状态 JSON
// 文件不存在 / 损坏视为全禁用（不报错）
//
// 走统一版本信封（E-7），旧的裸 map[string]bool 格式仍能读出（CompatLegacy）。
// 高版本文件（CompatNewer）也按当前结构尽力解析：最坏情况是某个插件的启用位读错，
// 用户重新点一下开关即可，比整份状态丢弃体验更好。
func (s *PluginService) loadEnabledState() map[string]bool {
	data, err := os.ReadFile(s.stateFile)
	if err != nil {
		return map[string]bool{}
	}
	out, _, err := schema.UnmarshalAs[map[string]bool](data, schema.PluginState)
	if err != nil || out == nil {
		return map[string]bool{}
	}
	return out
}

// saveEnabledState 持久化启用状态
func (s *PluginService) saveEnabledState(m map[string]bool) error {
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0750); err != nil {
		return core.WrapError(core.ErrPermission, "创建状态目录失败", err)
	}
	if m == nil {
		m = map[string]bool{}
	}
	data, err := schema.MarshalAs(schema.PluginState, m)
	if err != nil {
		return core.WrapError(core.ErrInternal, "序列化插件状态失败", err)
	}
	return fsutil.AtomicWrite(s.stateFile, data, 0640)
}

// loadTrustState 读取信任授权记录
// 文件不存在/损坏视为「无任何授权」（不报错）——安全默认值
//
// 与启用状态不同，这里对 CompatNewer 采取保守策略：高版本可能给授权加了
// 我们读不懂的附加约束（例如作用域、有效期），按当前结构解析等于无视这些约束、
// 可能误放行。宁可让用户重新确认一次。
func (s *PluginService) loadTrustState() map[string]trustRecord {
	data, err := os.ReadFile(s.trustFile)
	if err != nil {
		return map[string]trustRecord{}
	}
	out, res, err := schema.UnmarshalAs[map[string]trustRecord](data, schema.PluginTrust)
	if err != nil || out == nil {
		return map[string]trustRecord{}
	}
	if res.Compat == schema.CompatNewer {
		log.Printf("[plugin] 信任状态 schemaVersion=%d 高于当前支持的 %d，为安全起见按「无任何授权」处理",
			res.FileVersion, schema.PluginTrust.Version)
		return map[string]trustRecord{}
	}
	return out
}

// saveTrustState 持久化信任授权记录
func (s *PluginService) saveTrustState(m map[string]trustRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.trustFile), 0750); err != nil {
		return core.WrapError(core.ErrPermission, "创建信任状态目录失败", err)
	}
	if m == nil {
		m = map[string]trustRecord{}
	}
	data, err := schema.MarshalAs(schema.PluginTrust, m)
	if err != nil {
		return core.WrapError(core.ErrInternal, "序列化插件信任状态失败", err)
	}
	// 原子写：半截写入会让 loadTrustState 判为「无任何授权」，虽然是安全方向的
	// 默认值（不会误放行），但用户得重新确认一遍所有插件。
	// atomicWrite 已从 service 包提到 internal/infra/fsutil，两边共用同一份实现。
	return fsutil.AtomicWrite(s.trustFile, data, 0640)
}

// GrantTrust 授权某个插件以 full 信任等级运行。
//
// 前置条件：插件 manifest 必须声明 trust=full。授权与当前源码哈希绑定——
// 插件更新后哈希变化，授权自动失效，需要用户重新确认。
func (s *PluginService) GrantTrust(id string) error {
	if id == "" {
		return core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	info, err := s.GetPlugin(id)
	if err != nil {
		return err
	}
	if info.Manifest.Trust != core.TrustFull {
		return core.NewError(core.ErrInvalidInput,
			"该插件未声明 trust=full，无需授权: "+id)
	}

	state := s.loadTrustState()
	state[id] = trustRecord{Hash: info.Hash, GrantedAt: time.Now()}
	return s.saveTrustState(state)
}

// RevokeTrust 撤销某个插件的 full 信任授权，插件立即回落到沙箱等级运行。
func (s *PluginService) RevokeTrust(id string) error {
	if id == "" {
		return core.NewError(core.ErrInvalidInput, "插件 ID 不能为空")
	}
	state := s.loadTrustState()
	if _, ok := state[id]; !ok {
		return nil // 本就没有授权，视为幂等成功
	}
	delete(state, id)
	return s.saveTrustState(state)
}

// isPluginFile 判断是否为可识别的插件文件后缀
func isPluginFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".js" || ext == ".mjs"
}

// hashShort 计算内容 sha256 摘要前 16 字符（用于变更检测）
func hashShort(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:16]
}
