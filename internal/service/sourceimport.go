package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/notevault/notevault/internal/core"
)

type CollectionKind string

type SourceImportFile struct {
	Name          string `json:"name"`
	ContentBase64 string `json:"contentBase64"`
}

// SourceImportAIConfig is transient request data. Never serialize the request
// into provenance, task messages, errors, or the generated collection metadata.
type SourceImportAIConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
}

type SourceImportRequest struct {
	SourceType       string               `json:"sourceType"`
	Source           string               `json:"source"`
	Files            []SourceImportFile   `json:"files"`
	Kind             CollectionKind       `json:"kind"`
	Name             string               `json:"name"`
	TargetFolder     string               `json:"targetFolder"`
	ConflictStrategy string               `json:"conflictStrategy"`
	Enrich           bool                 `json:"enrich"`
	Instruction      string               `json:"instruction"`
	AI               SourceImportAIConfig `json:"ai"`
}

type SourceImportPreview struct {
	Name         string         `json:"name"`
	Kind         CollectionKind `json:"kind"`
	TargetFolder string         `json:"targetFolder"`
	MetadataPath string         `json:"metadataPath"`
	FileCount    int            `json:"fileCount"`
	Files        []string       `json:"files"`
	Warnings     []string       `json:"warnings"`
	Existing     bool           `json:"existing"`
}

type SourceImportResult struct {
	TaskID       string   `json:"taskId"`
	TargetFolder string   `json:"targetFolder"`
	MetadataPath string   `json:"metadataPath"`
	Imported     int      `json:"imported"`
	Skipped      int      `json:"skipped"`
	Updated      int      `json:"updated"`
	Conflicts    []string `json:"conflicts"`
	Warnings     []string `json:"warnings"`
	Files        []string `json:"files"`
	Cancelled    bool     `json:"cancelled"`
}

const (
	sourceMaxFileBytes  = 20 << 20
	sourceMaxTotalBytes = 100 << 20
	sourceMaxFiles      = 500
	sourceLedgerName    = "sources.md"
	sourceAIName        = "ai-plan.md"
)

type sourceImportRecord struct {
	workspace string
	result    SourceImportResult
}

type sourceImportState struct {
	mu         sync.RWMutex
	results    map[string]*sourceImportRecord
	writeGate  chan struct{}
	httpClient *http.Client
}

type sourceImportInput struct {
	name    string
	origin  string
	baseURL string
	local   string
	data    []byte
}

type sourceImportOutput struct {
	name           string
	origin         string
	source         string
	sourceHash     string
	extractionMode string
	warning        string
	data           []byte
}

type sourceImportSetup struct {
	workspace string
	request   SourceImportRequest
	metadata  string
	inputs    []sourceImportInput
	warnings  []string
	existing  bool
}

type sourceImportPlan struct {
	outputs      []sourceImportOutput
	warnings     []string
	technologies []string
	skipped      int
}

// PreviewSourceImport validates and extracts bounded source data without
// creating directories, metadata, manifests, or any other workspace files.
func (s *ImportService) PreviewSourceImport(workspacePath string, request SourceImportRequest) (*SourceImportPreview, error) {
	setup, err := prepareSourceImportSetup(workspacePath, request)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	plan, err := s.prepareSourceImportPlan(ctx, setup)
	if err != nil {
		return nil, err
	}
	preview := &SourceImportPreview{
		Name: setup.request.Name, Kind: setup.request.Kind, TargetFolder: setup.request.TargetFolder,
		MetadataPath: setup.metadata, Files: []string{}, Warnings: plan.warnings, Existing: setup.existing,
	}
	for _, output := range plan.outputs {
		preview.Files = append(preview.Files, path.Join(setup.request.TargetFolder, output.name))
	}
	if request.SourceType == "adopt" {
		for _, input := range setup.inputs {
			preview.Files = append(preview.Files, path.Join(setup.request.TargetFolder, input.name))
		}
	}
	preview.FileCount = len(preview.Files)
	return preview, nil
}

