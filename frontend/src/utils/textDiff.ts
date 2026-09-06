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
