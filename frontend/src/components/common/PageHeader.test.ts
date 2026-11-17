// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { Rocket } from '@lucide/vue'
import PageHeader from './PageHeader.vue'

describe('PageHeader', () => {
  it('渲染 eyebrow、标题与副标题', () => {
    const wrapper = mount(PageHeader, {
      props: {
        title: '商城',
        subtitle: '完成接口联调',
        eyebrow: '项目',
        icon: Rocket,
      },
    })
    expect(wrapper.get('.page-header-eyebrow').text()).toContain('项目')
    expect(wrapper.get('h1').text()).toBe('商城')
    expect(wrapper.get('.page-header-subtitle').text()).toBe('完成接口联调')
  })

  it('没有副标题时不渲染占位段落', () => {
    const wrapper = mount(PageHeader, { props: { title: '资料库' } })
    expect(wrapper.find('.page-header-subtitle').exists()).toBe(false)
    expect(wrapper.find('.page-header-eyebrow').exists()).toBe(false)
  })

  it('渲染操作区、概要条与页签插槽', () => {
    const wrapper = mount(PageHeader, {
      props: { title: '商城', tabsAriaLabel: '项目页签' },
      slots: {
        actions: '<button data-testid="primary">新建</button>',
        badges: '<span data-testid="badge">进行中</span>',
        meta: '<p data-testid="meta">下一步：联调</p>',
        tabs: '<button data-testid="tab">任务</button>',
      },
    })
    expect(wrapper.get('[data-testid="primary"]').text()).toBe('新建')
    expect(wrapper.get('[data-testid="badge"]').text()).toBe('进行中')
    expect(wrapper.get('[data-testid="meta"]').text()).toContain('下一步')
    const nav = wrapper.get('.page-header-tabs')
    expect(nav.attributes('aria-label')).toBe('项目页签')
    expect(wrapper.get('[data-testid="tab"]').text()).toBe('任务')
  })

  it('dense 收紧间距，且无页签时不渲染 nav', () => {
    const wrapper = mount(PageHeader, { props: { title: '商城', dense: true } })
    expect(wrapper.get('.page-header').classes()).toContain('dense')
    expect(wrapper.find('.page-header-tabs').exists()).toBe(false)
  })
})