// StartSourceImport uses the existing cancellable TaskService. Every completed
// file is entered into Markdown provenance before its progress is published.
func (s *ImportService) StartSourceImport(workspacePath string, request SourceImportRequest) (string, error) {
	if s.tasks == nil {
		return "", core.NewError(core.ErrInternal, "异步任务框架未初始化")
	}
	setup, err := prepareSourceImportSetup(workspacePath, request)
	if err != nil {
		return "", err
	}
	record := &sourceImportRecord{workspace: setup.workspace, result: SourceImportResult{
		TargetFolder: setup.request.TargetFolder,
		Conflicts:    []string{}, Warnings: []string{}, Files: []string{},
	}}
	s.sourceImports.mu.Lock()
	if s.sourceImports.results == nil {
		s.sourceImports.results = make(map[string]*sourceImportRecord)
		s.sourceImports.writeGate = make(chan struct{}, 1)
	}
	for id := range s.sourceImports.results {
		if s.tasks.GetTask(id) == nil {
			delete(s.sourceImports.results, id)
		}
	}
	s.sourceImports.mu.Unlock()
	registered := make(chan struct{})
	handle := s.tasks.submit(context.Background(), "导入资料", func(ctx context.Context, reporter TaskReporter) (taskErr error) {
		<-registered
		result := record.result
		publish := func() {
			s.sourceImports.mu.Lock()
			record.result = cloneSourceImportResult(result)
			s.sourceImports.mu.Unlock()
		}
		defer func() {
			if recover() != nil {
				taskErr = fmt.Errorf("资料解析失败；已保存的文件与导入记录已保留")
			}
			if ctx.Err() != nil {
				result.Cancelled = true
				taskErr = ctx.Err()
			}
			if taskErr != nil && !result.Cancelled {
				result.Warnings = append(result.Warnings, taskErr.Error())
			}
			publish()
		}()
		reporter.Message("正在检查与提取资料…")
		plan, err := s.prepareSourceImportPlan(ctx, setup)
		if err != nil {
			return err
		}
		result.Warnings = append(result.Warnings, plan.warnings...)
		result.Skipped = plan.skipped
		publish()
		// Keep conflicting imports serial, while cancellation still works in the
		// wait. Root-constrained filesystem operations also protect against links.
		select {
		case s.sourceImports.writeGate <- struct{}{}:
			defer func() { <-s.sourceImports.writeGate }()
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := s.executeSourceImport(ctx, setup, plan, &result, reporter, publish); err != nil {
			return err
		}
		reporter.Message(fmt.Sprintf("导入完成：新增 %d，更新 %d，跳过 %d", result.Imported, result.Updated, result.Skipped))
		return nil
	})
	s.sourceImports.mu.Lock()
	record.result.TaskID = handle.ID()
	s.sourceImports.results[handle.ID()] = record
	s.sourceImports.mu.Unlock()
	close(registered)
	return handle.ID(), nil
}

func (s *ImportService) GetSourceImportResult(workspacePath, taskID string) (*SourceImportResult, error) {
	workspace, err := sourceWorkspacePath(workspacePath)
	if err != nil {
		return nil, err
	}
	s.sourceImports.mu.RLock()
	record := s.sourceImports.results[taskID]
	if record == nil || !sourceSamePath(record.workspace, workspace) {
		s.sourceImports.mu.RUnlock()
		return nil, core.NewError(core.ErrNotFound, "当前工作区中没有此导入任务")
	}
	result := cloneSourceImportResult(record.result)
	s.sourceImports.mu.RUnlock()
	// TaskService does not invoke a task body when it is cancelled in its
	// queue, so the initial result must also reflect that terminal state.
	if info := s.tasks.GetTask(taskID); info != nil && info.Status == TaskCancelled {
		result.Cancelled = true
	}
	return &result, nil
}

func cloneSourceImportResult(result SourceImportResult) SourceImportResult {
	result.Conflicts = append([]string{}, result.Conflicts...)
	result.Warnings = append([]string{}, result.Warnings...)
	result.Files = append([]string{}, result.Files...)
	return result
}

func prepareSourceImportSetup(workspacePath string, request SourceImportRequest) (sourceImportSetup, error) {
	setup := sourceImportSetup{warnings: []string{}}
	workspace, err := sourceWorkspacePath(workspacePath)
	if err != nil {
		return setup, err
	}
	setup.workspace = workspace
	space, metadata := "", ""
	switch request.Kind {
	case "project":
		space, metadata = "Projects", "project.md"
	case "book":
		space, metadata = "Learning", "book.md"
	case "topic":
		space, metadata = "Resources", "index.md"
	default:
		return setup, fmt.Errorf("资料类型必须为 project、book 或 topic")
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Source = strings.TrimSpace(request.Source)
	request.Files = append([]SourceImportFile{}, request.Files...)
	if request.ConflictStrategy == "" {
		request.ConflictStrategy = "skip"
	}
	if request.ConflictStrategy != "skip" && request.ConflictStrategy != "update" && request.ConflictStrategy != "copy" {
		return setup, fmt.Errorf("冲突策略必须为 skip、update 或 copy")
	}
	if len(request.Instruction) > 16<<10 {
		return setup, fmt.Errorf("整理要求过长（上限 16 KiB）")
	}
	switch request.SourceType {
	case "empty":
		if request.Name == "" {
			return setup, fmt.Errorf("请填写名称")
		}
	case "folder", "adopt":
		if request.Source == "" && request.SourceType == "adopt" {
			request.Source = request.TargetFolder
		}
		local := request.Source
		if request.SourceType == "adopt" && !filepath.IsAbs(local) {
			relative, err := sourceRelativePath(local)
			if err != nil {
				return setup, err
			}
			local = filepath.Join(workspace, filepath.FromSlash(relative))
		}
		if !filepath.IsAbs(local) {
			return setup, fmt.Errorf("源文件夹必须是绝对路径")
		}
		local, err = sourceDirectoryPath(local)
		if err != nil {
			return setup, err
		}
		request.Source = local
		if request.Name == "" {
			request.Name = filepath.Base(local)
		}
		if request.SourceType == "adopt" {
			rel, err := filepath.Rel(workspace, local)
			if err != nil {
				return setup, fmt.Errorf("接管目录必须位于当前工作区")
			}
			rel = filepath.ToSlash(rel)
			if request.TargetFolder == "" {
				request.TargetFolder = rel
			}
			target, err := sourceRelativePath(request.TargetFolder)
			if err != nil || !sourceSamePath(filepath.Join(workspace, filepath.FromSlash(target)), local) {
				return setup, fmt.Errorf("接管必须使用原目录作为目标目录")
			}
		}
		setup.inputs, setup.warnings, err = sourceScanFolder(local)
		if err != nil {
			return setup, err
		}
	case "files":
		if len(request.Files) == 0 || len(request.Files) > sourceMaxFiles {
			return setup, fmt.Errorf("请选择 1 至 500 个文件")
		}
		var total int64
		seen := map[string]bool{}
		for _, file := range request.Files {
			relative, err := sourceRelativePath(file.Name)
			if err != nil {
				return setup, err
			}
			if seen[strings.ToLower(relative)] {
				return setup, fmt.Errorf("上传文件含重复相对路径，请保留来源目录结构")
			}
			seen[strings.ToLower(relative)] = true
			if len(file.ContentBase64) > base64.StdEncoding.EncodedLen(sourceMaxFileBytes) {
				return setup, fmt.Errorf("单个源文件不能超过 20 MiB")
			}
			n, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(file.ContentBase64)))
			if err != nil {
				return setup, fmt.Errorf("文件内容不是有效的 Base64：%s", file.Name)
			}
			total += n
			if n > sourceMaxFileBytes || total > sourceMaxTotalBytes {
				return setup, fmt.Errorf("源文件超出 20 MiB 单文件或 100 MiB 总量限制")
			}
		}
		if request.Name == "" {
			request.Name = strings.TrimSuffix(path.Base(strings.ReplaceAll(request.Files[0].Name, "\\", "/")), path.Ext(request.Files[0].Name))
		}
	case "url":
		u, err := sourceParseURL(request.Source)
		if err != nil {
			return setup, err
		}
		if request.Name == "" {
			request.Name = sourceURLName(u)
		}
	default:
		return setup, fmt.Errorf("不支持的导入来源")
	}
	if _, err := sourceRelativePath(request.Name); err != nil || strings.ContainsAny(request.Name, "/\\") || strings.HasPrefix(request.Name, ".") || len([]rune(request.Name)) > 120 {
		return setup, fmt.Errorf("名称必须是有效的单个文件夹名称，长度不超过 120 字符")
	}
	if request.TargetFolder == "" {
		request.TargetFolder = path.Join(space, request.Name)
	}
	target, err := sourceRelativePath(request.TargetFolder)
	if err != nil {
		return setup, err
	}
	parts := strings.Split(target, "/")
	if len(parts) != 2 || parts[0] != space || strings.HasPrefix(parts[1], ".") {
		return setup, fmt.Errorf("目标目录必须是 %s 下的一个资料文件夹", space)
	}
	request.TargetFolder = target
	setup.request = request
	setup.metadata = path.Join(target, metadata)
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return setup, fmt.Errorf("无法打开工作区")
	}
	defer root.Close()
	for _, rel := range []string{target, setup.metadata, path.Join(target, sourceLedgerName), path.Join(target, sourceAIName)} {
		if err := sourceCheckPath(root, rel); err != nil {
			return setup, err
		}
	}
	if info, err := root.Stat(target); err == nil {
		if !info.IsDir() {
			return setup, fmt.Errorf("目标路径不是目录")
		}
		setup.existing = true
	} else if !os.IsNotExist(err) {
		return setup, fmt.Errorf("无法访问目标目录")
	}
	if request.SourceType == "adopt" && !setup.existing {
		return setup, fmt.Errorf("接管目录不存在")
	}
	if request.SourceType == "folder" {
		srcTarget := filepath.Join(workspace, filepath.FromSlash(target))
		if sourcePathWithin(request.Source, srcTarget) || sourcePathWithin(srcTarget, request.Source) {
			return setup, fmt.Errorf("导入源与目标目录不能相同或互相包含；已有目录请使用接管")
		}
	}
	return setup, nil
}

