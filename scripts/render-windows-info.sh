#!/usr/bin/env bash
# 把 build/windows/info.json 里的版本字段替换为指定版本，输出到 stdout。
#
# 由 `wails3 task windows:generate:syso VERSION=vX.Y.Z` 调用：syso 生成器只认
# 磁盘上的 info.json，直接改会弄脏受版本控制的文件，所以渲染一份临时副本。
# Windows 的 file_version 不接受前导 v，这里统一去掉。
set -euo pipefail

VER="${1:-}"
if [ -z "$VER" ]; then
  echo "usage: $0 vX.Y.Z" >&2
  exit 2
fi
VER="${VER#v}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

sed -E 's/("(file_version|product_version|FileVersion|ProductVersion)":[[:space:]]*")[^"]*/\1'"${VER}"'/g' \
  "${ROOT_DIR}/build/windows/info.json"
