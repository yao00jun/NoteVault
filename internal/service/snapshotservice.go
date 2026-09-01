package service

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/notevault/notevault/internal/core"
	"github.com/notevault/notevault/internal/infra/schema"
)

// 版本快照 / 时间机器（P1-2）。
//
// 设计取舍：
//   - **不引 go-git。** 只需要「按时间点取回某篇笔记的旧内容」，一整个 git 实现是
//     数量级的过度设计，且会破坏「工作区就是一堆纯 .md」的心智模型。
//   - **内容寻址存储。** 快照对象按内容 sha256 落在 .notevault/history/objects/<ab>/<rest>，
//     同一内容反复保存只占一份磁盘。gzip 压缩，Markdown 通常能压到 1/3。
//   - **快照的是「覆盖前的旧内容」，不是新内容。** 目的是「误改能退回上一版」；
//     当前最新内容就在工作区文件里，没必要再存一份。
//   - **派生数据全在 .notevault/。** 删掉整个 history 目录只丢历史，笔记本体不受影响。

const (
	// snapshotSaveInterval 同一文件两次 save 快照的最小间隔。
	// 编辑器自动保存很频繁，不节流会在一小时内堆出上百份几乎相同的快照。
	// 5 分钟对齐 Obsidian File Recovery 的默认值。
	snapshotSaveInterval = 5 * time.Minute

	// snapshotMaxPerFile 单文件保留的 save 快照上限（daily/manual/restore/delete 不计入）
	snapshotMaxPerFile = 50

	// snapshotKeepDays 非 manual 快照的保留天数
	snapshotKeepDays = 90

	// snapshotMaxFileSize 超过该大小的文件不进历史，避免把大附件塞进 .notevault
	snapshotMaxFileSize = 2 << 20 // 2MB
)

// 快照成因。前端据此区分「自动存点」与「用户主动打的标记」，
// 也决定保留策略：manual 永不自动清理。
const (
	SnapshotReasonSave    = "save"    // 覆盖保存前自动留存
	SnapshotReasonDaily   = "daily"   // 当日首次留存（每天至少留一份）
	SnapshotReasonManual  = "manual"  // 用户主动打点
	SnapshotReasonRestore = "restore" // 恢复操作前的安全备份
	SnapshotReasonDelete  = "delete"  // 删除前留存
)

// Snapshot 表示一份历史版本的元数据（内容存在 objects 里，按 Hash 寻址）
type Snapshot struct {
	ID        string `json:"id"`
	Path      string `json:"path"` // 相对工作区路径，统一用 / 分隔
	Hash      string `json:"hash"` // 内容 sha256（内容寻址键）
	Size      int64  `json:"size"` // 原始内容字节数（非压缩后）
	CreatedAt string `json:"createdAt"`
	Reason    string `json:"reason"`
}

// SnapshotFileSummary 按文件聚合的快照概览，用于时间机器面板左侧文件列表
type SnapshotFileSummary struct {
	Path     string `json:"path"`
	Count    int    `json:"count"`
	LatestAt string `json:"latestAt"`
	Bytes    int64  `json:"bytes"`
}

// SnapshotDiff 是某个快照与目标版本（另一快照或当前文件）的差异
type SnapshotDiff struct {
	Path      string   `json:"path"`
	FromID    string   `json:"fromId"`
	ToID      string   `json:"toId"` // 空串表示「当前工作区文件」
	FromAt    string   `json:"fromAt"`
	ToAt      string   `json:"toAt"`
	Ops       []DiffOp `json:"ops"`
	Added     int      `json:"added"`
	Removed   int      `json:"removed"`
	Truncated bool     `json:"truncated"`
	Identical bool     `json:"identical"`
}

// SnapshotRestoreResult 恢复结果。BackupID 是恢复前自动打的安全备份，
// 让「恢复错了」也能再退回去——AI/批量改写场景下这一步是刚需。
type SnapshotRestoreResult struct {
	Path       string `json:"path"`
	RestoredID string `json:"restoredId"`
	BackupID   string `json:"backupId"`
	Bytes      int64  `json:"bytes"`
}

// SnapshotStats 历史库占用概览
type SnapshotStats struct {
	Snapshots int   `json:"snapshots"`
	Files     int   `json:"files"`
	Objects   int   `json:"objects"`
	DiskBytes int64 `json:"diskBytes"`
}

