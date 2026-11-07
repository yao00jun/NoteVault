#!/usr/bin/env bash
# =============================================================================
# NoteVault 发布脚本（由 `wails3 task release VERSION=vX.Y.Z` 调用）
#
# 前置：Taskfile 已完成 windows:build，bin/notevault.exe 为最新产物
# 流程（任一步失败即中止）：
#   1. 校验版本号格式（vX.Y.Z[-预发布后缀]）
#   2. 校验 git 工作区干净、本地与远端 tag 不存在
#   3. exe 启动冒烟（复用 e2e/exe-smoke.sh）
#   4. 推送当前分支，Release tag 锚定推送后的 commit（保证 exe 与源码一致）
#   5. 生成发布说明（自上个 tag 以来的提交）并 gh release create
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

EXE="bin/notevault.exe"
VERSION="${1:-}"

cleanup() {
  powershell -NoProfile -Command "Get-Process notevault -ErrorAction SilentlyContinue | Stop-Process -Force" >/dev/null 2>&1 || true
}
trap cleanup EXIT

die() { echo "FAIL: $*" >&2; exit 1; }

# 0. 工具与产物
command -v gh >/dev/null 2>&1 || die "需要 GitHub CLI（gh），请先安装并 gh auth login"
[ -f "$EXE" ] || die "未找到 $EXE，请通过 wails3 task release 重新执行（含构建）"

# 1. 版本号格式：v主.次.修订，可带 -预发布 后缀
echo "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' \
  || die "版本号 \"$VERSION\" 不合法，应为 vX.Y.Z（如 v0.2.0）"

# 2. git 状态：工作区必须干净（含未跟踪文件），tag 本地/远端均不存在
[ -z "$(git status --porcelain)" ] || die "工作区有未提交改动，请先提交后再发布"
git rev-parse -q --verify "refs/tags/$VERSION" >/dev/null \
  && die "本地已存在 tag $VERSION"
[ -n "$(git ls-remote --tags origin "refs/tags/$VERSION")" ] \
  && die "远端已存在 tag $VERSION"

# 3. exe 冒烟：验证生产产物可正常启动（失败即中止发布）
echo "==> 冒烟测试：$EXE"
bash e2e/exe-smoke.sh
cleanup

# 4. 推送并锚定 commit：tag、源码、exe 三者一致
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
echo "==> 推送 $BRANCH 到 origin"
git push origin "$BRANCH"
HEAD_SHA="$(git rev-parse HEAD)"

# 5. 发布说明：自上个 tag 以来的提交；首个版本回退为最近提交列表
PREV_TAG="$(git describe --tags --abbrev=0 "$HEAD_SHA" 2>/dev/null || true)"
NOTES="$(mktemp "${TMPDIR:-/tmp}/notevault_release_XXXXXX.md")"
{
  echo "## NoteVault $VERSION"
  echo
  echo "本地优先的个人知识库：纯 Markdown 存储，双向链接、知识图谱、混合检索（全文 + 语义）、AI 总结 / RAG 问答、待办提醒、Canvas 白板、插件系统。"
  echo
  echo "### 📦 安装"
  echo
  echo "1. 下载 \`notevault.exe\`（绿色单文件，无需安装）"
  echo "2. Windows 10/11 自带 WebView2，双击即可运行"
  echo "3. 首次启动选择笔记目录作为工作区"
  echo
  echo "### 📝 本版变更"
  echo
  if [ -n "$PREV_TAG" ]; then
    git log --oneline "$PREV_TAG..$HEAD_SHA" | sed 's/^/- /'
  else
    echo "> 首个公开版本，完整功能清单见 [README](https://github.com/yao00jun/NoteVault#readme)。"
    git log --oneline -15 | sed 's/^/- /'
  fi
  echo
  echo "### 🔐 校验"
  echo
  echo '```'
  echo "SHA256: $(sha256sum "$EXE" | awk '{print $1}')"
  echo '```'
} > "$NOTES"

echo "==> 创建 Release $VERSION（target: ${HEAD_SHA:0:7}）"
gh release create "$VERSION" "$EXE" \
  --target "$HEAD_SHA" \
  --title "NoteVault $VERSION" \
  --notes-file "$NOTES"

rm -f "$NOTES"
echo "==> 发布完成：$VERSION"
