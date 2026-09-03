package service

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/notevault/notevault/internal/core"
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
// zip bomb 防护上限：单条目 50MB，全包累计 200MB。
// 条目头声明的 UncompressedSize64 可伪造，必须靠读取端硬截断兜底。
const (
	maxZipEntryBytes = 50 << 20
	maxZipTotalBytes = 200 << 20
)

type ImportService struct {
	// tasks 异步任务框架（E-5）。nil 时异步导入不可用，
	// 但同步导入照常工作——不能让任务框架变成导入功能的硬依赖。
	tasks *TaskService
}

// NewImportService 创建导入服务（不接异步任务框架）
func NewImportService() *ImportService {
	return &ImportService{}
}

// NewImportServiceWithTasks 创建导入服务并接入异步任务框架。
//
// 大库导入动辄几千个文件，同步执行会把 UI 卡死几十秒且无法中断。
// 接入后前端可以用返回的 taskID 查进度、随时取消。
func NewImportServiceWithTasks(tasks *TaskService) *ImportService {
	return &ImportService{tasks: tasks}
}

// ImportMarkdownFolder 将文件夹中的 Markdown 文件导入到工作区
// srcDir: 源文件夹绝对路径
// workspacePath: 目标工作区根目录绝对路径
// opts: 导入选项（空值使用默认）
//
// 这是同步版本，行为与历史完全一致，供脚本化调用与测试使用。
// 前端的大库导入应改用 ImportMarkdownFolderAsync。
func (s *ImportService) ImportMarkdownFolder(srcDir, workspacePath string, opts ImportOptions) (*ImportResult, error) {
	return s.importFolder(context.Background(), srcDir, workspacePath, opts, nil)
}

// ImportMarkdownFolderAsync 异步导入文件夹，立即返回任务 ID。
//
// 返回的是任务 ID 而不是结果：结果通过 task:finished 事件推送，
// 前端也可以随时用 TaskService.GetTask 查询。
//
// 导入期间前端 UI 不被阻塞，用户可以取消（TaskService.Cancel）。
// 取消后已写入的文件会保留——导入是逐文件落盘的，中途停下会让工作区处于
// "部分导入"状态，这一点前端必须如实告知用户，而不是假装什么都没发生。
func (s *ImportService) ImportMarkdownFolderAsync(srcDir, workspacePath string, opts ImportOptions) (string, error) {
	if s.tasks == nil {
		return "", core.NewError(core.ErrInternal, "异步任务框架未初始化")
	}
	// 参数校验在这里同步做掉：用户选错目录应当立刻得到反馈，
	// 而不是等一个"启动后马上失败"的任务。
	if srcDir == "" {
		return "", core.NewError(core.ErrInvalidInput, "源文件夹不能为空")
	}
	if workspacePath == "" {
		return "", core.NewError(core.ErrInvalidInput, "目标工作区不能为空")
	}

	handle := s.tasks.submit(context.Background(), "导入 Markdown 文件夹", func(ctx context.Context, rep TaskReporter) error {
		rep.Message("正在扫描文件…")
		result, err := s.importFolder(ctx, srcDir, workspacePath, opts, func(done, total int, current string) {
			rep.ReportMessage(done, total, "正在导入 "+filepath.Base(current))
		})
		if err != nil {
			return err
		}
		rep.Message(fmt.Sprintf("导入完成：新增 %d，跳过 %d，失败 %d",
			result.Imported, result.Skipped, len(result.Errors)))
		return nil
	})
	return handle.ID(), nil
}

// importFolder 是文件夹导入的实际实现。
//
// onProgress 为 nil 时表示不做进度回报（同步调用路径）。
// ctx 用于取消：遍历的每个文件都是检查点，取消后不会开始下一个文件。
func (s *ImportService) importFolder(ctx context.Context, srcDir, workspacePath string, opts ImportOptions, onProgress func(done, total int, current string)) (*ImportResult, error) {
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

	// 先数一遍：没有总数就算不出百分比，进度条会退化成"一直在转"。
	// 多一次遍历的代价（只读目录项，不读文件内容）远小于进度可见的收益。
	total := 0
	if onProgress != nil {
		total = countMarkdownFiles(srcDir)
	}

	result := &ImportResult{}
	visited := make(map[string]bool)
	done := 0

	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		// 取消检查点：每个条目进来都看一眼，用户点取消后最多再多处理一个文件。
		if err := ctx.Err(); err != nil {
			return err
		}
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
		if onProgress != nil {
			onProgress(done, total, path)
		}
		if err := s.importOneFile(path, workspacePath, targetRel, strategy, visited, result); err != nil {
			result.Errors = append(result.Errors, err.Error())
		}
		done++
		if onProgress != nil {
			onProgress(done, total, path)
		}
		return nil
	})

	// 取消导致的中断：如实返回，让任务框架归类为 cancelled 而不是 failed。
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if walkErr != nil {
		return nil, core.OsToNVError(walkErr, "遍历源文件夹失败")
	}
	return result, nil
}

// countMarkdownFiles 统计目录树里的 Markdown 文件数量（用于进度总量）。
// 遍历出错时返回目前已数到的数量——总量只影响进度显示，不值得因此失败。
func countMarkdownFiles(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if isMarkdownFile(d.Name()) {
			n++
		}
		return nil
	})
	return n
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
	// 累计写入量（zip bomb 熔断）
	var totalWritten int

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
		// zip bomb 防护：条目声明的解压大小不可信（可伪造），用 LimitReader
		// 硬截断单条目体积；全包累计写入量超限即终止导入。
		content, err := io.ReadAll(io.LimitReader(rc, maxZipEntryBytes+1))
		rc.Close()
		if err != nil {
			result.Errors = append(result.Errors, "读取 zip 条目失败: "+f.Name)
			continue
		}
		if len(content) > maxZipEntryBytes {
			result.Errors = append(result.Errors, "跳过超大条目（上限 "+strconv.Itoa(maxZipEntryBytes/1024/1024)+"MB）: "+f.Name)
			continue
		}
		totalWritten += len(content)
		if totalWritten > maxZipTotalBytes {
			return nil, core.WrapError(core.ErrInvalidInput, "zip 解压总量超出上限（"+strconv.Itoa(maxZipTotalBytes/1024/1024)+"MB），已终止导入", nil)
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
	// 路径穿越防护（与 FileService.confineToWorkspace 同一口径，zip 条目名不可信）
	if _, err := confineToWorkspace(workspacePath, targetRel); err != nil {
		return core.WrapError(core.ErrInvalidInput, "非法目标路径: "+targetRel, nil)
	}

	finalRel := filepath.Clean(targetRel)
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
