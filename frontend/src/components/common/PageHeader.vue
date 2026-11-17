<script setup lang="ts">
/**
 * PageHeader - 统一的页面头
 *
 * 收敛此前散落在各页面里的 `collection-hero` / `header-left` 结构，把
 * 「eyebrow + 标题 + 副标题 + 操作 + 页签 + 概要条」压进同一个紧凑区块，
 * 避免页面顶部连叠四五层留白、正文迟迟不出现。
 *
 * 组成：
 *  - 默认插槽无需使用，内容全部走具名插槽
 *  - #actions：右上角操作按钮组
 *  - #meta：标题下方的一行概要（状态 / 下一步 / 上下文），dense 时更紧
 *  - #tabs：页签条，自动吸附在头部底边，去掉额外外边距
 */
import type { Component } from 'vue'

withDefaults(
  defineProps<{
    title: string
    subtitle?: string
    eyebrow?: string
    icon?: Component
    /** 紧凑模式：详情页使用，减少标题到正文之间的留白 */
    dense?: boolean
    /** 页签条与头部是否分离（默认紧贴头部底边） */
    tabsDetached?: boolean
    /** 页签条的 aria-label */
    tabsAriaLabel?: string
  }>(),
  {
    subtitle: '',
    eyebrow: '',
    icon: undefined,
    dense: false,
    tabsDetached: false,
    tabsAriaLabel: undefined,
  }
)
</script>

<template>
  <header
    class="page-header"
    :class="{ dense, 'has-tabs': $slots.tabs }"
  >
    <div class="page-header-top">
      <div class="page-header-main">
        <div
          v-if="eyebrow"
          class="page-header-eyebrow collection-eyebrow"
        >
          <component
            :is="icon"
            v-if="icon"
            :size="15"
          /><span>{{ eyebrow }}</span>
        </div>
        <div class="page-header-title-row">
          <h1>{{ title }}</h1>
          <slot name="badges" />
        </div>
        <p
          v-if="subtitle"
          class="page-header-subtitle"
        >
          {{ subtitle }}
        </p>
      </div>
      <div
        v-if="$slots.actions"
        class="page-header-actions"
      >
        <slot name="actions" />
      </div>
    </div>
    <div
      v-if="$slots.meta"
      class="page-header-meta"
    >
      <slot name="meta" />
    </div>
    <nav
      v-if="$slots.tabs"
      class="page-header-tabs"
      :class="{ detached: tabsDetached }"
      :aria-label="tabsAriaLabel"
    >
      <slot name="tabs" />
    </nav>
  </header>
</template>

<style scoped>
.page-header {
  display: flex;
  flex-direction: column;
  gap: 14px;
  margin-bottom: 20px;
  min-width: 0;
}
.page-header-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 24px;
}
.page-header-main {
  min-width: 0;
}
.page-header-eyebrow {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 8px;
  color: var(--accent);
  font-size: var(--text-xs);
  font-weight: 600;
  letter-spacing: 1.2px;
}
.page-header-title-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.page-header-title-row h1 {
  margin: 0;
  font-size: clamp(23px, 2.6vw, 30px);
  font-weight: 650;
  letter-spacing: -0.6px;
  line-height: 1.28;
  overflow-wrap: anywhere;
}
.page-header-subtitle {
  margin: 7px 0 0;
  max-width: 660px;
  color: var(--text-secondary);
  font-size: 13.5px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}
.page-header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  flex-shrink: 0;
  max-width: 100%;
}
.page-header-meta {
  min-width: 0;
}
.page-header-tabs {
  display: flex;
  align-items: center;
  gap: 4px;
  margin: 0 0 -20px;
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
}
.page-header-tabs.detached {
  margin-bottom: 0;
}
/* dense：详情页/长列表页，收紧标题区，正文更早进入视野 */
.page-header.dense {
  gap: 10px;
  margin-bottom: 14px;
}
.page-header.dense .page-header-eyebrow {
  margin-bottom: 5px;
}
.page-header.dense .page-header-title-row h1 {
  font-size: clamp(21px, 2.2vw, 26px);
}
.page-header.dense .page-header-subtitle {
  margin-top: 5px;
  font-size: 13px;
  line-height: 1.6;
}
.page-header.dense .page-header-tabs {
  margin-bottom: -14px;
}
.page-header.dense .page-header-tabs.detached {
  margin-bottom: 0;
}
@media (max-width: 760px) {
  .page-header-top {
    flex-direction: column;
    gap: 12px;
  }
  .page-header-actions {
    width: 100%;
  }
}
</style>
