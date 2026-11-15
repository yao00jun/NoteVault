import { normalizeNotePath } from './navigation'

/**
 * 功能托管文件（学习/项目闭环自动创建与读写的工作簿）。
 * 编辑器文件树默认隐藏：它们的查看/编辑入口在对应功能页——
 * 分册档案/来源记录/AI 学习计划 → 学习页书架；项目档案/任务清单 → 项目页。
 * 磁盘文件不受影响（学习/蒸馏/项目页仍按路径读写）。
 */
const META_FILE_PATTERNS: RegExp[] = [
  /^Learning\/[^/]+\/book\.md$/i, // 分册档案
  /^Learning\/[^/]+\/sources\.md$/i, // 来源记录
  /^Learning\/[^/]+\/ai-plan\.md$/i, // AI 学习计划
  /^Projects\/[^/]+\/project\.md$/i, // 项目档案
  /^Projects\/[^/]+\/tasks\.md$/i, // 任务清单
]

export function isMetaFile(path: string): boolean {
  const norm = normalizeNotePath(path)
  return META_FILE_PATTERNS.some((pattern) => pattern.test(norm))
}
