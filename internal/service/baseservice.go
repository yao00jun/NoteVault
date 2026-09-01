package service

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/schema"
)

// BaseService 提供结构化视图（Bases）能力：把工作区里的 Markdown
// 当成一张可查询的表，按 front matter 属性 / 标签 / 待办 / 文件元数据筛选、排序、分组。
//
// 设计要点：
//   - **不建独立数据库。** 属性从文件现读现算（带 30s TTL 缓存），
//     笔记永远是唯一事实来源；用外部编辑器改了 front matter 也不会数据不一致。
//   - **视图定义是普通文件。** `.notevault/bases/*.nvbase` 是可读 JSON，
//     可以进 git、可以手改、可以在设备间同步。
//   - **查询永不报错。** 属性名打错、正则写坏、类型对不上，一律降级 + 冒 warning，
//     绝不用一个报错弹窗打断用户的探索过程。
type BaseService struct {
	indexer *propertyIndexer
}

// NewBaseService 创建结构化视图服务实例。
func NewBaseService() *BaseService {
	return &BaseService{indexer: newPropertyIndexer()}
}

// baseFileExt 是视图定义的扩展名。
const baseFileExt = ".nvbase"

// basesDir 返回视图定义目录。
func basesDir(workspacePath string) string {
	return filepath.Join(workspacePath, ".notevault", "bases")
}

// ---------------------------------------------------------------------------
// 文件名安全
// ---------------------------------------------------------------------------

// sanitizeBaseName 把用户输入的视图名转成安全的文件名主干。
//
// 这里是**安全边界**：Name 完全来自前端输入，不做处理就直接拼进路径，
// `../../.ssh/authorized_keys` 这类输入能写到工作区外面去。
// 策略是白名单——只保留字母数字、CJK、空格、下划线、连字符，其余一律替换。
func sanitizeBaseName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			b.WriteRune(r)
		case r > 0x7F && r != '/' && r != '\\':
			// 非 ASCII 一律放行（中文视图名是主流用法），但排除路径分隔符
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), " -")
	// 连续的替换字符压成一个，避免 "a///b" 变成 "a---b"
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	// Windows 保留名：CON/PRN/AUX/NUL/COM1..9/LPT1..9 建不出文件
	switch strings.ToUpper(out) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = "_" + out
	}
	if len(out) > 80 {
		out = strings.TrimRight(out[:80], " -")
	}
	return out
}

// basePath 返回某个视图的定义文件路径，并校验它确实落在 bases 目录内。
func basePath(workspacePath, name string) (string, error) {
	safe := sanitizeBaseName(name)
	if safe == "" {
		return "", core.NewError(core.ErrInvalidInput, "视图名不能为空")
	}
	dir := basesDir(workspacePath)
	full := filepath.Join(dir, safe+baseFileExt)

	// 双重保险：即便 sanitize 有漏，这里也会拦住越界写入
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.Contains(rel, string(filepath.Separator)) {
		return "", core.NewError(core.ErrInvalidInput, "非法的视图名："+name)
	}
	return full, nil
}

// ---------------------------------------------------------------------------
// 视图定义读写
// ---------------------------------------------------------------------------