func (s *ImportService) prepareSourceImportPlan(ctx context.Context, setup sourceImportSetup) (sourceImportPlan, error) {
	plan := sourceImportPlan{outputs: []sourceImportOutput{}, warnings: append([]string{}, setup.warnings...)}
	if setup.request.SourceType == "empty" || setup.request.SourceType == "adopt" {
		return plan, nil
	}
	inputs := setup.inputs
	if setup.request.SourceType == "files" {
		for _, file := range setup.request.Files {
			name, _ := sourceRelativePath(file.Name)
			if sourceIgnoredPath(name) {
				plan.skipped++
				plan.warnings = append(plan.warnings, "已忽略凭据、缓存或生成文件："+name)
				continue
			}
			data, err := base64.StdEncoding.DecodeString(file.ContentBase64)
			if err != nil {
				return plan, fmt.Errorf("文件编码无效")
			}
			inputs = append(inputs, sourceImportInput{name: name, origin: "upload:" + name, data: data})
		}
	}
	if setup.request.SourceType == "url" {
		var err error
		inputs, err = s.sourceFetchInputs(ctx, setup.request.Source)
		if err != nil {
			return plan, err
		}
	}
	budget := &sourceImportBudget{}
	technologies := map[string]bool{}
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		if input.local != "" {
			data, err := sourceReadLocalFile(ctx, setup.request.Source, input.local)
			if err != nil {
				return plan, err
			}
			input.data = data
		}
		if setup.request.Kind == "project" {
			for _, technology := range sourceInferTechnologies(input.name, input.data) {
				technologies[technology] = true
			}
		}
		outputs, warnings, err := sourceExtractInput(ctx, input, budget, 0)
		if err != nil {
			return plan, err
		}
		plan.warnings = append(plan.warnings, warnings...)
		// Extraction notices represent skipped leaf sources, including members
		// of an otherwise successful archive.
		plan.skipped += len(warnings)
		if len(outputs) == 0 && len(warnings) == 0 {
			plan.skipped++
		}
		plan.outputs = append(plan.outputs, outputs...)
	}
	for technology := range technologies {
		plan.technologies = append(plan.technologies, technology)
	}
	sort.Strings(plan.technologies)
	seen := map[string]bool{}
	var total int
	root, err := os.OpenRoot(setup.workspace)
	if err != nil {
		return plan, fmt.Errorf("无法打开工作区")
	}
	defer root.Close()
	if _, _, _, err := sourceLoadManifest(root, setup.request.TargetFolder); err != nil {
		return plan, err
	}
	for i := range plan.outputs {
		output := &plan.outputs[i]
		if output.warning != "" {
			plan.warnings = append(plan.warnings, output.warning)
		}
		name := output.name
		if strings.EqualFold(name, path.Base(setup.metadata)) || strings.EqualFold(name, sourceLedgerName) || strings.EqualFold(name, sourceAIName) {
			name = "source-" + name
			plan.warnings = append(plan.warnings, "来源文件与资料元数据重名，已保留为："+name)
		}
		for n := 2; seen[strings.ToLower(name)]; n++ {
			name = sourceNumberedPath(output.name, n)
		}
		output.name = name
		seen[strings.ToLower(name)] = true
		total += len(output.data)
		if len(output.data) > sourceMaxFileBytes || total > sourceMaxTotalBytes || len(plan.outputs) > sourceMaxFiles {
			return plan, fmt.Errorf("提取结果超过 20 MiB 单文件、100 MiB 总量或 500 文件上限")
		}
		if err := sourceCheckPath(root, path.Join(setup.request.TargetFolder, name)); err != nil {
			return plan, err
		}
		if info, err := root.Lstat(path.Join(setup.request.TargetFolder, name)); err == nil && info.IsDir() {
			return plan, fmt.Errorf("目标资料路径已被目录占用：%s", name)
		}
	}
	return plan, nil
}
