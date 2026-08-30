// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter as createVueRouter } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { i18n } from '@/i18n'
import CommandPalette from './CommandPalette.vue'
import { usePluginRuntimeStore } from '@/stores/pluginRuntime'

function createRouter() {
  return createVueRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/search', component: { template: '<div />' } },
      { path: '/', component: { template: '<div />' } },
    ],
  })
}

describe('CommandPalette', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('merges runtime commands directly after built-in commands', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter()
    await router.push('/')
    const wrapper = mount(CommandPalette, {
      attachTo: document.body,
      props: { visible: true },
      global: { plugins: [pinia, router, i18n], stubs: { teleport: true } },
    })
    await flushPromises()

    const store = usePluginRuntimeStore()
    store.commands = [
      {
        description: 'Notify the E2E harness',
        id: 'hello',
        label: 'E2E Plugin Notify',
        pluginId: 'p1',
      },
    ]
    store.failedPlugins = [{ id: 'bad', name: 'Broken Plugin' }]
    await flushPromises()

    const items = wrapper.findAll('.command-item')
    expect(items).toHaveLength(18)
    expect(items[17].text()).toContain('E2E Plugin Notify')
    const pluginItems = wrapper.findAll('[data-testid="plugin-command"]')
    expect(pluginItems[0].attributes('data-plugin-id')).toBe('p1')
    expect(wrapper.find('[data-testid="plugin-runtime-failed"]').text()).toContain('Broken Plugin')
    wrapper.unmount()
  })

  it('runs a plugin command when its result is clicked', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter()
    await router.push('/')
    const wrapper = mount(CommandPalette, {
      attachTo: document.body,
      props: { visible: true },
      global: { plugins: [pinia, router, i18n], stubs: { teleport: true } },
    })
    await flushPromises()

    const store = usePluginRuntimeStore()
    store.commands = [
      {
        description: '',
        id: 'hello',
        label: 'E2E Plugin Notify',
        pluginId: 'p1',
      },
    ]
    await flushPromises()
    const runCommand = vi.spyOn(store, 'runCommand').mockResolvedValue(undefined)

    await wrapper.find('[data-testid="plugin-command"]').trigger('click')
    await flushPromises()
    expect(runCommand).toHaveBeenCalledWith('p1', 'hello')
    wrapper.unmount()
  })

  it('closes from a global Escape even when focus is outside the palette', async () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const router = createRouter()
    await router.push('/')
    const wrapper = mount(CommandPalette, {
      attachTo: document.body,
      props: { visible: true },
      global: { plugins: [pinia, router, i18n], stubs: { teleport: true } },
    })
    await flushPromises()

    document.body.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    }))

    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })
})
