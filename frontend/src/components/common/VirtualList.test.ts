// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import VirtualList from './VirtualList.vue'

interface Row {
  name: string
}

function mountList(count: number, rowHeight = 40) {
  const items = Array.from({ length: count }, (_, i) => ({ name: `row-${i}` })) as Row[]
  return mount(VirtualList as unknown as { new (): { $props: { items: Row[]; rowHeight: number } } }, {
    props: { items, rowHeight } as never,
    slots: {
      default: `<template #default="{ item }"><div class="row" :style="{ height: ${rowHeight} + 'px' }">{{ item.name }}</div></template>`,
    },
  })
}

describe('VirtualList', () => {
  it('万级数据只渲染可视窗口的行', () => {
    const wrapper = mountList(10000)
    // jsdom 视口高度为 0 → 渲染 overscan*2 兜底行，而不是 10000 行
    const rows = wrapper.findAll('.row')
    expect(rows.length).toBeLessThan(60)
    expect(rows.length).toBeGreaterThan(0)
    expect(wrapper.find('.virtual-list-spacer').exists()).toBe(true)
  })

  it('数据量小的时候全量渲染', () => {
    const wrapper = mountList(5)
    expect(wrapper.findAll('.row').length).toBe(5)
  })
})