// snapshotIndex 是 .notevault/history/index.json 的载荷（信封的 data 字段）。
//
// 版本登记在 schema.SnapshotIndex：
//   - v1：顶层内联 {"schemaVersion":1,"snapshots":[...]}
//   - v2：迁到统一版本信封（E-7），schemaVersion/kind/updatedAt 由信封承载
type snapshotIndex struct {
	Snapshots []*Snapshot `json:"snapshots"`
}

// snapshotIndexV1 是 v1 的磁盘结构，仅用于升级时读出旧数据。
//
// 这里必须显式兼容而不能像搜索摘要那样丢弃重建：历史版本一旦丢了索引，
// objects/ 里的内容还在但再也定位不到，等于用户的历史版本全部人间蒸发。
type snapshotIndexV1 struct {
	SchemaVersion int         `json:"schemaVersion"`
	Snapshots     []*Snapshot `json:"snapshots"`
}

// SnapshotService 提供版本快照的留存、查看、比对与恢复
type SnapshotService struct {
	// mu 串行化索引的读改写。快照写入由保存动作触发，多个文件并发保存时
	// 不加锁会丢条目（读到旧索引 → 各自追加 → 后写者覆盖前写者）。
	mu sync.Mutex
}

// NewSnapshotService 创建快照服务实例
func NewSnapshotService() *SnapshotService {
	return &SnapshotService{}
}

// ---------------------------------------------------------------------------
// 路径
// ---------------------------------------------------------------------------

func snapshotHistoryDir(workspacePath string) string {
	return filepath.Join(workspacePath, ".notevault", "history")
}

func snapshotObjectsDir(workspacePath string) string {
	return filepath.Join(snapshotHistoryDir(workspacePath), "objects")
}

func snapshotIndexPath(workspacePath string) string {
	return filepath.Join(snapshotHistoryDir(workspacePath), "index.json")
}

// snapshotObjectPath 内容寻址：前两位做子目录，避免单目录堆几万个文件
func snapshotObjectPath(workspacePath, hash string) string {
	if len(hash) < 3 {
		return filepath.Join(snapshotObjectsDir(workspacePath), hash)
	}
	return filepath.Join(snapshotObjectsDir(workspacePath), hash[:2], hash[2:])
}

// normalizeSnapshotPath 统一成 / 分隔的相对路径，保证索引键跨平台稳定
// （同一份 .notevault 在 Windows 与 macOS 打开时不能出现两套键）
func normalizeSnapshotPath(relativePath string) string {
	return filepath.ToSlash(filepath.Clean(relativePath))
}

// ---------------------------------------------------------------------------
// 索引读写
// ---------------------------------------------------------------------------

// loadIndex 读取索引。JSON 损坏或版本超前时返回空索引而非报错：
// 历史是派生数据，丢了只是少一份保险，绝不能因此让保存动作失败。
//
// v1 旧索引（内联 schemaVersion）会被读出并在下次写入时自动升级到信封格式，
// 不走「丢弃重建」——索引丢了 objects/ 里的内容就再也定位不到了。
func (s *SnapshotService) loadIndex(workspacePath string) *snapshotIndex {
	empty := &snapshotIndex{Snapshots: []*Snapshot{}}

	data, err := os.ReadFile(snapshotIndexPath(workspacePath))
	if err != nil {
		return empty
	}

	idx, res, err := schema.UnmarshalAs[snapshotIndex](data, schema.SnapshotIndex)
	if err != nil {
		return empty
	}
	switch res.Compat {
	case schema.CompatExact:
		// 正常路径
	case schema.CompatOlder, schema.CompatLegacy:
		// v1：{"schemaVersion":1,"snapshots":[...]}，snapshots 在顶层而非 data 里
		var v1 snapshotIndexV1
		if err := json.Unmarshal(data, &v1); err != nil {
			return empty
		}
		log.Printf("[snapshot] 索引格式 v%d → v%d，已读出 %d 条历史，下次写入时自动升级",
			res.FileVersion, schema.SnapshotIndex.Version, len(v1.Snapshots))
		idx = snapshotIndex{Snapshots: v1.Snapshots}
	case schema.CompatNewer:
		// 更高版本的 NoteVault 写过它：结构可能已变，按当前结构解析可能读出错误的
		// 路径 / 哈希，进而在清理时删错对象。宁可当作空索引（历史暂时不可见），
		// 也不要在错误的数据上执行删除。
		log.Printf("[snapshot] 索引 schemaVersion=%d 高于当前支持的 %d，暂不加载历史",
			res.FileVersion, schema.SnapshotIndex.Version)
		return empty
	}

	if idx.Snapshots == nil {
		idx.Snapshots = []*Snapshot{}
	}
	return &idx
}

