// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { isLocalBaseURL } from './localEndpoint'

describe('isLocalBaseURL（免 Key 守卫的本机判定）', () => {
  it('识别常见本机地址写法', () => {
    expect(isLocalBaseURL('http://localhost:11434/v1')).toBe(true)
    expect(isLocalBaseURL('http://127.0.0.1:11434/v1')).toBe(true)
    expect(isLocalBaseURL('http://127.10.0.3:8080')).toBe(true)
    expect(isLocalBaseURL('http://[::1]:11434/v1')).toBe(true)
    expect(isLocalBaseURL('http://0.0.0.0:11434')).toBe(true)
    expect(isLocalBaseURL('https://api.localhost/v1')).toBe(true)
  })

  it('云端地址不误判', () => {
    expect(isLocalBaseURL('https://api.openai.com/v1')).toBe(false)
    expect(isLocalBaseURL('https://api.siliconflow.cn/v1')).toBe(false)
    expect(isLocalBaseURL('http://192.168.1.10:11434/v1')).toBe(false)
  })

  it('空值与非法输入安全返回 false', () => {
    expect(isLocalBaseURL('')).toBe(false)
    expect(isLocalBaseURL(null)).toBe(false)
    expect(isLocalBaseURL(undefined)).toBe(false)
    expect(isLocalBaseURL('not a url')).toBe(false)
  })
})
