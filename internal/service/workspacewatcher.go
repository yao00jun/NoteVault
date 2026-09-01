package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// 文件变更监控（E-6）
// ---------------------------------------------------------------------------
//
// 背景：前端的 WorkspaceEvent（插件收到的文件增删改通知）原本靠
// "轮询文件树 + 比对快照"产生（见 frontend/src/stores/pluginRuntime.ts 的
// diffTreeSnapshots）。这有两个硬伤：
//  1. 不实时——外部修改（用户用别的编辑器改了文件、或者 git pull）要等下一轮轮询；
//  2. 浪费——每次轮询都要重新遍历整棵树、重算每个文件的修改时间。
//
// 后端用 fsnotify 实时监听工作区目录树，把变更直接推成事件。
// 这是 E-6 的核心交付：用"内核事件推送"替换"轮询快照比对"。
//
// fsnotify 在三大平台都是非递归的（一次 Add 只监控一个目录），所以这里手动
// Walk 整棵树逐个 Add，并在"新建子目录"时补 Add、"删除子目录"由 fsnotify 自动回收。

// FileChangeType 与前端 WorkspaceEvent.type 对齐：create / modify / delete。
type FileChangeType string

const (
	FileChangeCreate FileChangeType = "create"
	FileChangeModify FileChangeType = "modify"
	FileChangeDelete FileChangeType = "delete"
)

// FileChangeEvent 是后端监测到的一次文件变更。
//
// Path 是相对工作区根目录的相对路径（以 "/" 分隔），与前端文件树节点对齐，
// 也避免把绝对路径这种敏感信息直接推到事件通道。
type FileChangeEvent struct {
	Type FileChangeType `json:"type"`
	Path string         `json:"path"`
}

// FileChangeSink 接收文件变更事件的出口。
//
// 抽象成接口是因为真正的推送要走到 Wails 事件总线（依赖全局 application 单例），
// 单元测试里它没有初始化，一调用就 panic。收窄成接口后，watcher 能在纯 Go 下完整测试。
type FileChangeSink interface {
	OnFileChange(event FileChangeEvent)
}

// nopFileChangeSink 丢弃所有事件。用于零值 / 未注入 sink 的场景。
type nopFileChangeSink struct{}

func (nopFileChangeSink) OnFileChange(FileChangeEvent) {}

// ignoredDirs 是 watcher 永远不进入、也不发出事件的目录。
//
// 这些目录要么是 NoteVault 自己的派生数据（.notevault，里面是索引/快照），
// 要么是版本控制（.git），要么是第三方依赖（node_modules）。它们不是"用户的笔记"，
// 变更不该通知插件——否则插件会在用户没碰笔记时被 .git 的内部变动刷屏。
var ignoredDirs = map[string]bool{
	".git":         true,
	".notevault":   true,
	"node_modules": true,
}

// isIgnoredPath 判断一个路径是否应当被 watcher 忽略。
//
// 同时过滤掉编辑器保存时产生的临时文件（.swp / ~ / .tmp）和操作系统垃圾
// （.DS_Store / Thumbs.db），否则用户每保存一次就会冒出一堆无意义事件，
// 既刷屏又可能触发插件误动作。
func isIgnoredPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if ignoredDirs[part] {
			return true
		}
	}
	base := filepath.Base(path)
	switch {
	case base == ".DS_Store", base == "Thumbs.db":
		return true
	case strings.HasSuffix(base, "~"):
		return true
	case strings.HasSuffix(base, ".swp"):
		return true
	case strings.HasSuffix(base, ".tmp"):
		return true
	}
	return false
}

// classify 把 fsnotify 的操作翻译成我们的事件类型。
// Create → create；Write → modify；Remove / Rename → delete；Chmod 等忽略。
func classify(op fsnotify.Op) (FileChangeType, bool) {
	switch {
	case op.Has(fsnotify.Create):
		return FileChangeCreate, true
	case op.Has(fsnotify.Write):
		return FileChangeModify, true
	case op.Has(fsnotify.Remove), op.Has(fsnotify.Rename):
		return FileChangeDelete, true
	default:
		return "", false
	}
}

// WorkspaceWatcher 用 fsnotify 监控工作区目录树，把变更推给 sink。
type WorkspaceWatcher struct {
	root string
	sink FileChangeSink
	w    *fsnotify.Watcher

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// NewWorkspaceWatcher 创建并初始化 watcher，同时把整棵目录树都挂上监控。
// root 不存在时返回错误；sink 为 nil 时退化为丢弃事件。
func NewWorkspaceWatcher(root string, sink FileChangeSink) (*WorkspaceWatcher, error) {
	if sink == nil {
		sink = nopFileChangeSink{}
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建文件监控器失败: %w", err)
	}
	w := &WorkspaceWatcher{
		root: root,
		sink: sink,
		w:    fw,
		done: make(chan struct{}),
	}
	if err := w.walkAndAdd(root); err != nil {
		fw.Close()
		return nil, err
	}
	return w, nil
}

// walkAndAdd 递归给每个目录挂上监控，遇到 ignoredDirs 直接跳过整棵子树。
func (w *WorkspaceWatcher) walkAndAdd(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// 单点失败（权限、符号链接环）不应中断整棵树。
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if isIgnoredPath(w.root, path) {
			return filepath.SkipDir
		}
		// 监控单个目录失败不致命（权限、路径过长），跳过继续。
		_ = w.w.Add(path)
		return nil
	})
}

// Start 启动事件循环。调用方负责在不再需要时调用 Stop()。
func (w *WorkspaceWatcher) Start() {
	go w.loop()
}

// loop 是事件主循环，直到 Stop 或 watcher 出错关闭。
func (w *WorkspaceWatcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.w.Events:
			if !ok {
				return
			}
			w.handle(event)
		case _, ok := <-w.w.Errors:
			if !ok {
				return
			}
			// 监控错误（如被监控目录被卸载）只丢事件，不致命。
		}
	}
}

// handle 处理单个 fsnotify 事件：补监控新目录、过滤忽略项、分类并推送。
func (w *WorkspaceWatcher) handle(event fsnotify.Event) {
	path := event.Name

	// 新建子目录：补上监控，否则里面的文件变更收不到。
	if event.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if !isIgnoredPath(w.root, path) {
				_ = w.w.Add(path)
			}
		}
	}

	if isIgnoredPath(w.root, path) {
		return
	}
	ft, ok := classify(event.Op)
	if !ok {
		return
	}

	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		rel = path
	}
	w.sink.OnFileChange(FileChangeEvent{
		Type: ft,
		Path: filepath.ToSlash(rel),
	})
}

// Stop 停止监控并释放底层资源。重复调用安全。
func (w *WorkspaceWatcher) Stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	close(w.done)
	w.mu.Unlock()
	_ = w.w.Close()
}
