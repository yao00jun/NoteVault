// Package schema 给所有落盘的派生数据 / 索引 JSON 提供统一的版本信封。
//
// 背景（E-7）：项目里有 6 处 JSON 落盘各自为政——快照索引带 schemaVersion、
// 搜索摘要带 version、回收站 / 提醒 / 工作区 / 插件状态是裸数组或裸 map 完全没有版本。
// 一旦格式演进，没版本字段的文件只有两种结局：解析失败（数据丢失）或
// 静默按新结构解析出错值（更糟，且不报错）。
//
// 统一后的磁盘形态：
//
//	{
//	  "schemaVersion": 1,
//	  "kind": "trash-index",
//	  "updatedAt": "2026-08-31T02:00:00+08:00",
//	  "data": [ ... ]
//	}
//
// 设计取舍：
//   - **不做自动迁移**。本包只负责判定「读到的是什么版本」，升级 / 丢弃 / 尽力解析
//     由调用方决定——因为策略取决于数据性质：搜索摘要是纯缓存，版本不符直接丢；
//     回收站索引丢了会留下孤儿文件，必须尽力保留。
//   - **必须兼容旧的裸格式**。已装机的用户磁盘上就是裸数组 / 裸 map，
//     读不出来等于丢数据，因此 Unmarshal 会回退直接解析并标记 CompatLegacy。
//   - **kind 字段是防错不是防御**。用于捕捉「路径算错、把提醒文件当回收站读」这类
//     编码错误，而不是安全边界。
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrKindMismatch 表示文件里的 kind 与期望不符——通常意味着路径算错了，
// 而不是文件损坏，因此单独成错误类型便于调用方区分处理。
var ErrKindMismatch = errors.New("schema: kind mismatch")

// Envelope 是落盘 JSON 的统一外层结构。
//
// 泛型参数 T 是业务载荷（切片、map、结构体皆可）。
// 本包不注册给 Wails，泛型不会影响 bindings 生成。
type Envelope[T any] struct {
	SchemaVersion int       `json:"schemaVersion"`
	Kind          string    `json:"kind"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Data          T         `json:"data"`
}

// Compat 描述读到的文件版本与当前代码期望版本的关系。
type Compat int

const (
	// CompatExact 版本完全一致，可直接使用
	CompatExact Compat = iota
	// CompatLegacy 文件没有 schemaVersion 字段，是加信封之前的裸格式
	CompatLegacy
	// CompatOlder 文件版本低于当前期望版本
	CompatOlder
	// CompatNewer 文件版本高于当前期望版本——更高版本的 NoteVault 写过它
	CompatNewer
)

func (c Compat) String() string {
	switch c {
	case CompatExact:
		return "exact"
	case CompatLegacy:
		return "legacy"
	case CompatOlder:
		return "older"
	case CompatNewer:
		return "newer"
	default:
		return fmt.Sprintf("compat(%d)", int(c))
	}
}

// Result 是一次读取的元信息判定结果。
type Result struct {
	// Compat 版本关系
	Compat Compat
	// FileVersion 文件里读到的 schemaVersion；CompatLegacy 时为 0
	FileVersion int
	// Kind 文件里读到的 kind；CompatLegacy 时为空
	Kind string
	// UpdatedAt 文件里读到的写入时间；CompatLegacy 时为零值
	UpdatedAt time.Time
}

// NeedsRewrite 表示这份数据应当在下次写入时升级到当前版本。
// 裸格式和低版本都属于「能读但该升级」，调用方通常在读完后立刻回写一次。
func (r Result) NeedsRewrite() bool {
	return r.Compat == CompatLegacy || r.Compat == CompatOlder
}

// Usable 表示载荷可以按当前代码的结构安全使用。
// CompatNewer 不在此列：更高版本可能改了载荷结构，是否尽力使用由调用方按数据性质决定。
func (r Result) Usable() bool {
	return r.Compat == CompatExact || r.Compat == CompatLegacy || r.Compat == CompatOlder
}

// Marshal 把载荷包进信封并序列化。
//
// 缩进两空格：这些文件用户可能会手工查看 / 提交进 git，可读性优先于体积
// （搜索摘要那种 8MB 大文件也一样——它本来就靠 modtime 增量更新，不是每次全写）。
func Marshal[T any](kind string, version int, data T) ([]byte, error) {
	if kind == "" {
		return nil, errors.New("schema: kind must not be empty")
	}
	if version <= 0 {
		return nil, fmt.Errorf("schema: version must be positive, got %d", version)
	}
	return json.MarshalIndent(Envelope[T]{
		SchemaVersion: version,
		Kind:          kind,
		UpdatedAt:     time.Now(),
		Data:          data,
	}, "", "  ")
}

// probe 只解析信封的元字段。
// SchemaVersion 用指针是为了区分「字段不存在」（裸格式）和「显式写了 0」（损坏）。
type probe struct {
	SchemaVersion *int   `json:"schemaVersion"`
	Kind          string `json:"kind"`
}

// Unmarshal 解析信封，并判定版本兼容性。
//
// 三条分支：
//  1. 顶层是数组，或没有 schemaVersion 字段 → 旧的裸格式，直接解析成 T，标记 CompatLegacy
//  2. 有 schemaVersion 且 kind 不符 → 返回 ErrKindMismatch（路径算错，不是数据问题）
//  3. 正常信封 → 解析 data 字段，按版本号算出 Compat
//
// 返回 error 仅代表「这份字节流无法解析成 T」。版本不符不是错误，
// 由调用方读 Result 自行决策——见包注释里的设计取舍。
func Unmarshal[T any](raw []byte, kind string, want int) (T, Result, error) {
	var zero T

	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 {
		return zero, Result{}, errors.New("schema: empty payload")
	}

	// 顶层数组一定是裸格式：信封永远是对象
	if trimmed[0] == '[' {
		return unmarshalLegacy[T](raw)
	}

	var p probe
	if err := json.Unmarshal(raw, &p); err != nil {
		// 探测失败最常见的原因是裸 map 的 value 类型与探测结构冲突
		// （例如插件状态 map[string]bool 里恰好有个叫 schemaVersion 的键）。
		// 直接解析成 T 再试一次；失败才算真损坏。
		return unmarshalLegacy[T](raw)
	}
	if p.SchemaVersion == nil {
		return unmarshalLegacy[T](raw)
	}
	if p.Kind != "" && p.Kind != kind {
		return zero, Result{
			Compat:      CompatExact,
			FileVersion: *p.SchemaVersion,
			Kind:        p.Kind,
		}, fmt.Errorf("%w: 期望 %q，文件里是 %q", ErrKindMismatch, kind, p.Kind)
	}

	var env Envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, Result{}, fmt.Errorf("schema: 解析信封载荷失败: %w", err)
	}

	res := Result{
		FileVersion: env.SchemaVersion,
		Kind:        env.Kind,
		UpdatedAt:   env.UpdatedAt,
	}
	switch {
	case env.SchemaVersion == want:
		res.Compat = CompatExact
	case env.SchemaVersion < want:
		res.Compat = CompatOlder
	default:
		res.Compat = CompatNewer
	}
	return env.Data, res, nil
}

func unmarshalLegacy[T any](raw []byte) (T, Result, error) {
	var data T
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, Result{}, fmt.Errorf("schema: 既不是版本信封也不是可识别的旧格式: %w", err)
	}
	return data, Result{Compat: CompatLegacy}, nil
}
