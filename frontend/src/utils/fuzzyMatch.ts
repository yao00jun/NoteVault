/**
 * 轻量模糊匹配打分（命令面板用）：query 逐字符按序匹配 text（子序列）。
 * 返回得分（越大越好）或 null（不匹配）。
 * 评分：完全子串 > 前缀命中 > 普通子序列；位置越靠前、连续命中越多分越高。
 */
export function fuzzyScore(query: string, text: string): number | null {
  const q = query.toLowerCase().trim()
  const t = text.toLowerCase()
  if (!q) return null

  // 完全子串命中：位置越靠前分越高
  const idx = t.indexOf(q)
  if (idx >= 0) {
    return 1000 - idx * 10 + (idx === 0 ? 200 : 0)
  }

  // 子序列匹配：逐字符按序查找，连续命中额外加分
  let score = 0
  let searchFrom = 0
  let lastHit = -1
  for (const qc of q) {
    let found = -1
    for (let i = searchFrom; i < t.length; i++) {
      if (t[i] === qc) {
        found = i
        break
      }
    }
    if (found < 0) return null
    if (found === lastHit + 1) score += 25
    score += 10
    lastHit = found
    searchFrom = found + 1
  }
  return score
}
