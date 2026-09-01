// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises } from '@vue/test-utils'
import MarkdownPreview from './MarkdownPreview.vue'
import { useSettingsStore } from '@/stores/settings'
import { createI18n } from 'vue-i18n'

// Mock FileService —— MarkdownPreview 在嵌入渲染时会调 ReadFile
// 同步 mock CredentialService 让 settings store 启动时不报错
const readFileMock = vi.fn()
vi.mock('@bindings/github.com/notevault/notevault/index.js', () => ({
  FileService: {
    ReadFile: (...args: unknown[]) => readFileMock(...args),
  },
  CredentialService: {
    GetCredential: vi.fn().mockResolvedValue(''),
    SaveCredential: vi.fn().mockResolvedValue(undefined),
    DeleteCredential: vi.fn().mockResolvedValue(undefined),
  },
}))

enableAutoUnmount(afterEach)

// 全局 i18n 实例（zh-CN 为主）
const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: {
    'zh-CN': {
      editor: {
        embedLoading: '嵌入加载中：',
        embedFailed: '嵌入失败：{file} ({reason})',
        embedUnsupported: '暂不支持嵌入：',
        embedNestedTooDeep: '嵌套过深：{file}',
        copyBlockLink: '复制块链接',
        copyHeadingLink: '复制标题链接',
        calloutTitle: {
          note: '笔记',
          info: '信息',
          tip: '提示',
          success: '成功',
          question: '疑问',
          warning: '警告',
          danger: '危险',
          bug: '缺陷',
          example: '示例',
          quote: '引用',
        },
      },
    },
    'en-US': {
      editor: {
        embedLoading: 'Loading embed: ',
        embedFailed: 'Embed failed: {file} ({reason})',
        embedUnsupported: 'Unsupported embed: ',
        embedNestedTooDeep: 'Nested too deep: {file}',
        copyBlockLink: 'Copy block link',
        copyHeadingLink: 'Copy heading link',
        calloutTitle: {
          note: 'Note',
          info: 'Info',
          tip: 'Tip',
          success: 'Success',
          question: 'Question',
          warning: 'Warning',
          danger: 'Danger',
          bug: 'Bug',
          example: 'Example',
          quote: 'Quote',
        },
      },
    },
  },
})

interface MountOpts {
  workspacePath?: string
  currentFileName?: string
}

function mountPreview(content: string, opts: MountOpts | string = {}) {
  const o: MountOpts = typeof opts === 'string' ? { workspacePath: opts } : opts
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(MarkdownPreview, {
    props: { content, workspacePath: o.workspacePath, currentFileName: o.currentFileName },
    global: { plugins: [pinia, i18n] },
  })
}

