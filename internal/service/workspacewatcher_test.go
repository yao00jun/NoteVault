package service

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ---------------------------------------------------------------------------
// 纯函数单测（无需真实文件系统 / fsnotify）
// ---------------------------------------------------------------------------

func TestIsIgnoredPath(t *testing.T) {
	root := "/vault"
	cases := []struct {
		path   string
		ignore bool
	}{
		{"/vault/notes/a.md", false},
		{"/vault/.git/config", true},
		{"/vault/.notevault/index.json", true},
		{"/vault/node_modules/foo/bar.js", true},
		{"/vault/notes/.DS_Store", true},
		{"/vault/notes/Thumbs.db", true},
		{"/vault/notes/draft.md~", true},
		{"/vault/notes/draft.md.swp", true},
		{"/vault/notes/draft.tmp", true},
		{"/vault/.git/refs/heads/main", true},
	}
	for _, c := range cases {
		if got := isIgnoredPath(root, c.path); got != c.ignore {
			t.Errorf("isIgnoredPath(%q) = %v, want %v", c.path, got, c.ignore)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		op     fsnotify.Op
		want   FileChangeType
		wantOK bool
	}{
		{fsnotify.Create, FileChangeCreate, true},
		{fsnotify.Write, FileChangeModify, true},
		{fsnotify.Remove, FileChangeDelete, true},
		{fsnotify.Rename, FileChangeDelete, true},
		{fsnotify.Chmod, "", false},
	}
	for _, c := range cases {
		got, ok := classify(c.op)
		if ok != c.wantOK || got != c.want {
			t.Errorf("classify(%v) = (%q, %v), want (%q, %v)", c.op, got, ok, c.want, c.wantOK)
		}
	}
}

// ---------------------------------------------------------------------------
// 真实 fsnotify 集成测试（临时目录，验证端到端推送）
// ---------------------------------------------------------------------------

// recordingSink 把收到的事件记下来，供测试断言。
//
// notify 是事件驱动的唤醒信号：waitHas 不再用纯轮询死等——
// 全量测试并发跑的时候，CPU 饥饿会把 3 秒忙等轮询拖成超时误报。
type recordingSink struct {
	mu     sync.Mutex
	events []FileChangeEvent
	notify chan struct{}
}

func (r *recordingSink) OnFileChange(e FileChangeEvent) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default: // 已有未消费的信号，够唤醒了
	}
}

func (r *recordingSink) has(wantType FileChangeType, wantPath string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Type == wantType && e.Path == wantPath {
			return true
		}
	}
	return false
}

func (r *recordingSink) anyPathContains(frag string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if strings.Contains(e.Path, frag) {
			return true
		}
	}
	return false
}

// waitHasTimeout 等待目标事件出现，最长 10 秒。
// 时限给得很宽：fsnotify 事件本身毫秒级到达，这里防的是
// CI / 全量并发下的调度饥饿，不是慢盘。
func (r *recordingSink) waitHasTimeout(t *testing.T, wantType FileChangeType, wantPath string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		if r.has(wantType, wantPath) {
			return
		}
		select {
		case <-r.notify:
		case <-deadline.C:
			r.mu.Lock()
			defer r.mu.Unlock()
			t.Fatalf("timed out waiting for %s %s; collected events: %v", wantType, wantPath, r.events)
		}
	}
}

func (r *recordingSink) waitHas(t *testing.T, wantType FileChangeType, wantPath string) {
	t.Helper()
	r.waitHasTimeout(t, wantType, wantPath)
}

func newRecordingSink() *recordingSink {
	return &recordingSink{notify: make(chan struct{}, 1)}
}

// TestWorkspaceWatcher_PushesRealChanges 端到端验证：
// 在临时目录里创建 / 修改 / 删除文件，watcher 都应当及时推对应事件，
// 且子目录（递归监控）和 .git 这类忽略目录都不应漏报或误报。
func TestWorkspaceWatcher_PushesRealChanges(t *testing.T) {
	root := t.TempDir()
	sink := newRecordingSink()
	w, err := NewWorkspaceWatcher(root, sink)
	if err != nil {
		t.Fatalf("NewWorkspaceWatcher: %v", err)
	}
	w.Start()
	defer w.Stop()

	// 1) 新建文件 → create
	aPath := filepath.Join(root, "a.md")
	if err := os.WriteFile(aPath, []byte("# a"), 0644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	sink.waitHas(t, FileChangeCreate, "a.md")

	// 2) 修改已存在文件 → modify
	if err := os.WriteFile(aPath, []byte("# a edited"), 0644); err != nil {
		t.Fatalf("edit a.md: %v", err)
	}
	sink.waitHas(t, FileChangeModify, "a.md")

	// 3) 子目录里的文件（验证递归监控）→ create，路径带子目录前缀
	subNote := filepath.Join(root, "sub", "note.md")
	if err := os.MkdirAll(filepath.Dir(subNote), 0750); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.WriteFile(subNote, []byte("# note"), 0644); err != nil {
		t.Fatalf("write sub/note.md: %v", err)
	}
	sink.waitHas(t, FileChangeCreate, "sub/note.md")

	// 4) 删除文件 → delete
	if err := os.Remove(aPath); err != nil {
		t.Fatalf("remove a.md: %v", err)
	}
	sink.waitHas(t, FileChangeDelete, "a.md")

	// 5) 忽略目录 .git 里的变更不应产生任何事件
	gitFile := filepath.Join(root, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(gitFile), 0750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(gitFile, []byte("ignored"), 0644); err != nil {
		t.Fatalf("write .git/config: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if sink.anyPathContains(".git/") {
		t.Errorf(".git changes should be ignored, but got an event under .git/")
	}
}

// TestWorkspaceWatcher_NilSinkDoesNotPanic 零值 sink 不应 panic（内置 nopSink）。
func TestWorkspaceWatcher_NilSinkDoesNotPanic(t *testing.T) {
	root := t.TempDir()
	w, err := NewWorkspaceWatcher(root, nil)
	if err != nil {
		t.Fatalf("NewWorkspaceWatcher(nil sink): %v", err)
	}
	w.Start()
	defer w.Stop()
	// 写入文件：sink 是 nop，不应 panic。
	_ = os.WriteFile(filepath.Join(root, "x.md"), []byte("x"), 0644)
	time.Sleep(200 * time.Millisecond)
}
