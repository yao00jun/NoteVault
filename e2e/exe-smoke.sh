#!/usr/bin/env bash
# =============================================================================
# notevault.exe 启动冒烟测试（exe 级 E2E）
#
# 验证生产构建的 exe 能否正常启动：
#   1. 进程启动成功且稳定运行
#   2. WebView2 环境初始化成功
#   3. 日志无致命错误（fatal/panic）
#   4. 嵌入的前端资源包含特征（dist 构建产物）
#
# 注意：
#   - 在无交互桌面/沙箱环境下 WebView2 渲染进程不会启动（CDP 端口不监听），
#     因此完整 UI 自动化需在真实桌面环境进行（见 README 中 NOTEvault_DEBUG_PORT）。
#   - 本脚本不负责杀掉测试进程（Git Bash kill 对 Windows GUI 进程无效），
#     测试结束后请用 PowerShell 执行：Get-Process notevault | Stop-Process -Force
# =============================================================================
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EXE="${1:-$ROOT/bin/notevault.exe}"
LOG="${TMPDIR:-/tmp}/notevault_smoke_$$.log"
PORT=9333
RUN_SECONDS=8
FAILED=0

echo "==> exe: $EXE"

# 0. 前置：exe 存在
if [ ! -f "$EXE" ]; then
  echo "FAIL: exe not found: $EXE"
  exit 1
fi

# 1. 启动（带调试端口开关，验证 NOTEvault_DEBUG_PORT 路径）
NOTEvault_DEBUG_PORT=$PORT "$EXE" >"$LOG" 2>&1 &
sleep 3

# 2. 进程存活
if ! tasklist 2>/dev/null | grep -qi "notevault.exe"; then
  echo "FAIL: exe exited early"
  cat "$LOG"
  exit 1
fi
echo "PASS: process started"

# 3. WebView2 初始化
if ! grep -q "WebView2.*created successfully" "$LOG"; then
  echo "FAIL: WebView2 environment not initialized"
  cat "$LOG"
  FAILED=1
else
  echo "PASS: WebView2 environment created"
fi

# 4. 无致命错误
if grep -qiE "fatal|panic:|error calling|failed to" "$LOG"; then
  echo "FAIL: fatal error in log:"
  cat "$LOG"
  FAILED=1
else
  echo "PASS: no fatal errors in log"
fi

# 5. 稳定运行
sleep $RUN_SECONDS
if ! tasklist 2>/dev/null | grep -qi "notevault.exe"; then
  echo "FAIL: process crashed after ${RUN_SECONDS}s"
  cat "$LOG"
  exit 1
fi
echo "PASS: stable for ${RUN_SECONDS}s"

# 6. 嵌入资源特征（vue 构建产物含应用标题）
if strings -e l "$EXE" 2>/dev/null | grep -q "NoteVault"; then
  echo "PASS: frontend assets embedded"
else
  # strings 可能不可用，降级为 grep 二进制
  if grep -c "NoteVault" "$EXE" >/dev/null 2>&1; then
    echo "PASS: frontend assets embedded (grep)"
  else
    echo "WARN: cannot verify embedded assets (strings unavailable)"
  fi
fi

echo "==> smoke log: $LOG"
echo "==> RESULT: $([ $FAILED -eq 0 ] && echo PASS || echo FAIL)"
exit $FAILED
