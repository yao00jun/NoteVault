import { describe, expect, it } from 'vitest'
import { conversationContext } from './conversation'

describe('conversation context', () => {
  it('keeps prior roles and answers for a follow-up without replaying failures', () => {
    const result = conversationContext([{ role: 'user', content: '解释乐观锁' }, { role: 'assistant', content: '通过版本号检查并发修改' }, { role: 'assistant', content: '网络失败', error: true }])
    expect(result).toContain('user')
    expect(result).toContain('assistant')
    expect(result).toContain('版本号')
    expect(result).not.toContain('网络失败')
  })
  it('bounds history while retaining the most recent conversation', () => {
    const result = conversationContext(Array.from({ length: 40 }, (_, i) => ({ role: 'user' as const, content: `${i}: ${'x'.repeat(3000)}` })))
    expect(result.length).toBeLessThanOrEqual(16000)
    expect(result).toContain('39:')
    expect(result).not.toContain('0:')
  })
})
