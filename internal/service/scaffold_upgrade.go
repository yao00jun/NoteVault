package service

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

// Fingerprints of the exact templates/guides shipped before this workflow.
// Regression fixtures retain the corresponding originals. Only CRLF/LF variants
// are equivalent; whitespace changes and any user edits make a file ineligible.
var legacyScaffoldDigests = map[string][]string{
	"开始使用.md":           {"4fcacb6e9a665f847ee25cbdc2397df1d9a2c2bb825b60e392b98cce0705feed"},
	"Inbox/使用引导.md":     {"afc78c93826286c002d3bab1256d9f4838ffa4ae0f5b3773bff2688e13a2cad9"},
	"Templates/日记模板.md": {"05bac6bfdc584596f6a4191d9fc0d6de959cfd8d55ff79e75e11cea43824317e"},
	"Templates/会议记录.md": {
		"42f78edfe2d7abab2fc0a1c2a806c809c93301c66a2afbc4a9c7c5ce2e4c8126",
		"bfa1c7c8fd482850bc45f4f4c0fa0b8e68b6ae17dcbf0e93419581b5e8bbd1c9",
	},
	"Templates/读书笔记.md": {
		"2fa45468e160b46df020895ffbad7a4512496cfc015a5787fccdebae93a90a6e",
		"ed2fe8d55b19e8b6b28945410ad219aeb32acb8dbe8abcf975ad0497475c3f2f",
	},
	"Templates/待办清单.md":  {"ed707df097e40e6e1195924bdd7b79db526f45fe47eb0e594234b6df14af1d9a"},
	"Templates/每周回顾.md":  {"0592ab3300cea26b431bd307384fe117bffa18bf398185cd8ac44a28ceb5db01"},
	"Templates/项目.md":    {"85f5ef7888d59d34b83b7cf76385fe89935cd100cad3597ab314ed108e4b197b"},
	"Templates/Daily.md": {"ab6963ef3ab4bd74fbf1b46276feffa77b7f7a8bcdd6cf83cd221f0c150399ee"},
}

func normalizeScaffoldDefault(content string) string {
	return strings.ReplaceAll(content, "\r\n", "\n")
}

func isLegacyScaffoldDefault(relative, content string) bool {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(normalizeScaffoldDefault(content))))
	for _, known := range legacyScaffoldDigests[relative] {
		if digest == known {
			return true
		}
	}
	return false
}

func ensureScaffoldFile(root *os.Root, relative, content string, currentDefault bool) error {
	before, exists, err := sourceReadRoot(root, relative, sourceMaxFileBytes)
	if err != nil {
		return err
	}
	if !exists {
		if !currentDefault {
			return nil
		}
		return sourceWriteRoot(root, relative, []byte(content), nil, false)
	}
	if !isLegacyScaffoldDefault(relative, string(before)) {
		return nil
	}
	backup := ".notevault/scaffold-backups/2026-09-09/" + relative
	saved, savedExists, err := sourceReadRoot(root, backup, sourceMaxFileBytes)
	if err != nil {
		return err
	}
	if savedExists && !bytes.Equal(saved, before) {
		return fmt.Errorf("旧默认文件的备份已存在且内容不同，保留原文件: %s", relative)
	}
	if !savedExists {
		if err := sourceWriteRoot(root, backup, before, nil, false); err != nil {
			return err
		}
	}
	if currentDefault {
		return sourceWriteRoot(root, relative, []byte(content), before, true)
	}
	// Retiring a shipped template never touches Daily/ or user-edited templates.
	current, _, err := sourceReadRoot(root, relative, sourceMaxFileBytes)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return fmt.Errorf("模板已在其他位置修改，保留最新内容: %s", relative)
	}
	return root.Remove(relative)
}