describe('MarkdownPreview', () => {
  beforeEach(() => {
    readFileMock.mockReset()
  })

  it('使用编辑器设置中的预览字号和行高', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const settingsStore = useSettingsStore()
    settingsStore.settings.editor.previewFontSize = 18
    settingsStore.settings.editor.lineHeight = 2

    const wrapper = mount(MarkdownPreview, {
      props: { content: '# Preview' },
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.find('.markdown-preview').attributes('style')).toContain('font-size: 18px')
    expect(wrapper.find('.markdown-preview').attributes('style')).toContain('line-height: 2')
  })

  it('wiki-link 解析：[[note]] 渲染为带 data-file 的链接', () => {
    const wrapper = mountPreview('链接 [[note]] 测试')
    const link = wrapper.find('.wiki-link')
    expect(link.exists()).toBe(true)
    expect(link.attributes('data-file')).toBe('note')
    expect(link.attributes('data-anchor') || '').toBe('')
    expect(link.attributes('data-block') || '').toBe('')
    expect(link.text()).toBe('note')
  })

  it('wiki-link 解析：[[note|别名]] 用别名作显示文本', () => {
    const wrapper = mountPreview('[[note|别名]]')
    const link = wrapper.find('.wiki-link')
    expect(link.attributes('data-file')).toBe('note')
    expect(link.text()).toBe('别名')
  })

  it('wiki-link 解析：[[note#标题]] 拆出 file + anchor', () => {
    const wrapper = mountPreview('[[note#某节]]')
    const link = wrapper.find('.wiki-link')
    expect(link.attributes('data-file')).toBe('note')
    expect(link.attributes('data-anchor')).toBe('某节')
    // 显示文本回退到 anchor
    expect(link.text()).toBe('某节')
  })

  it('wiki-link 解析：[[note^block1]] 拆出 file + block', () => {
    const wrapper = mountPreview('[[note^blk1]]')
    const link = wrapper.find('.wiki-link')
    expect(link.attributes('data-file')).toBe('note')
    expect(link.attributes('data-block')).toBe('blk1')
    // 显示文本回退到 block
    expect(link.text()).toBe('blk1')
  })

  it('wiki-link 解析：[[#标题]] 同文件锚点（file 为空）', () => {
    const wrapper = mountPreview('[[#标题]]')
    const link = wrapper.find('.wiki-link')
    expect(link.attributes('data-file') || '').toBe('')
    expect(link.attributes('data-anchor')).toBe('标题')
    expect(link.text()).toBe('标题')
  })

  it('wiki-link 解析：[[note#标题^blk|别名]] 全四元组（别名优先）', () => {
    const wrapper = mountPreview('[[note#标题^blk|别名]]')
    const link = wrapper.find('.wiki-link')
    expect(link.attributes('data-file')).toBe('note')
    expect(link.attributes('data-anchor')).toBe('标题')
    expect(link.attributes('data-block')).toBe('blk')
    expect(link.text()).toBe('别名')
  })

  it('点击 wiki-link 触发 wiki-link-click 事件，载荷为结构化对象', async () => {
    const wrapper = mountPreview('[[note#标题]]')
    await wrapper.find('.wiki-link').trigger('click')
    const evt = wrapper.emitted('wiki-link-click')
    expect(evt).toBeTruthy()
    expect(evt![0][0]).toEqual({
      file: 'note',
      anchor: '标题',
      block: '',
      raw: 'note#标题',
    })
  })

  it('点击同文件锚点不触发事件（MarkdownPreview 自处理滚动）', async () => {
    const wrapper = mountPreview('[[#标题]]')
    await wrapper.find('.wiki-link').trigger('click')
    // 同文件锚点 file 为空时，组件自己处理滚动，不向上 emit
    expect(wrapper.emitted('wiki-link-click')).toBeFalsy()
  })

  it('渲染后给 heading 加 slugified id（用于锚点跳转）', async () => {
    const wrapper = mountPreview('# Hello World\n\n正文')
    await wrapper.vm.$nextTick()
    const h1 = wrapper.find('h1')
    expect(h1.attributes('id')).toBe('hello-world')
  })

  it('块 ID 预处理：独立成行的 ^id 渲染为隐藏锚点元素', () => {
    const wrapper = mountPreview('一段内容\n\n^blk1\n\n下一段')
    const anchor = wrapper.find('.nv-block-anchor')
    expect(anchor.exists()).toBe(true)
    expect(anchor.attributes('data-block-id')).toBe('blk1')
    expect(anchor.attributes('id')).toBe('^blk1')
  })

  it('嵌入 ![[note]] 渲染为占位 div（异步加载前）', () => {
    const wrapper = mountPreview('![[note]]', '/workspace')
    const embed = wrapper.find('.nv-embed[data-embed-kind="markdown"]')
    expect(embed.exists()).toBe(true)
    expect(embed.attributes('data-embed-file')).toBe('note')
    expect(embed.classes()).toContain('nv-embed-loading')
  })

  it('嵌入图片 ![[image.png]] 渲染为 <img class=nv-embed-image>', () => {
    const wrapper = mountPreview('![[image.png]]', '/workspace')
    const img = wrapper.find('img.nv-embed-image')
    expect(img.exists()).toBe(true)
    expect(img.attributes('data-embed-file')).toBe('image.png')
    expect(img.attributes('alt')).toBe('image.png')
  })

  it('嵌入图片别名：![[image.png|图片描述]]', () => {
    const wrapper = mountPreview('![[image.png|图片描述]]', '/workspace')
    const img = wrapper.find('img.nv-embed-image')
    expect(img.attributes('alt')).toBe('图片描述')
  })

  it('嵌入非 markdown/pdf 文件给友好提示', () => {
    const wrapper = mountPreview('![[doc.pdf]]', '/workspace')
    const embed = wrapper.find('.nv-embed-unsupported')
    expect(embed.exists()).toBe(true)
    expect(embed.attributes('data-embed-file')).toBe('doc.pdf')
  })

  it('嵌入 markdown 异步加载：成功时显示嵌入内容', async () => {
    readFileMock.mockResolvedValue('# 嵌入标题\n\n嵌入正文')
    const wrapper = mountPreview('![[target]]', '/workspace')
    // 等待异步加载完成
    await flushPromises()
    await wrapper.vm.$nextTick()
    // ReadFile 被调用
    expect(readFileMock).toHaveBeenCalledWith('/workspace', 'target.md')
    // 加载成功后 loading class 移除、loaded class 添加
    const embed = wrapper.find('.nv-embed[data-embed-kind="markdown"]')
    expect(embed.classes()).toContain('nv-embed-loaded')
    expect(embed.classes()).not.toContain('nv-embed-loading')
    // 嵌入头部有 target 标签
    expect(embed.find('.nv-embed-link').text()).toBe('target')
    // 嵌入正文含原内容
    expect(embed.html()).toContain('嵌入标题')
    expect(embed.html()).toContain('嵌入正文')
  })

  it('嵌入 markdown 异步加载：失败时显示错误信息', async () => {
    readFileMock.mockRejectedValue(new Error('文件不存在'))
    const wrapper = mountPreview('![[missing]]', '/workspace')
    await flushPromises()
    await wrapper.vm.$nextTick()
    const embed = wrapper.find('.nv-embed[data-embed-kind="markdown"]')
    expect(embed.classes()).toContain('nv-embed-error')
    expect(embed.html()).toContain('文件不存在')
    // 触发 embed-error 事件
    const evt = wrapper.emitted('embed-error')
    expect(evt).toBeTruthy()
    expect((evt![0][0] as { file: string; reason: string }).file).toBe('missing')
  })

  it('嵌入无 workspacePath 时保留 loading 占位（降级）', async () => {
    const wrapper = mountPreview('![[note]]') // 不传 workspacePath
    await flushPromises()
    expect(readFileMock).not.toHaveBeenCalled()
    const embed = wrapper.find('.nv-embed[data-embed-kind="markdown"]')
    expect(embed.classes()).toContain('nv-embed-loading')
  })

  it('嵌入 #anchor 切片：只渲染对应章节', async () => {
    const content = '# 顶部\n\n前面内容\n\n## 章节A\n\nA 内容\n\n## 章节B\n\nB 内容'
    readFileMock.mockResolvedValue(content)
    const wrapper = mountPreview('![[target#章节A]]', '/workspace')
    await flushPromises()
    await wrapper.vm.$nextTick()
    const embed = wrapper.find('.nv-embed[data-embed-kind="markdown"]')
    expect(embed.html()).toContain('章节A')
    expect(embed.html()).toContain('A 内容')
    // 章节B 的内容不应出现
    expect(embed.html()).not.toContain('B 内容')
  })

  it('HTML 注入防御：file 含双引号时被转义', () => {
    const wrapper = mountPreview('[[note" onclick="alert(1)]]')
    const link = wrapper.find('.wiki-link')
    expect(link.exists()).toBe(true)
    // 不应出现未转义的双引号注入
    const html = wrapper.html()
    expect(html).not.toContain('onclick="alert(1)"')
  })
})

