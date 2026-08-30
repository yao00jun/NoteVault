package plugin

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

// bundledPlugins 预装插件源码。
//
// 为什么必须 go:embed 而不是放在安装目录：NoteVault 是单文件应用，
// 外部文件在便携版 / 手动拷贝的场景下可能丢失，编进二进制才能保证「一定有」。
//
//go:embed bundled/*.js
var bundledPlugins embed.FS

// installBundledPlugins 首次启动时把预装插件写入插件目录并默认启用。
//
// 存在意义：工具栏插件化之后宿主不再内置任何按钮（对齐 Obsidian 的做法）。
// 若不预装，新用户第一次打开会看到一个空工具栏——
// 这是「把功能交给插件」必须补上的一环，否则等于把成本转嫁给用户。
//
// 已在插件目录存在同名文件时的处理（按健康度分级）：
//   - manifest 能解析且声明了 permissions → **不覆盖**，尊重用户可能的修改；
//   - manifest 解析失败 **或** permissions 为空 → 升级为预装版。
//     这是修 bug：旧版若没正确声明权限（常见原因：文件头有 /// 引用行
//     阻塞 frontmatter 解析），整插件启动会失败；不能因为「不覆盖」就拒绝修复。
//
// 用户想真的关掉预装行为，应该走 disable 机制（PluginService.DisablePlugin），
// 不是留个坏掉的预装文件在那里。
func (s *PluginService) installBundledPlugins() {
	entries, err := bundledPlugins.ReadDir("bundled")
	if err != nil {
		return
	}

	state := s.loadEnabledState()
	changed := false

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}

		target := filepath.Join(s.pluginsDir, entry.Name())

		// 先把预装版字节读出来——无论下面走"跳过"还是"覆盖"分支都要用
		data, err := bundledPlugins.ReadFile("bundled/" + entry.Name())
		if err != nil {
			continue
		}

		needWrite := true
		if existingData, readErr := os.ReadFile(target); readErr == nil {
			if existing, _, parseErr := parsePluginManifest(string(existingData), entry.Name()); parseErr == nil && len(existing.Permissions) > 0 {
				needWrite = false // 健康：跳过覆盖
			}
			// 损坏或无 permissions → 需要写
		}

		if needWrite {
			if err := os.MkdirAll(s.pluginsDir, 0750); err != nil {
				continue
			}
			if err := os.WriteFile(target, data, 0640); err != nil {
				continue
			}
		}

		// 启用键用 manifest 里的 id，与扫描逻辑保持一致
		manifest, _, err := parsePluginManifest(string(data), entry.Name())
		if err != nil || manifest.ID == "" {
			continue
		}
		if _, exists := state[manifest.ID]; !exists {
			state[manifest.ID] = true
			changed = true
		}
	}

	if changed {
		_ = s.saveEnabledState(state)
	}
}
