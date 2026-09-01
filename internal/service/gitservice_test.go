package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitRunner 记录每条命令，并按预设脚本回复。
type fakeGitRunner struct {
	calls     []string          // 每次 call 的 "dir|args..." 拼接
	responses map[string]string // key 为首参数（子命令），value 为合并输出
	fail      map[string]bool   // 需要返回错误的子命令
}

func newFakeGitRunner() *fakeGitRunner {
	return &fakeGitRunner{
		responses: map[string]string{},
		fail:      map[string]bool{},
	}
}

func (f *fakeGitRunner) run(ctx context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, dir+"|"+strings.Join(args, " "))
	first := args[0]
	if f.fail[first] {
		return f.responses[first], &fakeGitError{}
	}
	return f.responses[first], nil
}

type fakeGitError struct{}

func (*fakeGitError) Error() string { return "git exit status 128" }

func (f *fakeGitRunner) calledWith(sub string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, "|"+sub) {
			return true
		}
	}
	return false
}

func TestGitService_Status(t *testing.T) {
	t.Run("git 未安装", func(t *testing.T) {
		runner := newFakeGitRunner()
		runner.fail["--version"] = true
		svc := NewGitServiceWithRunner(runner.run)
		st, err := svc.Status(t.TempDir())
		if err != nil {
			t.Fatalf("Status 不应报错: %v", err)
		}
		if st.Installed {
			t.Fatal("git 不可用时 installed 应为 false")
		}
		if st.IsRepo || st.Branch != "" {
			t.Fatal("未安装时不应读仓库状态")
		}
	})

	t.Run("非仓库目录", func(t *testing.T) {
		svc := NewGitServiceWithRunner(newFakeGitRunner().run)
		st, err := svc.Status(t.TempDir())
		if err != nil {
			t.Fatalf("Status 不应报错: %v", err)
		}
		if !st.Installed {
			t.Fatal("git 可用时 installed 应为 true")
		}
		if st.IsRepo {
			t.Fatal("没有 .git 时 isRepo 应为 false")
		}
	})

	t.Run("仓库目录解析分支与变更数", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["status"] = "## main...origin/main [ahead 1]\n M note.md\n?? new.md\n?? Draft/todo.md\n D old.md\n"
		svc := NewGitServiceWithRunner(runner.run)
		st, err := svc.Status(dir)
		if err != nil {
			t.Fatalf("Status 不应报错: %v", err)
		}
		if !st.IsRepo {
			t.Fatal("有 .git 时 isRepo 应为 true")
		}
		if st.Branch != "main" {
			t.Fatalf("branch 应解析为 main, got %q", st.Branch)
		}
		if st.Untracked != 2 || st.Changed != 2 {
			t.Fatalf("untracked=%d changed=%d, want 2/2", st.Untracked, st.Changed)
		}
	})

	t.Run("No commits yet 分支解析", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["status"] = "## No commits yet on master\n?? a.md\n"
		svc := NewGitServiceWithRunner(runner.run)
		st, _ := svc.Status(dir)
		if st.Branch != "master" {
			t.Fatalf("branch 应解析为 master, got %q", st.Branch)
		}
	})
}

func TestGitService_InitRepo(t *testing.T) {
	t.Run("非仓库时执行 git init 并生成 .gitignore", func(t *testing.T) {
		dir := t.TempDir()
		runner := newFakeGitRunner()
		svc := NewGitServiceWithRunner(runner.run)
		if err := svc.InitRepo(dir); err != nil {
			t.Fatalf("InitRepo 失败: %v", err)
		}
		if !runner.calledWith("init") {
			t.Fatalf("应执行 git init, calls=%v", runner.calls)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatalf("应生成 .gitignore: %v", err)
		}
		if !strings.Contains(string(data), ".notevault/") {
			t.Fatal(".gitignore 应排除 .notevault/")
		}
	})

	t.Run("已是仓库时不重复 init 但补 .gitignore", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		svc := NewGitServiceWithRunner(runner.run)
		if err := svc.InitRepo(dir); err != nil {
			t.Fatalf("InitRepo 失败: %v", err)
		}
		if runner.calledWith("init") {
			t.Fatal("已有 .git 时不应再 init")
		}
		if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
			t.Fatal("已有仓库也应补 .gitignore")
		}
	})

	t.Run("init 失败时返回带输出的错误", func(t *testing.T) {
		runner := newFakeGitRunner()
		runner.fail["init"] = true
		runner.responses["init"] = "fatal: unsafe repository"
		svc := NewGitServiceWithRunner(runner.run)
		err := svc.InitRepo(t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "unsafe repository") {
			t.Fatalf("应包含 git 输出, got %v", err)
		}
	})
}

