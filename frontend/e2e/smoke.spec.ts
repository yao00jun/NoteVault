import { test, expect } from '@playwright/test'

/**
 * 浏览器侧启动冒烟
 *
 * 只验证「SPA 能在没有 Go 后端的情况下正常挂载」这一件事：
 * 白屏、模块解析失败、启动期未捕获异常都会在这里被拦住。
 * 后端调用的 404 属预期（绑定桩未注入），不作为失败条件。
 */
test.describe('应用启动', () => {
  test('欢迎页可以正常挂载且无未捕获异常', async ({ page }) => {
    const pageErrors: string[] = []
    page.on('pageerror', (err) => pageErrors.push(err.message))

    await page.goto('/')

    await expect(page).toHaveTitle(/NoteVault/)
    await expect(page.locator('#app')).toBeVisible()
    // 欢迎页的核心入口：能渲染出按钮说明 i18n、路由、组件树都初始化完成
    await expect(page.getByText('新建文档').first()).toBeVisible()

    expect(pageErrors).toEqual([])
  })
})
