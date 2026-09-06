package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 蓝图专项 6：媒体资产全生命周期管理（孤立附件 GC）。
//
// 原则：扫描是纯只读；清理走 TrashService 移入 .trash/（可恢复），
// 绝不物理粉碎。判定口径保守——只要资产文件名出现在工作区任何
// 文本文件（md/canvas）里就视为被引用，宁可漏报不可误删。

// ScanOrphanAssets 扫描 assets/ 下未被任何笔记引用的文件。
// 引用判定：资产文件名（basename）出现在工作区任一 .md / .canvas 文本中
// 即视为被引用（覆盖 ![](assets/x.png)、![[x.png]] 与 Canvas JSON 引用）。
func (s *FileService) ScanOrphanAssets(workspacePath string) ([]string, error) {
	assetsDir := filepath.Join(workspacePath, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // 没有 assets 目录就是没有孤立附件
		}
		return nil, fmt.Errorf("读取 assets 目录失败: %w", err)
	}

	var assetFiles []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		assetFiles = append(assetFiles, e.Name())
	}
	if len(assetFiles) == 0 {
		return []string{}, nil
	}

	// 汇集全库文本（md + canvas）的引用集（按 basename）
	referenced := map[string]bool{}
	walkErr := filepath.Walk(workspacePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if path != workspacePath && (strings.HasPrefix(name, ".") || name == "assets") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".markdown" && ext != ".canvas" {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		text := string(data)
		for _, a := range assetFiles {
			if strings.Contains(text, a) {
				referenced[a] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("遍历工作区失败: %w", walkErr)
	}

	orphanRelPaths := make([]string, 0)
	for _, name := range assetFiles {
		if !referenced[name] {
			orphanRelPaths = append(orphanRelPaths, filepath.ToSlash(filepath.Join("assets", name)))
		}
	}
	sort.Strings(orphanRelPaths)
	return orphanRelPaths, nil
}

// MoveOrphansToTrash 把指定孤立附件逐个移入 .trash/（可恢复）。
// 单个失败不中断整体，返回成功数量与最后一个错误。
func (s *FileService) MoveOrphansToTrash(workspacePath string, relPaths []string) (int, error) {
	trash := NewTrashService()
	moved := 0
	var lastErr error
	for _, rel := range relPaths {
		node, err := trash.MoveToTrash(workspacePath, rel)
		if err != nil {
			lastErr = err
			continue
		}
		_ = node
		moved++
	}
	return moved, lastErr
}