func TestGitService_EnsureGitignore(t *testing.T) {
	t.Run("新建后幂等", func(t *testing.T) {
		dir := t.TempDir()
		svc := NewGitServiceWithRunner(newFakeGitRunner().run)
		created, err := svc.EnsureGitignore(dir)
		if err != nil || !created {
			t.Fatalf("首次应新建: created=%v err=%v", created, err)
		}
		created, err = svc.EnsureGitignore(dir)
		if err != nil || created {
			t.Fatalf("二次应跳过: created=%v err=%v", created, err)
		}
	})

	t.Run("用户已有 .gitignore 时不覆盖", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/私有/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		svc := NewGitServiceWithRunner(newFakeGitRunner().run)
		created, err := svc.EnsureGitignore(dir)
		if err != nil || created {
			t.Fatalf("已有文件不应覆盖: created=%v err=%v", created, err)
		}
		data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if string(data) != "/私有/\n" {
			t.Fatalf("用户内容被篡改: %q", data)
		}
	})
}

func TestGitService_CommitAll(t *testing.T) {
	t.Run("非仓库报错", func(t *testing.T) {
		svc := NewGitServiceWithRunner(newFakeGitRunner().run)
		if _, err := svc.CommitAll(t.TempDir(), "msg"); err == nil {
			t.Fatal("非仓库应报错")
		}
	})

	t.Run("暂存区为空时是正常路径", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["diff"] = ""
		svc := NewGitServiceWithRunner(runner.run)
		msg, err := svc.CommitAll(dir, "")
		if err != nil {
			t.Fatalf("空暂存不应报错: %v", err)
		}
		if !strings.Contains(msg, "没有可提交") {
			t.Fatalf("应提示无可提交, got %q", msg)
		}
		if runner.calledWith("commit") {
			t.Fatal("没有变更时不应执行 commit")
		}
	})

	t.Run("有暂存内容则 add + commit", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["diff"] = "note.md\n"
		runner.responses["commit"] = "[main abc123] ok\n"
		svc := NewGitServiceWithRunner(runner.run)
		msg, err := svc.CommitAll(dir, "写笔记")
		if err != nil {
			t.Fatalf("CommitAll 失败: %v", err)
		}
		if !strings.Contains(msg, "提交成功") {
			t.Fatalf("应报成功, got %q", msg)
		}
		if !runner.calledWith("add") || !runner.calledWith("commit") {
			t.Fatalf("应执行 add 与 commit, calls=%v", runner.calls)
		}
		// 提交信息作为独立参数传递（防注入）
		found := false
		for _, c := range runner.calls {
			if strings.HasSuffix(c, "|commit -m 写笔记") {
				found = true
			}
		}
		if !found {
			t.Fatalf("commit -m 应携带完整消息参数, calls=%v", runner.calls)
		}
	})

	t.Run("Git 身份未配置给出指引", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["diff"] = "note.md\n"
		runner.fail["commit"] = true
		runner.responses["commit"] = "Please tell me who you are... user.name\n"
		svc := NewGitServiceWithRunner(runner.run)
		_, err := svc.CommitAll(dir, "msg")
		if err == nil || !strings.Contains(err.Error(), "user.name") {
			t.Fatalf("身份未配置应给指引, got %v", err)
		}
	})

	t.Run("空消息使用默认提交信息", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".git"), 0o750); err != nil {
			t.Fatal(err)
		}
		runner := newFakeGitRunner()
		runner.responses["diff"] = "note.md\n"
		svc := NewGitServiceWithRunner(runner.run)
		if _, err := svc.CommitAll(dir, "   "); err != nil {
			t.Fatalf("空白消息不应报错: %v", err)
		}
		found := false
		for _, c := range runner.calls {
			if strings.HasSuffix(c, "|commit -m NoteVault 一键提交") {
				found = true
			}
		}
		if !found {
			t.Fatalf("应使用默认消息, calls=%v", runner.calls)
		}
	})
}
