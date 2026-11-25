# 发布手册（Release Runbook）

> 适用版本：v3.1.0 起（Wails v3 + Go 1.27 + pnpm 12）
> 最后实战校验：2026-11-26，tag `v3.1.0`
> 配套文件：`.github/workflows/release.yml`、`build/*/Taskfile.yml`、`scripts/render-windows-info.sh`

本文覆盖「打 tag → CI 出三平台产物 → 验收 → 失败重跑 / 救场」的完整链路，并记录了 v3.1.0 首发时踩到的坑。**版本号只有一个来源：git tag**，其余地方的版本都是被它注入或覆写的。

---

## 0. 两条发布路径的区别

| 路径 | 命令 | 能出什么 | 用途 |
| :--- | :--- | :--- | :--- |
| 本地 | `wails3 task release VERSION=vX.Y.Z` | 仅 Windows（exe + NSIS 安装包 + MCP） | 本机快速验证、离线交付 |
| CI | 推送 `v*` tag | Windows / macOS(arm64+amd64) / Linux 全量 14 个资产 | 正式发布，唯一可信来源 |

本地只能出 Windows 包（跨平台 cgo 无法在本机交叉），所以**正式产物一律以 CI 为准**；两条路径的产物命名规则保持一致，可互相替换。

---

## 1. 发布前：本地门禁

在干净工作区上跑完整门禁，任何一项红都不打 tag。

```bash
# 工作区必须干净（未提交改动先提交，详见第 7 节「打 tag 前的拦截」）
git status --short

# 后端
go vet ./...
go test ./internal/... ./cmd/...
golangci-lint run --max-issues-per-linter=0 --max-same-issues=0

# 前端
cd frontend
pnpm test          # vitest，务必看真实用例数，别被 worker 超时吞掉文件
pnpm typecheck
pnpm lint
```

注意两个易误判点：

- `golangci-lint run` 默认 `max-same-issues: 3`，只显示 3 条同类问题，看起来「快修完了」其实还有几十条 → 验收必须带上 `--max-issues-per-linter=0 --max-same-issues=0`。
- `go vet ./...` 在干净 checkout 里会因 `//go:embed all:frontend/dist` 失败（`all:` 连空目录都不收）→ 先 `mkdir -p frontend/dist && printf '' > frontend/dist/index.html`；这正是 CI 里那行占位文件的由来。

### 1.1 先本地验证版本注入（可选，省一次 8 分钟的 CI 往返）

```bash
# 客户端：注入 v0.2.0 后读 exe 版本资源
wails3 task windows:build VERSION=v0.2.0
powershell -c "(Get-Item bin/notevault.exe).VersionInfo.FileVersion"   # 期望 0.2.0

# MCP：发 initialize 报文，看 serverInfo.version
wails3 task mcp:build VERSION=v3.1.0
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"t","version":"1"}}}' \
  | ./bin/notevault-mcp --workspace /tmp/nv-check | grep version
# 期望 "version":"v3.1.0"；不传 VERSION 时应回落到 0.1.0
```

---

## 2. 打 tag 并推送

tag 名即版本号（`vX.Y.Z`，含 `-` 的（如 `v0.2.0-beta.1`）会被自动标记为 prerelease）。

```bash
git tag -a v3.1.0 -m "Release v3.1.0"
git tag --points-at HEAD          # 确认 tag 指向预期提交
```

推送走 **SSH over 443**（本机 HTTPS push 会挂在 git-receive-pack 无响应）：

```bash
export GIT_SSH_COMMAND="ssh -p 443 -o StrictHostKeyChecking=accept-new"
git push ssh://git@ssh.github.com:443/yao00jun/NoteVault.git main
git push ssh://git@ssh.github.com:443/yao00jun/NoteVault.git v3.1.0
```

推送成功后手工修正远端跟踪 ref（这条链路 push 完不会自动更新）：

```bash
git fetch origin 2>/dev/null || true
printf '%s\n' "$(git rev-parse HEAD)" > .git/refs/remotes/origin/main
```

### 🔴 绝对不要 force-update 已存在的同名 tag

`git push -f refs/tags/v3.1.0` 在 GitHub 上**不产生 push 事件**，流水线完全不触发（v3.1.0 第四轮就是这么静默丢的）。需要让已存在 tag 重新触发时，用「删远端 tag → 重推」：

```bash
git push origin :refs/tags/v3.1.0     # 删除
git push origin refs/tags/v3.1.0      # 重建 → create 事件能触发
```

⚠️ 删 tag 有副作用：**关联的 Release 会被自动转成 draft**，而 draft 与新 tag 共存时 `action-gh-release` 会连续 500。删 tag 前先记下 Release id（第 6.3 节给救法）。