func (s *SnapshotService) saveIndex(workspacePath string, idx *snapshotIndex) error {
	if idx.Snapshots == nil {
		idx.Snapshots = []*Snapshot{}
	}
	data, err := schema.MarshalAs(schema.SnapshotIndex, *idx)
	if err != nil {
		return core.WrapError(core.ErrInternal, "序列化快照索引失败", err)
	}
	// 原子写：崩溃时不留半截 JSON，否则重启后整条历史链都读不出来
	if err := atomicWrite(snapshotIndexPath(workspacePath), data, 0644); err != nil {
		return core.OsToNVError(err, "写入快照索引失败")
	}
	return nil
}

// ---------------------------------------------------------------------------
// 对象读写
// ---------------------------------------------------------------------------

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// writeObject 写入内容对象（gzip 压缩）。已存在则跳过——内容寻址天然去重。
func (s *SnapshotService) writeObject(workspacePath, hash, content string) error {
	objPath := snapshotObjectPath(workspacePath, hash)
	if _, err := os.Stat(objPath); err == nil {
		return nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		return core.WrapError(core.ErrInternal, "压缩快照内容失败", err)
	}
	if err := zw.Close(); err != nil {
		return core.WrapError(core.ErrInternal, "压缩快照内容失败", err)
	}
	if err := atomicWrite(objPath, buf.Bytes(), 0644); err != nil {
		return core.OsToNVError(err, "写入快照对象失败")
	}
	return nil
}

