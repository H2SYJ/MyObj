<template>
  <div class="page-toolbar" :class="{ 'page-toolbar--selected': selectedCount > 0 }">
    <div class="page-toolbar__primary">
      <span v-if="selectedCount > 0" class="page-toolbar__selection">{{ selectedText }}</span>
      <slot name="primary" />
    </div>
    <div class="page-toolbar__secondary"><slot /></div>
  </div>
</template>

<script setup lang="ts">
  const props = withDefaults(defineProps<{ selectedCount?: number; selectedLabel?: string }>(), {
    selectedCount: 0,
    selectedLabel: ''
  })
  const selectedText = computed(() => props.selectedLabel || `已选择 ${props.selectedCount} 项`)
</script>

<style scoped>
  .page-toolbar {
    min-height: 56px;
    padding: 10px 12px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    background: var(--desktop-surface);
    box-shadow: var(--desktop-shadow-sm);
  }
  .page-toolbar--selected {
    border-color: color-mix(in srgb, var(--primary-color) 34%, var(--desktop-border));
    background: var(--desktop-primary-soft);
  }
  .page-toolbar__primary,
  .page-toolbar__secondary {
    min-width: 0;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .page-toolbar__secondary {
    justify-content: flex-end;
  }
  .page-toolbar__selection {
    padding: 0 8px;
    color: var(--primary-color);
    font-size: 13px;
    font-weight: 700;
  }
</style>
