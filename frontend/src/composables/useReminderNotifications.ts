import { ref, onMounted, onBeforeUnmount } from 'vue'
import { ReminderService } from '@bindings/github.com/notevault/notevault/index.js'

interface Reminder {
  id: string
  content: string
  remindAt: string
  completed: boolean
}

/**
 * 提醒通知 composable
 * 定时检查提醒，到时间时弹出系统通知
 */
export function useReminderNotifications(workspacePath: () => string | undefined) {
  const notifiedIds = ref<Set<string>>(new Set())
  let checkInterval: ReturnType<typeof setInterval> | null = null

  // 请求通知权限
  async function requestNotificationPermission() {
    if ('Notification' in window && Notification.permission === 'default') {
      try {
        await Notification.requestPermission()
      } catch (e) {
        console.warn('Notification permission request failed:', e)
      }
    }
  }

  // 显示系统通知
  function showNotification(title: string, body: string) {
    if ('Notification' in window && Notification.permission === 'granted') {
      try {
        const notification = new Notification(title, {
          body,
          icon: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><text y=".9em" font-size="90">🔔</text></svg>',
        })
        notification.onclick = () => {
          window.focus()
          notification.close()
        }
        // 5 秒后自动关闭
        setTimeout(() => notification.close(), 5000)
      } catch (e) {
        console.warn('Failed to show notification:', e)
      }
    }
  }

  // 检查提醒
  async function checkReminders() {
    const path = workspacePath()
    if (!path) return

    try {
      const reminders = await ReminderService.GetAllReminders(path) as Reminder[]
      const now = new Date()

      for (const reminder of reminders) {
        if (reminder.completed) continue
        if (notifiedIds.value.has(reminder.id)) continue

        const remindTime = new Date(reminder.remindAt)
        // 如果提醒时间已到（且在过去 1 小时内），弹出通知
        if (remindTime <= now && now.getTime() - remindTime.getTime() < 3600000) {
          showNotification('⏰ NoteVault 提醒', reminder.content)
          notifiedIds.value.add(reminder.id)
        }
      }
    } catch (e) {
      console.error('Failed to check reminders:', e)
    }
  }

  // 开始定时检查
  function startChecking() {
    requestNotificationPermission()
    // 每 30 秒检查一次
    checkInterval = setInterval(checkReminders, 30000)
    // 启动时立即检查一次
    setTimeout(checkReminders, 2000)
  }

  // 停止检查
  function stopChecking() {
    if (checkInterval) {
      clearInterval(checkInterval)
      checkInterval = null
    }
  }

  onMounted(() => {
    startChecking()
  })

  onBeforeUnmount(() => {
    stopChecking()
  })

  return {
    checkReminders,
    showNotification,
  }
}