---

## 3. 观察流水线

```bash
gh run list --workflow=release.yml --limit 3
gh run watch <run-id>                # 实时跟；Ctrl-C 不影响远端
gh run view <run-id> --log-failed    # 只看失败步骤日志
```

结构：4 个 `Build *` 并行（windows-amd64 / darwin-arm64 / darwin-amd64 / linux-amd64），全部成功后 1 个 `Publish GitHub Release`。

关键耗时参考（v3.1.0 实测）：整体 7~8 分钟；linux job 最慢（AppImage 要联网拉 linuxdeploy）。

---

## 4. 「Verify embedded version」自检在把关什么

每个 build job 在**所有打包任务跑完之后**断言两件事：

1. `grep -qaF "${VERSION}"` 命中客户端二进制 → 客户端 `-ldflags -X .../core.AppVersion` 真的注入了；
2. 启动 MCP 二进制发 `initialize`，`serverInfo.version == tag` → MCP 不是硬编码版本。

它兜住两类「构建成功但版本错」：

- Linux 的 `create:{deb,rpm,aur,appimage}` 都 `deps: build`，会**重跑构建并覆盖 `bin/notevault`**；漏传 `VERSION` 就退回默认 0.1.0（v3.1.0 首发真实踩到，5 个 Linux 产物全错版本）。
- `internal/mcp/server.go` 的 `ServerVersion` 曾是硬编码 `"1.0.0"`，与 tag 无关（同轮踩到）。

所以**新增任何带 `deps: build` 的打包任务，必须显式传 `VERSION=`**，否则自检会红——这是设计意图，不是误报。

---

## 5. 发布后验收清单

```bash
# 1) Release 状态与资产数量（期望 isDraft=false、isPrerelease=false、14 个资产）
gh release view v3.1.0 --json tagName,isDraft,isPrerelease,assets \
  --jq '{tag:.tagName,draft:.isDraft,pre:.isPrerelease,n:(.assets|length)}'

# 2) 抽检可下载性（期望 206）
curl -sL -r 0-1023 -o /dev/null -w '%{http_code}\n' \
  https://github.com/yao00jun/NoteVault/releases/download/v3.1.0/notevault-v3.1.0-linux-amd64.tar.gz

# 3) 产物内二进制版本（tar.gz 里两个二进制都要命中）
tar -xzf notevault-v3.1.0-linux-amd64.tar.gz && grep -aF 'v3.1.0' notevault notevault-mcp
```

14 个资产分布：

| 平台 | 资产 |
| :--- | :--- |
| Windows | `notevault-<ver>-windows-amd64.exe`、`-installer.exe`、`notevault-mcp-<ver>-windows-amd64.exe` |
| macOS arm64 | 裸二进制、`.dmg`、`notevault-mcp-*` |
| macOS amd64 | 裸二进制、`.dmg`、`notevault-mcp-*` |
| Linux amd64 | `.tar.gz`（含两个二进制）、`.deb`、`.rpm`、`.pkg.tar.zst`（AUR）、`.AppImage` |

发布说明由提交历史自动生成；需要润色时 `gh release edit v3.1.0 --notes-file notes.md`。

**Linux 产物要求目标机 glibc ≥ 2.39**（构建机钉 ubuntu-24.04，Wails v3 的 Linux 后端是 GTK4 + WebKitGTK 6.0）。下载页/Release 说明里应写明，22.04 及更早跑不起来。

---

## 6. 失败处置与重跑

### 6.1 先分清是「代码问题」还是「平台问题」

```bash
gh run view <run-id>            # 哪个 job 红
gh run view <run-id> --log-failed
curl -s https://www.githubstatus.com/api/v2/summary.json | head -c 600   # Actions/Webhooks 是否 degraded
```

v3.1.0 的典型场景：**4 个 build job 全绿，只有 `Publish GitHub Release` 红**，且 `gh run rerun --failed` 返回 500 → 这是 GitHub Releases 写接口故障，不是产物问题。此时不要重打 tag、不要改代码。

### 6.2 重跑（首选，最轻）

```bash
gh run rerun <run-id> --failed        # 只重跑失败 job；成功 job 与产物复用
gh run rerun <run-id>                 # 整条重跑（会重新构建+重新上传资产）
gh run watch <run-id>
```

build job 全绿、只有 Publish 红时，用 `--failed` 即可，几分钟内完成，不会重新编译。

如果 `rerun` 接口也 5xx（v3.1.0 当时连试 4 次都 500），两条退路：

