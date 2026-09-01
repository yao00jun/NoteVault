package service

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/notevault/notevault/internal/infra/schema"
)

// TrashedFile 表示回收站中的文件
type TrashedFile struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	Name         string `json:"name"`
	OriginalPath string `json:"originalPath"`
	DeletedAt    string `json:"deletedAt"`
	Size         int64  `json:"size"`
}

// TrashService 提供回收站管理功能
type TrashService struct{}

// NewTrashService 创建回收站服务实例
func NewTrashService() *TrashService {
	return &TrashService{}
}

// getTrashDir 获取回收站目录
func (s *TrashService) getTrashDir(workspacePath string) string {
	return filepath.Join(workspacePath, ".trash")
}

// getTrashIndexPath 获取回收站索引文件路径
func (s *TrashService) getTrashIndexPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".notevault", "trash.json")
}

// loadTrashIndex 加载回收站索引
func (s *TrashService) loadTrashIndex(workspacePath string) ([]*TrashedFile, error) {
	indexPath := s.getTrashIndexPath(workspacePath)

	dir := filepath.Dir(indexPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return []*TrashedFile{}, nil
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, err
	}

	// 统一版本信封（E-7）。回收站索引是用户数据：读不出来会留下孤儿文件
	// （物理文件躺在 .trash/ 里，界面上却看不到、也恢复不了），
	// 因此旧的裸数组格式必须继续能读，高版本文件也尽力解析而不是丢弃。
	files, res, err := schema.UnmarshalAs[[]*TrashedFile](data, schema.TrashIndex)
	if err != nil {
		return nil, err
	}
	if res.Compat == schema.CompatNewer {
		log.Printf("[trash] 索引 schemaVersion=%d 高于当前支持的 %d，按当前结构尽力解析",
			res.FileVersion, schema.TrashIndex.Version)
	}
	if files == nil {
		files = []*TrashedFile{}
	}

	return files, nil
}

// saveTrashIndex 保存回收站索引
func (s *TrashService) saveTrashIndex(workspacePath string, files []*TrashedFile) error {
	indexPath := s.getTrashIndexPath(workspacePath)

	dir := filepath.Dir(indexPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	if files == nil {
		files = []*TrashedFile{}
	}
	data, err := schema.MarshalAs(schema.TrashIndex, files)
	if err != nil {
		return err
	}

	// 原子写：崩溃时不留半截索引 JSON（否则重启后索引丢失、文件查不到）
	return atomicWrite(indexPath, data, 0644)
}

// MoveToTrash 移动文件到回收站
func (s *TrashService) MoveToTrash(workspacePath string, relativePath string) (*TrashedFile, error) {
	srcPath := filepath.Join(workspacePath, relativePath)
	trashDir := s.getTrashDir(workspacePath)

	// 确保回收站目录存在
	if err := os.MkdirAll(trashDir, 0750); err != nil {
		return nil, err
	}

	// 生成唯一 ID，避免重名冲突
	id := "trash_" + time.Now().Format("20060102150405")
	destPath := filepath.Join(trashDir, id+"_"+filepath.Base(relativePath))

	// 移动文件
	if err := os.Rename(srcPath, destPath); err != nil {
		return nil, err
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return nil, err
	}

	trashedFile := &TrashedFile{
		ID:           id,
		Path:         filepath.ToSlash(id + "_" + filepath.Base(relativePath)),
		Name:         filepath.Base(relativePath),
		OriginalPath: filepath.ToSlash(relativePath),
		DeletedAt:    time.Now().Format(time.RFC3339),
		Size:         info.Size(),
	}

	// 更新索引
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return nil, err
	}
	files = append(files, trashedFile)
	if err := s.saveTrashIndex(workspacePath, files); err != nil {
		return nil, err
	}

	return trashedFile, nil
}

// RestoreFromTrash 从回收站恢复文件
func (s *TrashService) RestoreFromTrash(workspacePath string, id string) error {
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return err
	}

	var trashed *TrashedFile
	for _, f := range files {
		if f.ID == id {
			trashed = f
			break
		}
	}
	if trashed == nil {
		return os.ErrNotExist
	}

	trashDir := s.getTrashDir(workspacePath)
	srcPath := filepath.Join(trashDir, trashed.Path)
	destPath := filepath.Join(workspacePath, trashed.OriginalPath)

	// 确保目标目录存在
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return err
	}

	// 移动文件
	if err := os.Rename(srcPath, destPath); err != nil {
		return err
	}

	// 从索引中移除
	for i, f := range files {
		if f.ID == id {
			files = append(files[:i], files[i+1:]...)
			break
		}
	}

	return s.saveTrashIndex(workspacePath, files)
}

// PermanentlyDelete 永久删除回收站中的文件
func (s *TrashService) PermanentlyDelete(workspacePath string, id string) error {
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return err
	}

	var trashed *TrashedFile
	for _, f := range files {
		if f.ID == id {
			trashed = f
			break
		}
	}
	if trashed == nil {
		return os.ErrNotExist
	}

	trashDir := s.getTrashDir(workspacePath)
	filePath := filepath.Join(trashDir, trashed.Path)

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		return err
	}

	// 从索引中移除
	for i, f := range files {
		if f.ID == id {
			files = append(files[:i], files[i+1:]...)
			break
		}
	}

	return s.saveTrashIndex(workspacePath, files)
}

// GetTrashedFiles 获取回收站中的所有文件
func (s *TrashService) GetTrashedFiles(workspacePath string) ([]*TrashedFile, error) {
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return nil, err
	}

	// 按删除时间降序排序
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].DeletedAt < files[j].DeletedAt {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	return files, nil
}

// EmptyTrash 清空回收站
func (s *TrashService) EmptyTrash(workspacePath string) error {
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return err
	}

	trashDir := s.getTrashDir(workspacePath)

	// 删除所有文件（删除失败则返回错误，索引保留以便重试）
	for _, f := range files {
		filePath := filepath.Join(trashDir, f.Path)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除回收站文件失败：%w", err)
		}
	}

	// 清空索引
	return s.saveTrashIndex(workspacePath, []*TrashedFile{})
}

// GetTrashStats 获取回收站统计
func (s *TrashService) GetTrashStats(workspacePath string) (map[string]int64, error) {
	files, err := s.loadTrashIndex(workspacePath)
	if err != nil {
		return nil, err
	}

	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	return map[string]int64{
		"count": int64(len(files)),
		"size":  totalSize,
	}, nil
}

// 确保字符串方法可用
var _ = strings.ToLower