describe('MarkdownPreview · 复制块/标题链接', () => {
  beforeEach(() => {
    readFileMock.mockReset()
  })

  it('块 ID 渲染为带 data-copy-block 的 ¶ 锚点', async () => {
    const wrapper = mountPreview('段落内容\n\n^blk1\n')
    await flushPromises()
    const anchor = wrapper.find('.nv-block-anchor[data-copy-block="blk1"]')
    expect(anchor.exists()).toBe(true)
    expect(anchor.attributes('data-block-id')).toBe('blk1')
    expect(anchor.attributes('title')).toBe('复制块链接')
  })

  it('heading 被注入 ¶ 复制按钮，且原文存进 data-heading-text', async () => {
    const wrapper = mountPreview('# 标题一\n\n正文')
    await flushPromises()
    const h1 = wrapper.find('h1')
    expect(h1.attributes('data-heading-text')).toBe('标题一')
    const btn = h1.find('.nv-heading-anchor')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('data-copy-anchor')).toBe('标题一')
  })

  it('多次渲染不会重复注入 ¶ 按钮', async () => {
    const wrapper = mountPreview('# 标题一\n\n正文')
    await flushPromises()
    await wrapper.setProps({ content: '# 标题一\n\n改过的正文' })
    await flushPromises()
    expect(wrapper.findAll('h1 .nv-heading-anchor').length).toBe(1)
  })

  it('点块 ¶：带文件名时复制 [[note^blk]]', async () => {
    const wrapper = mountPreview('段落\n\n^blk1\n', { currentFileName: 'note.md' })
    await flushPromises()
    await wrapper.find('[data-copy-block="blk1"]').trigger('click')
    await flushPromises()
    const ev = wrapper.emitted('anchor-copy') as Array<Array<{ text: string; ok: boolean }>> | undefined
    expect(ev).toBeTruthy()
    expect(ev![0][0].text).toBe('[[note.md^blk1]]')
  })

  it('点块 ¶：无文件名时降级为同文件引用 [[^blk]]', async () => {
    const wrapper = mountPreview('段落\n\n^blk1\n')
    await flushPromises()
    await wrapper.find('[data-copy-block="blk1"]').trigger('click')
    await flushPromises()
    const ev = wrapper.emitted('anchor-copy') as Array<Array<{ text: string; ok: boolean }>> | undefined
    expect(ev![0][0].text).toBe('[[^blk1]]')
  })

  it('点标题 ¶：复制 [[note#标题]]（用原文而非 slug）', async () => {
    const wrapper = mountPreview('# Hello World\n\n正文', { currentFileName: 'note.md' })
    await flushPromises()
    await wrapper.find('.nv-heading-anchor').trigger('click')
    await flushPromises()
    const ev = wrapper.emitted('anchor-copy') as Array<Array<{ text: string; ok: boolean }>> | undefined
    expect(ev![0][0].text).toBe('[[note.md#Hello World]]')
  })

  it('复制失败时仍 emit（ok=false），父组件可兜底提示', async () => {
    // jsdom 无 navigator.clipboard / execCommand，预期走到底返回 false
    const wrapper = mountPreview('段落\n\n^blk1\n', { currentFileName: 'note.md' })
    await flushPromises()
    await wrapper.find('[data-copy-block="blk1"]').trigger('click')
    await flushPromises()
    const ev = wrapper.emitted('anchor-copy') as Array<Array<{ text: string; ok: boolean }>> | undefined
    expect(ev![0][0].ok).toBe(false)
  })

  it('剪贴板可用时 emit ok=true', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })
    const wrapper = mountPreview('段落\n\n^blk1\n', { currentFileName: 'note.md' })
    await flushPromises()
    await wrapper.find('[data-copy-block="blk1"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('[[note.md^blk1]]')
    const ev = wrapper.emitted('anchor-copy') as Array<Array<{ text: string; ok: boolean }>> | undefined
    expect(ev![0][0].ok).toBe(true)
    Reflect.deleteProperty(navigator, 'clipboard')
  })

  it('¶ 点击不触发 wiki-link 跳转', async () => {
    const wrapper = mountPreview('段落\n\n^blk1\n', { currentFileName: 'note.md' })
    await flushPromises()
    await wrapper.find('[data-copy-block="blk1"]').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('wiki-link-click')).toBeFalsy()
  })
})