```bash
# 退路 A：手动触发，指定已存在的 tag（走 workflow_dispatch 分支）
gh workflow run release.yml -f tag=v3.1.0

# 退路 B：直接调 API（gh 子命令 500 时 REST 有时仍可用）
gh api -X POST repos/yao00jun/NoteVault/actions/runs/<run-id>/rerun -f enable_debug_logging=false
```

`workflow_dispatch` 分支的 `VERSION` 取自 `inputs.tag`，与 push 分支行为一致（产物命名、注入、Release 标题全部对齐）。

### 6.3 Release 变成 draft 了怎么救

删过远端 tag 就会遇到。**不要删了重建 Release**（新 tag 与 draft 并存时创建会连续 500），直接把 draft 发布出去：

```bash
gh api repos/yao00jun/NoteVault/releases --jq '.[] | {id,tag_name,draft}'
gh api -X PATCH repos/yao00jun/NoteVault/releases/<id> -f draft=false
```

`gh release edit --draft=false` 在平台故障期同样会 500，但上面的 PATCH 当时是通的。

### 6.4 资产已经发布、只想补流程绿灯

如果产物验证无误（第 5 节全过），只是流水线状态红：**先确认资产版本正确，再决定是否重跑**。整条重跑会用 tag 处代码重建并覆盖同名资产——应用代码一致时安全，但会多花 8 分钟。

---

## 7. v3.1.0 实战记录（复盘）

时间线（2026-11-25 23:06 → 次日 00:10，北京时间）：

| 时间 | 事件 |
| :--- | :--- |
| 23:06 | 工作区有未提交的侧栏重构（147 行）→ 先提交再打 tag；提交前发现组件引用了 `sidebar.sub.*` 但中英文 i18n 键都不存在，补 8 个键 + 加「标签不得是原始键名」回归锁 |
| 23:10 | 首轮触发（run 45642203717）成功，但发现 MCP 自报 1.0.0、Linux 5 个产物退回 0.1.0 |
| 23:28 | 修复后重推（run 45645578638）成功，产物全部正确 |
| 23:37 | 新增版本自检，重推后 macOS 挂 `VERSION（: unbound variable` |
| 23:51 | 修 bash 3.2 兼容，重推；4 个 build 全绿，Publish 因平台 500 红 |
| 00:15 | `gh release edit` 恢复（说明已润色）、资产 206 可下载，但 `gh run rerun --failed` 仍 500 |
| 次日 01:15 | 平台恢复后 `gh run rerun 45649280901 --failed` 首次即成功：**只重跑了 Publish job**，4 个 build job 直接复用（未重新编译），约 2 分钟收尾 |

最终状态：run `45649280901` 五个 job 全绿；Release `v3.1.0` 非 draft、非 prerelease、14 个资产（`updatedAt` 已刷新为本次重跑时间），抽检 tar.gz / exe / dmg 均 206。

> 这次重跑也验证了第 6.2 节的判断：`--failed` 不会重建产物，只补失败的发布步骤，是「产物已验证正确、只缺流程绿灯」时的最优解。

四个坑与对应规则：

1. **force-update tag 不触发流水线** → 要重触发就删远端 tag 再推（并接受 Release 转 draft 的副作用）。
2. **构建成功 ≠ 版本正确** → 任何 `deps: build` 的任务都要传 `VERSION`；发布自检必须放在全部打包之后。
3. **macOS bash 3.2 + C locale** → CI 脚本里变量一律写 `${VAR}`；紧跟中文/全角符号时尤其致命（`$VERSION（` 会把「（」并进变量名）。
4. **`set -e` 下 `[ -f x ] && var=y` 会自杀** → 条件赋值一律写 `if ...; then ...; fi`。

另外两条与发布无关但同样会拦住 tag 的检查：`build/windows/info.json` 的版本段必须是 `"0409"` 且含 `FileVersion`，否则 `generate syso` **静默**丢版本资源（exe 属性一片空白）；发布时用 `scripts/render-windows-info.sh` 渲染临时副本，别改受控文件。

---

## 8. 命令速查

```bash
# 发布
git tag -a vX.Y.Z -m "Release vX.Y.Z" && git push ssh://git@ssh.github.com:443/yao00jun/NoteVault.git vX.Y.Z
gh run list --workflow=release.yml --limit 3

# 验收
gh release view vX.Y.Z --json tagName,isDraft,isPrerelease,assets --jq '{tag:.tagName,draft:.isDraft,n:(.assets|length)}'

# 重跑 / 救场
gh run rerun <run-id> --failed
gh workflow run release.yml -f tag=vX.Y.Z
gh api -X PATCH repos/yao00jun/NoteVault/releases/<id> -f draft=false
gh release edit vX.Y.Z --notes-file notes.md
```
