package service

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchivedFile 表示一个归档文件
type ArchivedFile struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	OriginalPath string `json:"originalPath"`
	ArchivedAt   string `json:"archivedAt"`
	Size         int64  `json:"size"`
}

// ArchiveService 提供归档管理功能
type ArchiveService struct{}

// NewArchiveService 创建归档服务实例
func NewArchiveService() *ArchiveService {
	return &ArchiveService{}
}

// getArchiveDir 获取归档目录
func (s *ArchiveService) getArchiveDir(workspacePath string) string {
	return filepath.Join(workspacePath, ".archive")
}

// ArchiveFile 归档文件（移动到 .archive 目录）
func (s *ArchiveService) ArchiveFile(workspacePath string, relativePath string) (*ArchivedFile, error) {
	srcPath := filepath.Join(workspacePath, relativePath)
	archiveDir := s.getArchiveDir(workspacePath)

	// 确保归档目录存在
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return nil, err
	}

	// 目标路径：保留原始目录结构
	destPath := filepath.Join(archiveDir, relativePath)
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return nil, err
	}

	// 移动文件
	if err := os.Rename(srcPath, destPath); err != nil {
		return nil, err
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return nil, err
	}

	return &ArchivedFile{
		Path:         filepath.ToSlash(relativePath),
		Name:         filepath.Base(relativePath),
		OriginalPath: filepath.ToSlash(relativePath),
		ArchivedAt:   time.Now().Format(time.RFC3339),
		Size:         info.Size(),
	}, nil
}

// UnarchiveFile 取消归档（移回原位置）
func (s *ArchiveService) UnarchiveFile(workspacePath string, relativePath string) error {
	archiveDir := s.getArchiveDir(workspacePath)
	srcPath := filepath.Join(archiveDir, relativePath)
	destPath := filepath.Join(workspacePath, relativePath)

	// 确保目标目录存在
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0750); err != nil {
		return err
	}

	// 移动文件
	return os.Rename(srcPath, destPath)
}

// GetArchivedFiles 获取所有归档文件
func (s *ArchiveService) GetArchivedFiles(workspacePath string) ([]*ArchivedFile, error) {
	archiveDir := s.getArchiveDir(workspacePath)

	// 如果归档目录不存在，返回空列表
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		return []*ArchivedFile{}, nil
	}

	files := make([]*ArchivedFile, 0)
	err := filepath.Walk(archiveDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}

		relPath, _ := filepath.Rel(archiveDir, path)
		relPath = filepath.ToSlash(relPath)

		files = append(files, &ArchivedFile{
			Path:         relPath,
			Name:         filepath.Base(relPath),
			OriginalPath: relPath,
			ArchivedAt:   info.ModTime().Format(time.RFC3339),
			Size:         info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}

// IsArchived 检查文件是否已归档
func (s *ArchiveService) IsArchived(workspacePath string, relativePath string) bool {
	archiveDir := s.getArchiveDir(workspacePath)
	archivedPath := filepath.Join(archiveDir, relativePath)
	_, err := os.Stat(archivedPath)
	return err == nil
}
