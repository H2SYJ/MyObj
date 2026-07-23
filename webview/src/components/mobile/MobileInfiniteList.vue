<template>
  <div ref="root" class="mobile-infinite-list">
    <slot />
    <div v-if="error" class="list-state error-state">
      <span>{{ error }}</span><button type="button" @click="$emit('retry')">重试</button>
    </div>
    <div v-else-if="loading" class="list-state"><el-icon class="is-loading"><Loading /></el-icon>加载中</div>
    <div v-else-if="!hasMore" class="list-state">没有更多了</div>
    <div ref="sentinel" class="sentinel" aria-hidden="true" />
  </div>
</template>

<script setup lang="ts">
  const props = defineProps<{ loading: boolean; hasMore: boolean; error?: string }>()
  const emit = defineEmits<{ loadMore: []; retry: [] }>()
  const sentinel = ref<HTMLElement>()
  let observer: IntersectionObserver | undefined

  onMounted(() => {
    observer = new IntersectionObserver(entries => {
      if (entries[0]?.isIntersecting && props.hasMore && !props.loading && !props.error) emit('loadMore')
    }, { rootMargin: '240px 0px' })
    if (sentinel.value) observer.observe(sentinel.value)
  })
  onBeforeUnmount(() => observer?.disconnect())
</script>

<style scoped>
  .list-state {
    min-height: 52px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 13px;
  }
  .list-state button {
    min-height: 36px;
    border: 0;
    border-radius: 12px;
    padding: 0 14px;
    background: color-mix(in srgb, var(--primary-color) 12%, transparent);
    color: var(--primary-color);
  }
  .sentinel { height: 1px; }
</style>
