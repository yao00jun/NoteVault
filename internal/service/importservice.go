package service

import (
	"archive/zip"
	"github.com/notevault/notevault/internal/core"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ImportResult 导入结果统计
type ImportResult struct {
	Imported int      `json:"imported"` // 成功导入文件数
	Skipped  int      `json:"skipped"`  // 跳过文件数（冲突/非 md）
	Renamed  int      `json:"renamed"`  // 冲突自动重命名数
	Errors   []string `json:"errors"`   // 失败明细（不中断整体导入）
}

// ConflictStrategy 冲突处理策略
type ConflictStrategy string

const (
	// ConflictSkip 跳过冲突文件（默认）
	ConflictSkip ConflictStrategy = "skip"
	// ConflictRename 自动重命名（文件名加序号）
	ConflictRename ConflictStrategy = "rename"
	// ConflictOverwrite 覆盖已有文件
	ConflictOverwrite ConflictStrategy = "overwrite"
)

// ImportOptions 导入选项
type ImportOptions struct {
	// ConflictStrategy 冲突策略：skip=跳过 / rename=自动重命名 / overwrite=覆盖（默认 skip）
	ConflictStrategy string `json:"conflictStrategy"`
	// IncludeSubdirs 是否保留源目录的子目录结构（默认 true）
	IncludeSubdirs bool `json:"includeSubdirs"`
}

// ImportService 负责从外部来源导入笔记数据
type ImportService struct{}

// NewImportService 创建导入服务
func NewImportService() *ImportService {
	return &ImportService{}
}

// ImportMarkdownFolder 将文件夹中的 Markdown 文件导入到工作区
// srcDir: 源文件夹绝对路径
// workspacePath: 目标工作区根目录绝对路径
// opts: 导入选项（空值使用默认）
func (s *ImportService) ImportMarkdownFolder(srcDir, workspacePath string, opts ImportOptions) (*ImportResult, error) {
	if srcDir == "" {
		return nil, core.NewError(core.ErrInvalidInput, "源文件夹不能为空")
	}
	if workspacePath == "" {
		return nil, core.NewError(core.ErrInvalidInput, "目标工作区不能为空")
	}
	info, err := os.Stat(srcDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.NewError(core.ErrNotFound, "源文件夹不存在")
		}
		return nil, core.OsToNVError(err, "无法访问源文件夹")
	}
	if !info.IsDir() {
		return nil, core.NewError(core.ErrIsDirectory, "源路径不是文件夹")
	}

	strategy := normalizeStrategy(opts.ConflictStrategy)
	includeSubdirs := opts.IncludeSubdirs

	result := &ImportResult{}
	visited := make(map[string]bool)

	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, "读取失败: "+path)
			return nil // 跳过无法读取的条目，继续遍历
		}
		if d.IsDir() {
			return nil
		}
		if !isMarkdownFile(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			result.Errors = append(result.Errors, "路径解析失败: "+path)
			return nil
		}
		targetRel := rel
		if !includeSubdirs {
			targetRel = filepath.Base(rel)
		}
		if err := s.importOneFile(path, workspacePath, targetRel, strategy, visited, result); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
		return nil
	})
	if walkErr != nil {
		return nil, core.OsToNVError(walkErr, "遍历源文件夹失败")
	}
	return result, nil
}

// ImportZip 将 zip 压缩包中的 Markdown 文件导入到工作区
// zipPath: zip 文件绝对路径
// workspacePath: 目标工作区根目录绝对路径
func (s *ImportService) ImportZip(zipPath, workspacePath string, opts ImportOptions) (*ImportResult, error) {
	if zipPath == "" {
		return nil, core.NewError(core.ErrInvalidInput, "zip 路径不能为空")
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, core.NewError(core.ErrNotFound, "zip 文件不存在")
		}
		return nil, core.WrapError(core.ErrInvalidInput, "无法打开 zip 文件", err)
	}
	defer zr.Close()

	strategy := normalizeStrategy(opts.ConflictStrategy)
	includeSubdirs := opts.IncludeSubdirs
	result := &ImportResult{}
	visited := make(map[string]bool)

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if !isMarkdownFile(f.Name) {
			continue
		}
		// 路径穿越防护：解压目标必须位于工作区内
		clean := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
			result.Errors = append(result.Errors, "跳过非法路径: "+f.Name)
			continue
		}
		targetRel := clean
		if !includeSubdirs {
			targetRel = filepath.Base(clean)
		}
		rc, err := f.Open()
		if err != nil {
			result.Errors = append(result.Errors, "读取 zip 条目失败: "+f.Name)
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			result.Errors = append(result.Errors, "读取 zip 条目失败: "+f.Name)
			continue
		}
		if err := s.writeToWorkspace(workspacePath, targetRel, content, strategy, visited, result); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
	}
	return result, nil
}

// importOneFile 导入单个源文件到工作区
func (s *ImportService) importOneFile(srcFull, workspacePath, targetRel string, strategy ConflictStrategy, visited map[string]bool, result *ImportResult) error {
	content, err := os.ReadFile(srcFull)
	if err != nil {
		return core.WrapError(core.ErrPermission, "读取文件失败: "+srcFull, err)
	}
	return s.writeToWorkspace(workspacePath, targetRel, content, strategy, visited, result)
}

// writeToWorkspace 将内容写入工作区，处理冲突
func (s *ImportService) writeToWorkspace(workspacePath, targetRel string, content []byte, strategy ConflictStrategy, visited map[string]bool, result *ImportResult) error {
	// 路径穿越防护
	clean := filepath.Clean(targetRel)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return core.WrapError(core.ErrInvalidInput, "非法目标路径: "+targetRel, nil)
	}

	finalRel := clean
	exists := false
	if _, err := os.Stat(filepath.Join(workspacePath, finalRel)); err == nil {
		exists = true
	} else if visited[finalRel] {
		exists = true // 同一次导入中的重复目标
	}

	if exists {
		switch strategy {
		case ConflictRename:
			base := strings.TrimSuffix(finalRel, filepath.Ext(finalRel))
			ext := filepath.Ext(finalRel)
			for i := 1; ; i++ {
				candidate := filepath.Join(filepath.Dir(finalRel), base+" ("+strconv.Itoa(i)+")"+ext)
				if _, err := os.Stat(filepath.Join(workspacePath, candidate)); os.IsNotExist(err) && !visited[candidate] {
					finalRel = candidate
					result.Renamed++
					break
				}
			}
		case ConflictOverwrite:
			// 直接覆盖
		default: // skip
			result.Skipped++
			return nil
		}
	}

	full := filepath.Join(workspacePath, finalRel)
	if err := os.MkdirAll(filepath.Dir(full), 0750); err != nil {
		return core.OsToNVError(err, "创建目录失败: "+filepath.Dir(full))
	}
	if err := os.WriteFile(full, content, 0644); err != nil {
		return core.OsToNVError(err, "写入文件失败: "+full)
	}
	visited[finalRel] = true
	result.Imported++
	return nil
}

// normalizeStrategy 归一化冲突策略
func normalizeStrategy(s string) ConflictStrategy {
	switch ConflictStrategy(strings.ToLower(strings.TrimSpace(s))) {
	case ConflictRename:
		return ConflictRename
	case ConflictOverwrite:
		return ConflictOverwrite
	default:
		return ConflictSkip
	}
}

// isMarkdownFile 判断文件名是否为 Markdown
func isMarkdownFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}
