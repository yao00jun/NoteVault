package service

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExportService 提供笔记导出能力
// 设计原则：导出逻辑零外部依赖（仅使用标准库），Markdown→HTML 的渲染由前端完成，
// Go 端只负责把内容写入用户选定的目标路径（或打包为 zip）。
type ExportService struct{}

// NewExportService 创建导出服务实例
func NewExportService() *ExportService {
	return &ExportService{}
}

// ExportWorkspaceMarkdown 将工作区中所有 Markdown 文件打包为 zip 写入 destPath
// destPath 为目标 zip 文件路径（若未以 .zip 结尾会自动补上）
func (s *ExportService) ExportWorkspaceMarkdown(workspacePath, destPath string) error {
	if !strings.HasSuffix(strings.ToLower(destPath), ".zip") {
		destPath += ".zip"
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建导出文件失败：%w", err)
	}

	zw := zip.NewWriter(out)

	count := 0
	err = filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" {
			return nil
		}
		rel, relErr := filepath.Rel(workspacePath, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		w, createErr := zw.Create(rel)
		if createErr != nil {
			return nil
		}
		if _, writeErr := w.Write(data); writeErr != nil {
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	if count == 0 {
		_ = zw.Close()
		_ = out.Close()
		return fmt.Errorf("工作区中没有可导出的 Markdown 文件")
	}
	// zip 尾部写入失败会产出损坏文件，必须显式检查
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return fmt.Errorf("打包 zip 失败：%w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("关闭导出文件失败：%w", err)
	}
	return nil
}

// ExportNoteMarkdown 将单个笔记（按相对路径）复制导出到 destPath
func (s *ExportService) ExportNoteMarkdown(workspacePath, relPath, destPath string) error {
	src := filepath.Join(workspacePath, filepath.FromSlash(relPath))
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取源文件失败：%w", err)
	}
	if !strings.HasSuffix(strings.ToLower(destPath), ".md") {
		destPath += ".md"
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("写入导出文件失败：%w", err)
	}
	return nil
}

// SaveText 将任意文本内容写入绝对路径（用于单文件 HTML 等富文本导出）
func (s *ExportService) SaveText(destPath, content string) error {
	if err := os.WriteFile(destPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入文件失败：%w", err)
	}
	return nil
}
