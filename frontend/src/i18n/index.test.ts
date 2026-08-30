import { describe, it, expect } from 'vitest'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'

// 递归提取所有叶子键路径
function flattenKeys(obj: Record<string, unknown>, prefix = ''): string[] {
  const keys: string[] = []
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      keys.push(...flattenKeys(value as Record<string, unknown>, path))
    } else {
      keys.push(path)
    }
  }
  return keys.sort()
}

describe('i18n locale 资源', () => {
  it('zh-CN 与 en-US 键结构完全一致', () => {
    const zhKeys = flattenKeys(zhCN)
    const enKeys = flattenKeys(enUS)
    expect(zhKeys).toEqual(enKeys)
  })

  it('zh-CN 与 en-US 键数量一致且非空', () => {
    const zhKeys = flattenKeys(zhCN)
    const enKeys = flattenKeys(enUS)
    expect(zhKeys.length).toBeGreaterThan(300)
    expect(zhKeys.length).toBe(enKeys.length)
  })

  it('两个 locale 均包含 qna 段', () => {
    expect(zhCN.qna).toBeDefined()
    expect(enUS.qna).toBeDefined()
    expect(zhCN.qna.title).toBeTruthy()
    expect(enUS.qna.title).toBeTruthy()
  })
})