describe('MarkdownPreview callout', () => {
  it('> [!note] 渲染为 .nv-callout 且默认标题本地化', async () => {
    const wrapper = mountPreview('> [!note]\n> 这是内容\n')
    await flushPromises()
    const callout = wrapper.find('.nv-callout')
    expect(callout.exists()).toBe(true)
    expect(callout.classes()).toContain('nv-callout-blue')
    const summary = callout.find('summary')
    expect(summary.exists()).toBe(true)
    expect(summary.text()).toContain('笔记')
  })

  it('自定义标题优先于默认标题', async () => {
    const wrapper = mountPreview('> [!warning] 小心陷阱\n> 内容\n')
    await flushPromises()
    const summary = wrapper.find('.nv-callout summary')
    expect(summary.text()).toContain('小心陷阱')
    expect(summary.text()).not.toContain('警告')
  })

  it('折叠标记 - 使 details 默认收起（无 open）', async () => {
    const wrapper = mountPreview('> [!tip]-\n> 折叠内容\n')
    await flushPromises()
    const details = wrapper.find('.nv-callout')
    expect(details.element.tagName.toLowerCase()).toBe('details')
    expect((details.element as HTMLDetailsElement).open).toBe(false)
  })

  it('展开标记 + 使 details 默认展开', async () => {
    const wrapper = mountPreview('> [!tip]+\n> 展开内容\n')
    await flushPromises()
    const details = wrapper.find('.nv-callout')
    expect((details.element as HTMLDetailsElement).open).toBe(true)
  })

  it('普通引用不被误判为 callout', async () => {
    const wrapper = mountPreview('> 这是普通引用\n')
    await flushPromises()
    expect(wrapper.find('.nv-callout').exists()).toBe(false)
    expect(wrapper.find('blockquote').exists()).toBe(true)
  })

  it('danger 类型映射为红色变体', async () => {
    const wrapper = mountPreview('> [!danger]\n> 危险\n')
    await flushPromises()
    const callout = wrapper.find('.nv-callout')
    expect(callout.classes()).toContain('nv-callout-red')
  })
})