// BaseSummary 是视图列表项（不含完整筛选配置，列表页不需要）。
type BaseSummary struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Folder      string    `json:"folder,omitempty"`
	ViewCount   int       `json:"viewCount"`
	FilterCount int       `json:"filterCount"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ListBases 列出工作区里所有已保存的视图定义。
func (s *BaseService) ListBases(workspacePath string) ([]*BaseSummary, error) {
	if workspacePath == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径不能为空")
	}
	dir := basesDir(workspacePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// 还没建过视图不是错误
			return []*BaseSummary{}, nil
		}
		return nil, core.OsToNVError(err, "读取视图目录失败")
	}

	out := make([]*BaseSummary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), baseFileExt) {
			continue
		}
		full := filepath.Join(dir, e.Name())
		def, updatedAt, err := readBaseFile(full)
		if err != nil {
			// 单个视图坏了不该让整个列表打不开
			log.Printf("[bases] 跳过无法解析的视图定义 %s：%v", e.Name(), err)
			continue
		}
		out = append(out, &BaseSummary{
			Name:        def.Name,
			Description: def.Description,
			Folder:      def.Folder,
			ViewCount:   len(def.Views),
			FilterCount: countFilters(def.Filters),
			UpdatedAt:   updatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// countFilters 递归统计条件数量，用于列表页展示"3 个条件"。
func countFilters(g BaseFilterGroup) int {
	n := len(g.Conditions)
	for _, sub := range g.Groups {
		n += countFilters(sub)
	}
	return n
}

// readBaseFile 读取并解析一个 .nvbase 文件。
func readBaseFile(path string) (BaseDef, time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BaseDef{}, time.Time{}, err
	}
	def, res, err := schema.UnmarshalAs[BaseDef](data, schema.BaseDefinition)
	if err != nil {
		return BaseDef{}, time.Time{}, err
	}
	if res.Compat == schema.CompatNewer {
		// 更高版本写的视图：结构可能有我们不认识的字段。
		// 这里不像快照那样"安全侧返回空"——视图是只读查询，按已知字段跑
		// 最坏结果只是少几个筛选条件，比整个视图消失更好。
		log.Printf("[bases] 视图 %s 的 schemaVersion=%d 高于当前支持的 %d，按已知字段解析",
			filepath.Base(path), res.FileVersion, schema.BaseDefinition.Version)
	}
	if def.Name == "" {
		// 兼容手写文件漏了 name：用文件名兜底，否则列表里是一行空白
		def.Name = strings.TrimSuffix(filepath.Base(path), baseFileExt)
	}
	return def, res.UpdatedAt, nil
}

// LoadBase 读取一个视图的完整定义。
func (s *BaseService) LoadBase(workspacePath, name string) (*BaseDef, error) {
	full, err := basePath(workspacePath, name)
	if err != nil {
		return nil, err
	}
	def, _, err := readBaseFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.NewError(core.ErrNotFound, "视图不存在："+name)
		}
		return nil, core.WrapError(core.ErrInternal, "解析视图定义失败", err)
	}
	return &def, nil
}

// SaveBase 保存（新建或覆盖）一个视图定义。
func (s *BaseService) SaveBase(workspacePath string, def BaseDef) error {
	if workspacePath == "" {
		return core.NewError(core.ErrInvalidInput, "工作区路径不能为空")
	}
	full, err := basePath(workspacePath, def.Name)
	if err != nil {
		return err
	}
	// 至少要有一个视图，否则打开后是一片空白，用户以为坏了
	if len(def.Views) == 0 {
		def.Views = defaultViews()
	}
	for i := range def.Views {
		if def.Views[i].ID == "" {
			def.Views[i].ID = "view-" + formatInt(int64(i+1))
		}
		if def.Views[i].Type == "" {
			def.Views[i].Type = ViewTable
		}
		if def.Views[i].Name == "" {
			def.Views[i].Name = def.Views[i].Type
		}
	}
	if def.Filters.Conjunction == "" {
		def.Filters.Conjunction = ConjAnd
	}

	data, err := schema.MarshalAs(schema.BaseDefinition, def)
	if err != nil {
		return core.WrapError(core.ErrInternal, "序列化视图定义失败", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return core.OsToNVError(err, "创建视图目录失败")
	}
	// 原子写：视图定义是用户手工搭的查询，写坏了要重新配一遍
	if err := atomicWrite(full, data, 0644); err != nil {
		return core.OsToNVError(err, "写入视图定义失败")
	}
	return nil
}

// DeleteBase 删除一个视图定义。
func (s *BaseService) DeleteBase(workspacePath, name string) error {
	full, err := basePath(workspacePath, name)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			return core.NewError(core.ErrNotFound, "视图不存在："+name)
		}
		return core.OsToNVError(err, "删除视图定义失败")
	}
	return nil
}

// RenameBase 重命名视图（同时改 Name 字段与文件名）。
func (s *BaseService) RenameBase(workspacePath, oldName, newName string) error {
	def, err := s.LoadBase(workspacePath, oldName)
	if err != nil {
		return err
	}
	safeNew := sanitizeBaseName(newName)
	if safeNew == "" {
		return core.NewError(core.ErrInvalidInput, "新视图名不能为空")
	}
	if strings.EqualFold(sanitizeBaseName(oldName), safeNew) {
		// 只是改了大小写或不可见字符，直接改名字段
		def.Name = strings.TrimSpace(newName)
		return s.SaveBase(workspacePath, *def)
	}
	newPath, err := basePath(workspacePath, safeNew)
	if err != nil {
		return err
	}
	if _, err := os.Stat(newPath); err == nil {
		return core.NewError(core.ErrAlreadyExists, "已存在同名视图："+newName)
	}
	def.Name = strings.TrimSpace(newName)
	if err := s.SaveBase(workspacePath, *def); err != nil {
		return err
	}
	// 新文件写成功后才删旧的，中途失败最坏是留下一份重复，不会丢配置
	return s.DeleteBase(workspacePath, oldName)
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// InvalidateCache 清除属性索引缓存。
//
// 文件保存 / 删除 / 切工作区后由上层调用，否则用户改完 front matter
// 最多要等 30 秒才在视图里看到变化。
func (s *BaseService) InvalidateCache(workspacePath string) {
	s.indexer.invalidate(workspacePath)
}

// RunBase 在给定定义上执行查询（不要求已保存）。
//
// 前端编辑筛选条件时走这条路径做实时预览：改一个条件就重跑一次，
// 不需要先保存到磁盘。
func (s *BaseService) RunBase(workspacePath string, def BaseDef, viewID string) (*BaseResult, error) {
	if workspacePath == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径不能为空")
	}
	records, err := s.indexer.scan(workspacePath)
	if err != nil {
		return nil, core.OsToNVError(err, "扫描工作区失败")
	}

	view := pickView(def.Views, viewID)
	known := knownProperties(records)
	return runQuery(records, def, view, known), nil
}

// RunSavedBase 读取已保存的视图定义并执行。
func (s *BaseService) RunSavedBase(workspacePath, name, viewID string) (*BaseResult, error) {
	def, err := s.LoadBase(workspacePath, name)
	if err != nil {
		return nil, err
	}
	return s.RunBase(workspacePath, *def, viewID)
}

// pickView 选出要执行的视图：按 ID 命中，否则取第一个，都没有则给个默认表格。
func pickView(views []BaseView, viewID string) BaseView {
	if viewID != "" {
		for _, v := range views {
			if v.ID == viewID {
				return v
			}
		}
	}
	if len(views) > 0 {
		return views[0]
	}
	return defaultViews()[0]
}

// knownProperties 收集工作区里实际存在的属性名，用于"属性名打错"告警。
func knownProperties(records []*NoteRecord) map[string]bool {
	known := make(map[string]bool)
	for _, r := range records {
		for name := range r.Props {
			known[name] = true
		}
	}
	// 隐式属性无条件视为已知：空工作区里筛 file.name 不该被报"不存在"
	for _, n := range implicitProps {
		known[n] = true
	}
	return known
}

// ListProperties 返回工作区里所有可用属性及统计信息，供前端属性选择器使用。
func (s *BaseService) ListProperties(workspacePath string) ([]*PropertyMeta, error) {
	if workspacePath == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径不能为空")
	}
	records, err := s.indexer.scan(workspacePath)
	if err != nil {
		return nil, core.OsToNVError(err, "扫描工作区失败")
	}
	return collectProperties(records), nil
}

// ListOperators 返回全部可用运算符，供前端下拉。
//
// 从后端返回而不是前端硬编码：运算符集合是查询语义的一部分，
// 加一个运算符时只该改一处。
func (s *BaseService) ListOperators() []string {
	out := make([]string, len(allOperators))
	copy(out, allOperators)
	return out
}

// ListViewTypes 返回全部视图类型。
func (s *BaseService) ListViewTypes() []string {
	return []string{ViewTable, ViewBoard, ViewList}
}

// ---------------------------------------------------------------------------
// 默认值 / 内置模板
// ---------------------------------------------------------------------------

// defaultViews 返回一份默认视图配置（表格 + 看板 + 列表各一）。
func defaultViews() []BaseView {
	return []BaseView{
		{
			ID:      "table",
			Name:    "表格",
			Type:    ViewTable,
			Columns: []string{PropFileTitle, PropFileTags, PropFileFolder, PropFileMtime},
			Sort:    []BaseSort{{Property: PropFileMtime, Desc: true}},
		},
		{
			ID:      "board",
			Name:    "看板",
			Type:    ViewBoard,
			Columns: []string{PropFileTitle, PropFileTags},
			GroupBy: PropFileTags,
		},
		{
			ID:      "list",
			Name:    "列表",
			Type:    ViewList,
			Columns: []string{PropFileTitle, PropFileMtime},
			Sort:    []BaseSort{{Property: PropFileTitle, Desc: false}},
		},
	}
}

// NewBaseTemplate 返回一份可直接保存的空白视图定义。
//
// 前端"新建视图"点下去就能得到一个能跑的东西，而不是一张空表单——
// 空表单是查询工具最大的上手门槛。
func (s *BaseService) NewBaseTemplate(name string) *BaseDef {
	if strings.TrimSpace(name) == "" {
		name = "新建视图"
	}
	return &BaseDef{
		Name:    name,
		Filters: BaseFilterGroup{Conjunction: ConjAnd},
		Views:   defaultViews(),
	}
}

// BuiltinTemplate 描述一个内置模板。
type BuiltinTemplate struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Def         BaseDef `json:"def"`
}

// ListTemplates 返回内置模板，覆盖几个最常见的知识库用法。
//
// 模板不是装饰：Bases 这类功能的核心难点是"我不知道能用它干什么"，
// 给出几个立刻能跑的例子比写一页文档有效。
func (s *BaseService) ListTemplates() []*BuiltinTemplate {
	return []*BuiltinTemplate{
		{
			ID:          "reading",
			Title:       "在读书单",
			Description: "status 为 reading 的笔记，按评分从高到低",
			Def: BaseDef{
				Name: "在读书单",
				Filters: BaseFilterGroup{
					Conjunction: ConjAnd,
					Conditions: []BaseFilter{
						{Property: "status", Operator: OpEq, Value: "reading"},
					},
				},
				Views: []BaseView{{
					ID:      "table",
					Name:    "表格",
					Type:    ViewTable,
					Columns: []string{PropFileTitle, "rating", PropFileTags, PropFileMtime},
					Sort:    []BaseSort{{Property: "rating", Desc: true}},
				}},
			},
		},
		{
			ID:          "unfinished-todo",
			Title:       "有未完成待办",
			Description: "还有未打勾待办的笔记，按未完成数量降序",
			Def: BaseDef{
				Name: "有未完成待办",
				Filters: BaseFilterGroup{
					Conjunction: ConjAnd,
					Conditions: []BaseFilter{
						{Property: PropTodoPending, Operator: OpGt, Value: "0"},
					},
				},
				Views: []BaseView{{
					ID:      "table",
					Name:    "表格",
					Type:    ViewTable,
					Columns: []string{PropFileTitle, PropTodoPending, PropTodoTotal, PropFileMtime},
					Sort:    []BaseSort{{Property: PropTodoPending, Desc: true}},
				}},
			},
		},
		{
			ID:          "tag-board",
			Title:       "标签看板",
			Description: "所有笔记按标签分组成看板",
			Def: BaseDef{
				Name:    "标签看板",
				Filters: BaseFilterGroup{Conjunction: ConjAnd},
				Views: []BaseView{{
					ID:      "board",
					Name:    "看板",
					Type:    ViewBoard,
					Columns: []string{PropFileTitle, PropFileMtime},
					GroupBy: PropFileTags,
				}},
			},
		},
		{
			ID:          "no-tags",
			Title:       "缺少标签",
			Description: "没有任何标签的笔记，适合定期整理",
			Def: BaseDef{
				Name: "缺少标签",
				Filters: BaseFilterGroup{
					Conjunction: ConjAnd,
					Conditions: []BaseFilter{
						{Property: PropFileTags, Operator: OpEmpty},
					},
				},
				Views: []BaseView{{
					ID:      "list",
					Name:    "列表",
					Type:    ViewList,
					Columns: []string{PropFileTitle, PropFileFolder, PropFileMtime},
					Sort:    []BaseSort{{Property: PropFileMtime, Desc: true}},
				}},
			},
		},
	}
}
