/**
 * 轻量行级 Diff（LCS）——冲突横幅「查看对比」用。
 * 规模：冲突场景下是单文件草稿 vs 磁盘，行数量级几百行，O(n*m) 完全够用。
 */
export interface DiffRow {
  type: 'same' | 'removed' | 'added'
  text: string
}

export function diffLines(oldText: string, newText: string): DiffRow[] {
  const a = oldText.split('\n')
  const b = newText.split('\n')
  const n = a.length
  const m = b.length
  // LCS 长度表
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const rows: DiffRow[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      rows.push({ type: 'same', text: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      rows.push({ type: 'removed', text: a[i++] })
    } else {
      rows.push({ type: 'added', text: b[j++] })
    }
  }
  while (i < n) rows.push({ type: 'removed', text: a[i++] })
  while (j < m) rows.push({ type: 'added', text: b[j++] })
  return rows
}

/**
 * 把统一 diff 行转为左右分栏（Beyond Compare 式）的对齐行对：
 *  - same → 左右同文；
 *  - removed 与 added 按顺序就近配对（删改对照）；
 *  - 配不上的单侧行，对侧补 null（渲染为空行占位，保持行号对齐）。
 */
export interface SplitPair {
  left: DiffRow | null
  right: DiffRow | null
}

export function toSplitPairs(rows: DiffRow[]): SplitPair[] {
  const pairs: SplitPair[] = []
  let k = 0
  while (k < rows.length) {
    const row = rows[k]!
    if (row.type === 'same') {
      pairs.push({ left: row, right: row })
      k += 1
      continue
    }
    // 收集连续的 removed / added 块，逐行配对
    const removed: DiffRow[] = []
    const added: DiffRow[] = []
    while (k < rows.length && rows[k]!.type !== 'same') {
      ;(rows[k]!.type === 'removed' ? removed : added).push(rows[k]!)
      k += 1
    }
    const len = Math.max(removed.length, added.length)
    for (let p = 0; p < len; p += 1) {
      pairs.push({ left: removed[p] ?? null, right: added[p] ?? null })
    }
  }
  return pairs
}
