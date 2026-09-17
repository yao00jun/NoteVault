// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { defineComponent } from 'vue'
import { ReminderService, NotificationsService } from '@/api'
import { useSettingsStore } from '@/stores/settings'

// 与既有测试同口径：settings store 初始化会碰这两个副依赖
vi.mock('@/api', () => ({
  ReminderService: { GetAllReminders: vi.fn() },
  NotificationsService: { SendNotification: vi.fn(async () => undefined) },
  CredentialService: {
    GetCredential: vi.fn().mockResolvedValue(''),
    SaveCredential: vi.fn().mockResolvedValue(undefined),
  },
  ClipperService: { ConfigureAI: vi.fn(async () => undefined) },
}))

import { useReminderNotifications } from './useReminderNotifications'

const remindersMock = vi.mocked(ReminderService.GetAllReminders)
const sendMock = vi.mocked(NotificationsService.SendNotification)

enableAutoUnmount(afterEach)

/** 在真实 setup 上下文里挂载 composable（内部用到 onMounted / store） */
function mountComposable(workspacePath = 'C:/vault') {
  const pinia = createPinia()
  setActivePinia(pinia)
  let api!: ReturnType<typeof useReminderNotifications>
  const Host = defineComponent({
    setup() {
      api = useReminderNotifications(() => workspacePath)
      return () => null
    },
  })
  mount(Host, { global: { plugins: [pinia] } })
  return { api, settings: useSettingsStore() }
}

/** 构造一条「刚刚到点」的提醒 */
function dueReminder(overrides: Record<string, unknown> = {}) {
  return {
    id: 'r1',
    content: '该写周报了',
    remindAt: new Date(Date.now() - 60_000).toISOString(),
    completed: false,
    filePath: 'Projects/周报.md',
    ...overrides,
  }
}

describe('useReminderNotifications', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sendMock.mockResolvedValue(undefined)
    remindersMock.mockResolvedValue([dueReminder()] as never)
  })

  it('到点提醒走 Wails 原生通知通道，并带上文件路径供点击跳转', async () => {
    const { api } = mountComposable()
    await api.checkReminders()
    await flushPromises()

    expect(sendMock).toHaveBeenCalledTimes(1)
    expect(sendMock.mock.calls[0]![0]).toMatchObject({
      id: 'reminder-r1',
      title: expect.stringContaining('NoteVault'),
      body: '该写周报了',
      data: { filePath: 'Projects/周报.md' },
    })
  })

  it('同一条提醒只通知一次（去重）', async () => {
    const { api } = mountComposable()
    await api.checkReminders()
    await api.checkReminders()
    await flushPromises()

    expect(sendMock).toHaveBeenCalledTimes(1)
  })

  it('原生通道失败时回退浏览器通知，不吞掉提醒', async () => {
    sendMock.mockRejectedValue(new Error('toast unavailable'))
    const browserSpy = vi.fn()
    vi.stubGlobal('Notification', function NotificationMock(this: unknown, title: string, opts: unknown) {
      browserSpy(title, opts)
    } as unknown as typeof Notification)
    Object.defineProperty(window.Notification, 'permission', { value: 'granted', configurable: true })

    const { api } = mountComposable()
    await api.checkReminders()
    await flushPromises()

    expect(sendMock).toHaveBeenCalledTimes(1)
    expect(browserSpy).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('勿扰时段内静默跳过，且不计入已通知（时段结束后仍可补发）', async () => {
    const { api, settings } = mountComposable()
    settings.settings.reminder.doNotDisturb = { enabled: true, start: '00:00', end: '23:59' }
    await api.checkReminders()
    await flushPromises()
    expect(sendMock).not.toHaveBeenCalled()
  })

  it('已完成的提醒不再打扰', async () => {
    remindersMock.mockResolvedValue([dueReminder({ completed: true })] as never)
    const { api } = mountComposable()
    await api.checkReminders()
    await flushPromises()
    expect(sendMock).not.toHaveBeenCalled()
  })

  it('工作区未就绪时不做任何请求', async () => {
    const { api } = mountComposable('')
    await api.checkReminders()
    await flushPromises()
    expect(remindersMock).not.toHaveBeenCalled()
  })
})
