package service

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type WorkbenchAttachment struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	NotePath string `json:"notePath"`
	Warning  string `json:"warning"`
}

func sourceDocumentAttachment(relative string) bool {
	switch strings.ToLower(path.Ext(relative)) {
	case ".pdf", ".docx", ".epub":
		return true
	}
	return false
}

func sourceBookAttachments(root *os.Root, folder string) ([]WorkbenchAttachment, error) {
	attachments := []WorkbenchAttachment{}
	if err := sourceCheckPath(root, folder); err != nil {
		return attachments, err
	}
	visited := 0
	err := fs.WalkDir(root.FS(), folder, func(relative string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited++
		if visited > 20000 {
			return fmt.Errorf("分册目录条目过多")
		}
		if sourceIgnoredPath(relative) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !sourceDocumentAttachment(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			attachments = append(attachments, WorkbenchAttachment{Path: relative, Name: entry.Name(), Size: info.Size()})
		}
		return nil
	})
	return attachments, err
}

func workbenchBookSources(workspace string, book *WorkbenchBook) error {
	book.Attachments = []WorkbenchAttachment{}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	book.Attachments, err = sourceBookAttachments(root, book.Folder)
	if err != nil {
		return err
	}
	regular := func(relative string) bool {
		if sourceCheckPath(root, relative) != nil {
			return false
		}
		info, err := root.Stat(relative)
		return err == nil && info.Mode().IsRegular()
	}
	if candidate := path.Join(book.Folder, sourceAIName); regular(candidate) {
		book.StudyPlanPath = candidate
	}
	if candidate := path.Join(book.Folder, sourceLedgerName); regular(candidate) {
		book.SourceRecordsPath = candidate
	}
	manifest, _, _, err := sourceLoadManifest(root, book.Folder)
	if err != nil {
		return err
	}
	for i := range book.Attachments {
		attachment := &book.Attachments[i]
		for _, entry := range manifest.Entries {
			if sourceSamePath(attachment.Path, entry.Path) {
				attachment.Warning = entry.Warning
			}
			source, err := sourceRelativePath(entry.Source)
			if err == nil && sourceSamePath(attachment.Path, path.Join(book.Folder, source)) && isMarkdownFile(entry.Path) && regular(entry.Path) {
				attachment.NotePath, attachment.Warning = entry.Path, ""
			}
		}
	}
	return nil
}

func sourceStoredPDFInputs(workspace, folder string) ([]sourceImportInput, error) {
	relative, err := sourceRelativePath(folder)
	parts := strings.Split(relative, "/")
	if err != nil || len(parts) != 2 || parts[0] != "Learning" || strings.HasPrefix(parts[1], ".") {
		return nil, fmt.Errorf("请选择当前工作区 Learning 下的分册目录")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if _, exists, err := sourceReadRoot(root, path.Join(relative, "book.md"), sourceMaxFileBytes); err != nil || !exists {
		return nil, fmt.Errorf("分册不存在，请先创建或接入分册")
	}
	attachments, err := sourceBookAttachments(root, relative)
	if err != nil {
		return nil, err
	}
	manifest, _, _, err := sourceLoadManifest(root, relative)
	if err != nil {
		return nil, err
	}
	inputs := []sourceImportInput{}
	var total int
	for _, attachment := range attachments {
		if !strings.EqualFold(path.Ext(attachment.Path), ".pdf") {
			continue
		}
		data, _, err := sourceReadRoot(root, attachment.Path, sourceMaxFileBytes)
		if err != nil {
			return nil, err
		}
		total += len(data)
		if total > sourceMaxTotalBytes || len(inputs) >= sourceMaxFiles {
			return nil, fmt.Errorf("分册 PDF 超出 100 MiB 或 500 文件上限")
		}
		name := strings.TrimPrefix(attachment.Path, relative+"/")
		input := sourceImportInput{name: name, origin: "workspace:" + attachment.Path, data: data}
		sourceHash := sourceChecksum(data)
		for _, entry := range manifest.Entries {
			if sourceSamePath(entry.Path, attachment.Path) || entry.Source == name && entry.SourceSHA256 == sourceHash {
				input.origin = entry.Origin
				break
			}
		}
		inputs = append(inputs, input)
	}
	if len(inputs) == 0 {
		return nil, fmt.Errorf("分册目录中没有可提取的 PDF")
	}
	return inputs, nil
}

// Validate an explicit user-open action before passing a document to the shell.
func ResolveWorkspaceAttachment(workspace, relative string) (string, error) {
	clean, err := sourceRelativePath(relative)
	if err != nil || !sourceDocumentAttachment(clean) || sourceIgnoredPath(clean) {
		return "", fmt.Errorf("只能打开工作区中的 PDF、DOCX 或 EPUB 资料")
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := sourceCheckPath(root, clean); err != nil {
		return "", err
	}
	info, err := root.Stat(clean)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("原始资料不存在或不是普通文件")
	}
	return filepath.Abs(filepath.Join(workspace, filepath.FromSlash(clean)))
}