// readObject 读取内容对象。
// 兼容未压缩存储：gzip 头解析失败时按原始字节返回，避免历史格式变更导致旧快照全废。
func (s *SnapshotService) readObject(workspacePath, hash string) (string, error) {
	raw, err := os.ReadFile(snapshotObjectPath(workspacePath, hash))
	if err != nil {
		if os.IsNotExist(err) {
			return "", core.WrapError(core.ErrNotFound, "快照内容已丢失: "+hash, err)
		}
		return "", core.OsToNVError(err, "读取快照对象失败")
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return string(raw), nil
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", core.WrapError(core.ErrInternal, "解压快照内容失败", err)
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// 留存
// ---------------------------------------------------------------------------

// captureSnapshot 留存一份内容快照。
//
// reason 为空时按 SnapshotReasonSave 处理，此时会应用两条节流规则：
//  1. 内容与该文件最新快照相同 → 直接返回已有快照，不新增条目
//  2. 距最新快照不足 snapshotSaveInterval 且当日已有快照 → 跳过，返回 (nil, nil)
//
// 其余 reason（manual/restore/delete/daily）一律强制留存，只做内容去重。
// 返回 nil 快照且 err 为 nil 表示「按策略跳过」，不是错误。
func (s *SnapshotService) captureSnapshot(workspacePath, relativePath, content, reason string) (*Snapshot, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	// 即使不往该路径写文件，也要校验，防止越界路径被写进索引成为后续恢复的攻击面
	if _, err := confineToWorkspace(workspacePath, relativePath); err != nil {
		return nil, err
	}
	if int64(len(content)) > snapshotMaxFileSize {
		return nil, core.NewError(core.ErrInvalidInput,
			fmt.Sprintf("内容超过快照上限 %d 字节，未留存历史", int64(snapshotMaxFileSize)))
	}
	if reason == "" {
		reason = SnapshotReasonSave
	}

	relPath := normalizeSnapshotPath(relativePath)
	hash := hashContent(content)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.loadIndex(workspacePath)
	latest := latestForPath(idx.Snapshots, relPath)

	if latest != nil && latest.Hash == hash {
		return latest, nil // 内容没变，没有留存价值
	}

	if reason == SnapshotReasonSave && latest != nil {
		if !hasSnapshotOn(idx.Snapshots, relPath, now) {
			// 当日首份：无论多频繁都留，保证「每天至少能退回一版」
			reason = SnapshotReasonDaily
		} else if at, err := time.Parse(time.RFC3339, latest.CreatedAt); err == nil &&
			now.Sub(at) < snapshotSaveInterval {
			return nil, nil
		}
	}

	if err := s.writeObject(workspacePath, hash, content); err != nil {
		return nil, err
	}

	snap := &Snapshot{
		ID:        newSnapshotID(idx.Snapshots, now, hash),
		Path:      relPath,
		Hash:      hash,
		Size:      int64(len(content)),
		CreatedAt: now.Format(time.RFC3339),
		Reason:    reason,
	}
	idx.Snapshots = append(idx.Snapshots, snap)

	kept, dropped := applySnapshotRetention(idx.Snapshots, now)
	idx.Snapshots = kept
	if err := s.saveIndex(workspacePath, idx); err != nil {
		return nil, err
	}
	if len(dropped) > 0 {
		s.gcObjects(workspacePath, kept)
	}
	return snap, nil
}

// captureBeforeWrite 在覆盖写入前留存磁盘上的现有内容。
// 文件不存在（新建）时返回 (nil, nil)——没有旧版本可存，不是错误。
func (s *SnapshotService) captureBeforeWrite(workspacePath, relativePath string) (*Snapshot, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, core.OsToNVError(err, "读取待快照文件失败: "+relativePath)
	}
	if int64(len(data)) > snapshotMaxFileSize {
		return nil, nil // 大文件静默跳过，不阻断保存
	}
	return s.captureSnapshot(workspacePath, relativePath, string(data), SnapshotReasonSave)
}

// captureBeforeDelete 在删除前留存内容，让「直接删除」也有后路
func (s *SnapshotService) captureBeforeDelete(workspacePath, relativePath string) (*Snapshot, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, core.OsToNVError(err, "读取待快照文件失败: "+relativePath)
	}
	if int64(len(data)) > snapshotMaxFileSize {
		return nil, nil
	}
	return s.captureSnapshot(workspacePath, relativePath, string(data), SnapshotReasonDelete)
}

// CreateManualSnapshot 用户主动为当前文件打一个版本点（永不自动清理）
func (s *SnapshotService) CreateManualSnapshot(workspacePath, relativePath string) (*Snapshot, error) {
	fullPath, err := confineToWorkspace(workspacePath, relativePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.WrapError(core.ErrNotFound, "文件不存在: "+relativePath, err)
		}
		return nil, core.OsToNVError(err, "读取文件失败: "+relativePath)
	}
	return s.captureSnapshot(workspacePath, relativePath, string(data), SnapshotReasonManual)
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// ListSnapshots 列出快照，按时间倒序。relativePath 为空则返回全工作区。
func (s *SnapshotService) ListSnapshots(workspacePath, relativePath string) ([]*Snapshot, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	s.mu.Lock()
	idx := s.loadIndex(workspacePath)
	s.mu.Unlock()

	filter := ""
	if strings.TrimSpace(relativePath) != "" {
		filter = normalizeSnapshotPath(relativePath)
	}

	out := make([]*Snapshot, 0, len(idx.Snapshots))
	for _, snap := range idx.Snapshots {
		if filter == "" || snap.Path == filter {
			out = append(out, snap)
		}
	}
	sortSnapshotsDesc(out)
	return out, nil
}

