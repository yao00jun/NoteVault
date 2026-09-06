/**
 * useWorkbenchSpaces - 工作台四大知识空间的分类扫描、统计与过滤
 *
 * 蓝图 2.1：按约定目录（Learning/Projects/Inbox/Daily）聚合文档，
 * 提供目录计数与 i18n 文案。扫描纯前端派生（输入是已扁平化的文件列表），
 * 不发起任何后端调用；列表为空时各空间计数自然为 0，无需降级分支。
 */
import { computed, type ComputedRef, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { BookOpen, Rocket, Inbox, Calendar } from '@lucide/vue'

export interface FlatFileEntry {
  path: string
  name: string
  fullPath?: string
  depth?: number
  isDir: boolean
  size?: number
  modTime?: string
}

// 四大空间的约定目录（与工作区种子结构一致；命名经用户工作流验证，勿随意改）
export const SPACE_DEFS = [
  { key: 'learning', dir: 'Learning', icon: BookOpen },
  { key: 'projects', dir: 'Projects', icon: Rocket },
  { key: 'inbox', dir: 'Inbox', icon: Inbox },
  { key: 'daily', dir: 'Daily', icon: Calendar },
] as const

export type SpaceKey = (typeof SPACE_DEFS)[number]['key']

export interface KnowledgeSpace {
  key: SpaceKey
  dir: string
  icon: typeof BookOpen
  label: string
  desc: string
  count: number
}

/** 统一 Windows/POSIX 路径分隔符为 '/' */
export function normalizePath(path: string): string {
  return path.replaceAll('\\', '/')
}

export function isMarkdownFile(file: { name: string; isDir: boolean }): boolean {
  return !file.isDir && /\.(md|markdown)$/i.test(file.name)
}

/**
 * @param flatFiles 已扁平化的文件列表（含目录与文件）
 */
export function useWorkbenchSpaces(
  flatFiles: Ref<FlatFileEntry[]> | ComputedRef<FlatFileEntry[]>,
): { knowledgeSpaces: ComputedRef<KnowledgeSpace[]> } {
  const { t } = useI18n()

  const knowledgeSpaces = computed<KnowledgeSpace[]>(() =>
    SPACE_DEFS.map((def) => ({
      ...def,
      label: t(`knowledge.workbench.spaces.${def.key}.name`),
      desc: t(`knowledge.workbench.spaces.${def.key}.desc`),
      count: flatFiles.value.filter(
        (f) => isMarkdownFile(f) && normalizePath(f.path).startsWith(`${def.dir}/`),
      ).length,
    })),
  )

  return { knowledgeSpaces }
}
