<script setup lang="ts">
/**
 * KnowledgeTagCloud - 知识库主页标签云
 * 纯展示组件：接收标签列表，点击跳转 /tags 页面。
 */
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Tags as TagsIcon, ArrowUpRight, Hash } from 'lucide-vue-next'
import type { TagInfo } from '@bindings/github.com/notevault/notevault/models.js'

const { t } = useI18n()
const router = useRouter()

defineProps<{ tags: TagInfo[] }>()

/** 标签字体大小计算（基于使用次数） */
function getTagFontSize(count: number): string {
  return `${12 + Math.min(count, 20) * 0.5}px`
}
</script>

<template>
  <div class="kv-card">
    <div class="kv-card-header">
      <h3>
        <TagsIcon :size="14" />
        <span>{{ t('knowledge.tagsPanel') }}</span>
      </h3>
      <router-link
        to="/tags"
        class="kv-card-link"
      >
        {{ t('knowledge.viewAll') }} <ArrowUpRight :size="12" />
      </router-link>
    </div>
    <div
      v-if="tags.length === 0"
      class="kv-card-empty"
    >
      {{ t('knowledge.noTags') }}
    </div>
    <div
      v-else
      class="kv-tag-cloud"
    >
      <button
        v-for="tag in tags"
        :key="tag.name"
        class="kv-tag-chip"
        :style="{ fontSize: getTagFontSize(tag.count) }"
        :title="t('knowledge.docCount', { count: tag.count })"
        @click="router.push('/tags')"
      >
        <Hash :size="10" />
        {{ tag.name }}
        <span class="kv-tag-count">{{ tag.count }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.kv-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
  flex-shrink: 0;
}

.kv-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
  background: var(--bg-window);
}

.kv-card-header h3 {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.kv-card-link {
  display: flex;
  align-items: center;
  gap: 2px;
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-decoration: none;
}

.kv-card-link:hover {
  color: var(--accent);
}

.kv-card-empty {
  padding: var(--space-4);
  font-size: var(--text-xs);
  color: var(--text-muted);
  text-align: center;
}

/* 标签云 */
.kv-tag-cloud {
  padding: var(--space-3) var(--space-4);
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-items: center;
}

.kv-tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: var(--bg-card);
  color: var(--text-secondary);
  font-weight: 500;
  cursor: pointer;
  transition: all var(--transition-fast);
}

.kv-tag-chip:hover {
  background: var(--accent-alpha, rgba(0, 122, 255, 0.1));
  border-color: var(--accent);
  color: var(--accent);
}

.kv-tag-count {
  font-size: 10px;
  background: var(--bg-sidebar);
  padding: 0 6px;
  border-radius: 8px;
  color: var(--text-muted);
  font-weight: 400;
}

.kv-tag-chip:hover .kv-tag-count {
  background: var(--accent);
  color: white;
}
</style>