// ListSnapshotFiles 按文件聚合快照概览，时间机器面板用它渲染左侧列表
func (s *SnapshotService) ListSnapshotFiles(workspacePath string) ([]*SnapshotFileSummary, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	s.mu.Lock()
	idx := s.loadIndex(workspacePath)
	s.mu.Unlock()

	grouped := map[string]*SnapshotFileSummary{}
	for _, snap := range idx.Snapshots {
		sum, ok := grouped[snap.Path]
		if !ok {
			sum = &SnapshotFileSummary{Path: snap.Path}
			grouped[snap.Path] = sum
		}
		sum.Count++
		sum.Bytes += snap.Size
		if snap.CreatedAt > sum.LatestAt {
			sum.LatestAt = snap.CreatedAt
		}
	}

	out := make([]*SnapshotFileSummary, 0, len(grouped))
	for _, sum := range grouped {
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LatestAt != out[j].LatestAt {
			return out[i].LatestAt > out[j].LatestAt
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// GetSnapshotContent 取出某个快照的完整内容
func (s *SnapshotService) GetSnapshotContent(workspacePath, id string) (string, error) {
	snap, err := s.findSnapshot(workspacePath, id)
	if err != nil {
		return "", err
	}
	return s.readObject(workspacePath, snap.Hash)
}

// DiffWithCurrent 比对快照与当前工作区文件（时间机器最常用的视图）
func (s *SnapshotService) DiffWithCurrent(workspacePath, id string) (*SnapshotDiff, error) {
	snap, err := s.findSnapshot(workspacePath, id)
	if err != nil {
		return nil, err
	}
	oldContent, err := s.readObject(workspacePath, snap.Hash)
	if err != nil {
		return nil, err
	}

	fullPath, err := confineToWorkspace(workspacePath, snap.Path)
	if err != nil {
		return nil, err
	}
	newContent := ""
	toAt := ""
	if data, err := os.ReadFile(fullPath); err == nil {
		newContent = string(data)
		if info, statErr := os.Stat(fullPath); statErr == nil {
			toAt = info.ModTime().Format(time.RFC3339)
		}
	}
	// 文件已被删除时 newContent 为空 —— diff 会展示为「全部删除」，正是用户想看到的

	return buildSnapshotDiff(snap.Path, snap.ID, "", snap.CreatedAt, toAt, oldContent, newContent), nil
}

// DiffSnapshots 比对同一文件的两个快照
func (s *SnapshotService) DiffSnapshots(workspacePath, fromID, toID string) (*SnapshotDiff, error) {
	from, err := s.findSnapshot(workspacePath, fromID)
	if err != nil {
		return nil, err
	}
	to, err := s.findSnapshot(workspacePath, toID)
	if err != nil {
		return nil, err
	}
	if from.Path != to.Path {
		return nil, core.NewError(core.ErrInvalidInput, "不能比对不同文件的快照")
	}
	oldContent, err := s.readObject(workspacePath, from.Hash)
	if err != nil {
		return nil, err
	}
	newContent, err := s.readObject(workspacePath, to.Hash)
	if err != nil {
		return nil, err
	}
	return buildSnapshotDiff(from.Path, from.ID, to.ID, from.CreatedAt, to.CreatedAt, oldContent, newContent), nil
}

// GetSnapshotStats 统计历史库占用（objects 目录实际磁盘占用，非原始内容之和）
func (s *SnapshotService) GetSnapshotStats(workspacePath string) (*SnapshotStats, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	s.mu.Lock()
	idx := s.loadIndex(workspacePath)
	s.mu.Unlock()

	paths := map[string]struct{}{}
	for _, snap := range idx.Snapshots {
		paths[snap.Path] = struct{}{}
	}

	stats := &SnapshotStats{Snapshots: len(idx.Snapshots), Files: len(paths)}
	objectsDir := snapshotObjectsDir(workspacePath)
	_ = filepath.WalkDir(objectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // 目录不存在等同于「零占用」，不是错误
		}
		if info, statErr := d.Info(); statErr == nil {
			stats.Objects++
			stats.DiskBytes += info.Size()
		}
		return nil
	})
	return stats, nil
}

// ---------------------------------------------------------------------------
// 恢复与清理
// ---------------------------------------------------------------------------

// RestoreSnapshot 把某个快照的内容写回工作区文件。
// 写回前先把当前内容存成 restore 快照，保证「恢复错了还能再退回来」。
func (s *SnapshotService) RestoreSnapshot(workspacePath, id string) (*SnapshotRestoreResult, error) {
	snap, err := s.findSnapshot(workspacePath, id)
	if err != nil {
		return nil, err
	}
	content, err := s.readObject(workspacePath, snap.Hash)
	if err != nil {
		return nil, err
	}
	fullPath, err := confineToWorkspace(workspacePath, snap.Path)
	if err != nil {
		return nil, err
	}

	backupID := ""
	if current, readErr := os.ReadFile(fullPath); readErr == nil {
		backup, capErr := s.captureSnapshot(workspacePath, snap.Path, string(current), SnapshotReasonRestore)
		if capErr != nil {
			// 存不下安全备份就不要动用户的文件——宁可恢复失败，也不能造成不可逆覆盖
			return nil, capErr
		}
		if backup != nil {
			backupID = backup.ID
		}
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0750); err != nil {
		return nil, core.OsToNVError(err, "创建父目录失败")
	}
	if err := atomicWrite(fullPath, []byte(content), 0644); err != nil {
		return nil, core.OsToNVError(err, "恢复文件失败: "+snap.Path)
	}

	return &SnapshotRestoreResult{
		Path:       snap.Path,
		RestoredID: snap.ID,
		BackupID:   backupID,
		Bytes:      int64(len(content)),
	}, nil
}

// DeleteSnapshot 删除单个快照条目，并回收不再被引用的对象
func (s *SnapshotService) DeleteSnapshot(workspacePath, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.loadIndex(workspacePath)
	kept := make([]*Snapshot, 0, len(idx.Snapshots))
	found := false
	for _, snap := range idx.Snapshots {
		if snap.ID == id {
			found = true
			continue
		}
		kept = append(kept, snap)
	}
	if !found {
		return core.NewError(core.ErrNotFound, "快照不存在: "+id)
	}
	idx.Snapshots = kept
	if err := s.saveIndex(workspacePath, idx); err != nil {
		return err
	}
	s.gcObjects(workspacePath, kept)
	return nil
}

// ClearSnapshots 清空某文件（relativePath 为空则全工作区）的全部历史，返回删除条目数
func (s *SnapshotService) ClearSnapshots(workspacePath, relativePath string) (int, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return 0, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.loadIndex(workspacePath)
	filter := ""
	if strings.TrimSpace(relativePath) != "" {
		filter = normalizeSnapshotPath(relativePath)
	}

	kept := make([]*Snapshot, 0, len(idx.Snapshots))
	removed := 0
	for _, snap := range idx.Snapshots {
		if filter == "" || snap.Path == filter {
			removed++
			continue
		}
		kept = append(kept, snap)
	}
	if removed == 0 {
		return 0, nil
	}
	idx.Snapshots = kept
	if err := s.saveIndex(workspacePath, idx); err != nil {
		return 0, err
	}
	s.gcObjects(workspacePath, kept)
	return removed, nil
}

// PruneSnapshots 手动触发保留策略 + 对象回收，返回清理后的占用统计
func (s *SnapshotService) PruneSnapshots(workspacePath string) (*SnapshotStats, error) {
	if strings.TrimSpace(workspacePath) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "工作区路径为空")
	}
	s.mu.Lock()
	idx := s.loadIndex(workspacePath)
	kept, dropped := applySnapshotRetention(idx.Snapshots, time.Now())
	if len(dropped) > 0 {
		idx.Snapshots = kept
		if err := s.saveIndex(workspacePath, idx); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		s.gcObjects(workspacePath, kept)
	}
	s.mu.Unlock()

	return s.GetSnapshotStats(workspacePath)
}

