package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// GitService —— P2-4「Git 友好」。
//
// 战略定位（见 docs/2026-optimization-todo.md P2-4）：NoteVault **不做同步**，
// 只帮用户把工作区接上他们已有的工具链（Git / Syncthing / 网盘）。
// 本服务仅提供三件事：git init、生成 .gitignore、一键提交。
// 冲突合并、增量传输、远端管理一律交给用户自己的 Git 工具。
//
// 实现说明：
//   - 直接 exec 系统 git，不引第三方库（go-git 体量大且我们只用基础命令）；
//   - 所有命令经 GitRunner 注入，单测用 fake runner，不依赖真 git；
//   - 不经 shell 拼接，参数全部独立传递，提交信息注入无风险。
// ============================================================================

// GitStatus 是工作区的 Git 概况，供前端决定展示「初始化」还是「提交」。
type GitStatus struct {
	Installed bool   `json:"installed"` // 系统里是否有可用的 git
	IsRepo    bool   `json:"isRepo"`    // 工作区是否已是 git 仓库
	Branch    string `json:"branch"`    // 当前分支（非仓库时为空）
	Changed   int    `json:"changed"`   // 已跟踪文件的变更数（含暂存）
	Untracked int    `json:"untracked"` // 未跟踪文件数
}

// GitRunner 在 dir 目录执行一条 git 命令，返回合并输出与错误。
type GitRunner func(ctx context.Context, dir string, args ...string) (string, error)

func defaultGitRunner(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GitService 提供 git init / .gitignore / 一键提交。
type GitService struct {
	run GitRunner
}

func NewGitService() *GitService {
	return &GitService{run: defaultGitRunner}
}

// NewGitServiceWithRunner 注入自定义 runner（测试用）。
func NewGitServiceWithRunner(run GitRunner) *GitService {
	return &GitService{run: run}
}

// gitignoreContent 是 EnsureGitignore 写入的默认内容。
// .notevault/ 是 NoteVault 的工作区元数据目录（索引缓存、快照等），
// 属于应用私有状态，不应进入用户的笔记仓库。
const gitignoreContent = `# NoteVault 工作区元数据与缓存（应用私有，不入库）
.notevault/

# 系统文件
.DS_Store
Thumbs.db
desktop.ini

# 编辑器临时文件
*.swp
*~
`

// Status 返回工作区 Git 概况。任何一步失败都降级为「能力缺失」而非报错：
// 前端拿到 false/0 就能渲染出对应的引导 UI，不需要处理错误分支。
func (s *GitService) Status(workspacePath string) (*GitStatus, error) {
	st := &GitStatus{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// git 是否可用（--version 与目录无关，失败即视为未安装）
	if _, err := s.run(ctx, workspacePath, "--version"); err != nil {
		return st, nil
	}
	st.Installed = true

	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err != nil {
		return st, nil
	}
	st.IsRepo = true

	// --porcelain --branch：首行是分支信息，其余每行一个变更条目
	out, err := s.run(ctx, workspacePath, "status", "--porcelain", "--branch")
	if err != nil {
		// 仓库存在但 status 失败（如损坏）：按"是仓库但读不到状态"处理
		return st, nil
	}
	for i, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i == 0 && strings.HasPrefix(line, "##") {
			// 形如 "## main...origin/main [ahead 1]" / "## No commits yet on main" / "## HEAD (no branch)"
			branch := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			switch {
			case strings.HasPrefix(branch, "No commits yet on "):
				st.Branch = strings.TrimPrefix(branch, "No commits yet on ")
			case strings.Contains(branch, "..."):
				// 本地分支...远端跟踪分支
				st.Branch = branch[:strings.Index(branch, "...")]
			case strings.Contains(branch, " ["):
				st.Branch = branch[:strings.Index(branch, " [")]
			case strings.Contains(branch, " ("):
				// detached HEAD
				st.Branch = branch[:strings.Index(branch, " (")]
			default:
				st.Branch = branch
			}
			continue
		}
		if strings.HasPrefix(line, "??") {
			st.Untracked++
		} else {
			st.Changed++
		}
	}
	return st, nil
}

// InitRepo 把工作区初始化为 git 仓库（幂等：已是仓库时不动），并顺手补 .gitignore。
func (s *GitService) InitRepo(workspacePath string) error {
	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err == nil {
		_, err := s.ensureGitignore(workspacePath)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := s.run(ctx, workspacePath, "init"); err != nil {
		return fmt.Errorf("git init 失败: %w\n%s", err, out)
	}
	_, err := s.ensureGitignore(workspacePath)
	return err
}

// EnsureGitignore 在工作区生成默认 .gitignore。
// 已存在时不覆盖（用户可能有自己的规则），返回是否新建。
func (s *GitService) EnsureGitignore(workspacePath string) (bool, error) {
	return s.ensureGitignore(workspacePath)
}

func (s *GitService) ensureGitignore(workspacePath string) (bool, error) {
	target := filepath.Join(workspacePath, ".gitignore")
	if _, err := os.Stat(target); err == nil {
		return false, nil
	}
	if err := os.WriteFile(target, []byte(gitignoreContent), 0o644); err != nil {
		return false, fmt.Errorf("写入 .gitignore 失败: %w", err)
	}
	return true, nil
}

// CommitAll 把全部变更（含未跟踪文件）提交为一个 commit，返回给用户的提示文案。
// 没有可提交内容时返回 ("没有可提交的变更", nil)——这是正常路径，不是错误。
func (s *GitService) CommitAll(workspacePath string, message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		message = "NoteVault 一键提交"
	}
	if info, err := os.Stat(workspacePath); err != nil || !info.IsDir() {
		return "", fmt.Errorf("工作区目录不存在: %s", workspacePath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err != nil {
		return "", fmt.Errorf("工作区还不是 git 仓库，请先初始化")
	}

	if out, err := s.run(ctx, workspacePath, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add 失败: %w\n%s", err, out)
	}

	// --cached：只看暂存区，工作区里未跟踪以外的噪音不影响判断
	out, err := s.run(ctx, workspacePath, "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("检查暂存区失败: %w\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return "没有可提交的变更", nil
	}

	commitOut, err := s.run(ctx, workspacePath, "commit", "-m", message)
	if err != nil {
		// 最常见的是没配 user.name/user.email，给用户可执行的指引
		if strings.Contains(commitOut, "user.name") || strings.Contains(commitOut, "user.email") {
			return "", fmt.Errorf("Git 身份未配置。请先执行：\n  git config --global user.name \"你的名字\"\n  git config --global user.email \"you@example.com\"")
		}
		return "", fmt.Errorf("git commit 失败: %w\n%s", err, commitOut)
	}
	return "提交成功", nil
}