// gcObjects 删除不再被任何快照引用的内容对象。调用方须持有 s.mu。
func (s *SnapshotService) gcObjects(workspacePath string, kept []*Snapshot) {
	referenced := make(map[string]struct{}, len(kept))
	for _, snap := range kept {
		referenced[snap.Hash] = struct{}{}
	}
	objectsDir := snapshotObjectsDir(workspacePath)
	_ = filepath.WalkDir(objectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // 单个条目读不到就跳过，GC 是最佳努力
		}
		// 对象路径形如 objects/<ab>/<rest>，还原出完整 hash
		parent := filepath.Base(filepath.Dir(path))
		hash := parent + d.Name()
		if _, ok := referenced[hash]; ok {
			return nil
		}
		_ = os.Remove(path)
		return nil
	})
}

// findSnapshot 按 ID 查找快照元数据
func (s *SnapshotService) findSnapshot(workspacePath, id string) (*Snapshot, error) {
	if strings.TrimSpace(id) == "" {
		return nil, core.NewError(core.ErrInvalidInput, "快照 ID 为空")
	}
	s.mu.Lock()
	idx := s.loadIndex(workspacePath)
	s.mu.Unlock()

	for _, snap := range idx.Snapshots {
		if snap.ID == id {
			return snap, nil
		}
	}
	return nil, core.NewError(core.ErrNotFound, "快照不存在: "+id)
}

// ---------------------------------------------------------------------------
// 纯函数辅助
// ---------------------------------------------------------------------------

// newSnapshotID 生成可读且有序的 ID：时间戳 + 内容哈希前缀。
// 同一毫秒内同一内容会被去重逻辑拦掉，剩余极端碰撞用后缀兜底。
func newSnapshotID(existing []*Snapshot, now time.Time, hash string) string {
	prefix := hash
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	base := now.Format("20060102T150405.000") + "-" + prefix
	taken := map[string]struct{}{}
	for _, snap := range existing {
		taken[snap.ID] = struct{}{}
	}
	id := base
	for i := 2; ; i++ {
		if _, ok := taken[id]; !ok {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

// latestForPath 返回某文件最新的快照
func latestForPath(snaps []*Snapshot, path string) *Snapshot {
	var latest *Snapshot
	for _, snap := range snaps {
		if snap.Path != path {
			continue
		}
		if latest == nil || snap.CreatedAt > latest.CreatedAt {
			latest = snap
		}
	}
	return latest
}

// hasSnapshotOn 判断某文件在 ref 所在自然日是否已有快照
func hasSnapshotOn(snaps []*Snapshot, path string, ref time.Time) bool {
	day := ref.Format("2006-01-02")
	for _, snap := range snaps {
		if snap.Path != path {
			continue
		}
		if strings.HasPrefix(snap.CreatedAt, day) {
			return true
		}
	}
	return false
}

func sortSnapshotsDesc(snaps []*Snapshot) {
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].CreatedAt != snaps[j].CreatedAt {
			return snaps[i].CreatedAt > snaps[j].CreatedAt
		}
		return snaps[i].ID > snaps[j].ID
	})
}

// applySnapshotRetention 应用保留策略，返回（保留, 淘汰）。
//
// 规则（按文件独立计算）：
//  1. 每个文件最新的一份永远保留 —— 保留策略不能把「唯一的后路」也清掉
//  2. manual 永不淘汰（用户主动打的点，语义是「这版重要」）
//  3. 超过 snapshotKeepDays 天的淘汰
//  4. 剩余 save 快照按时间倒序保留 snapshotMaxPerFile 份
func applySnapshotRetention(snaps []*Snapshot, now time.Time) (kept, dropped []*Snapshot) {
	byPath := map[string][]*Snapshot{}
	for _, snap := range snaps {
		byPath[snap.Path] = append(byPath[snap.Path], snap)
	}

	cutoff := now.AddDate(0, 0, -snapshotKeepDays)
	keepSet := map[string]struct{}{}

	for _, group := range byPath {
		sortSnapshotsDesc(group)
		saveCount := 0
		for i, snap := range group {
			if i == 0 || snap.Reason == SnapshotReasonManual {
				keepSet[snap.ID] = struct{}{}
				continue
			}
			if at, err := time.Parse(time.RFC3339, snap.CreatedAt); err == nil && at.Before(cutoff) {
				continue
			}
			if snap.Reason == SnapshotReasonSave {
				saveCount++
				if saveCount > snapshotMaxPerFile {
					continue
				}
			}
			keepSet[snap.ID] = struct{}{}
		}
	}

	kept = make([]*Snapshot, 0, len(snaps))
	for _, snap := range snaps {
		if _, ok := keepSet[snap.ID]; ok {
			kept = append(kept, snap)
		} else {
			dropped = append(dropped, snap)
		}
	}
	return kept, dropped
}

// buildSnapshotDiff 组装 diff 结果
func buildSnapshotDiff(path, fromID, toID, fromAt, toAt, oldContent, newContent string) *SnapshotDiff {
	res := diffText(oldContent, newContent)
	return &SnapshotDiff{
		Path:      path,
		FromID:    fromID,
		ToID:      toID,
		FromAt:    fromAt,
		ToAt:      toAt,
		Ops:       res.Ops,
		Added:     res.Added,
		Removed:   res.Removed,
		Truncated: res.Truncated,
		Identical: res.Added == 0 && res.Removed == 0,
	}
}
