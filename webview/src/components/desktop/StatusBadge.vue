<template>
  <span class="status-badge" :class="`status-badge--${tone}`"><slot /></span>
</template>

<script setup lang="ts">
  withDefaults(defineProps<{ tone?: 'neutral' | 'primary' | 'success' | 'warning' | 'danger' }>(), { tone: 'neutral' })
</script>

<style scoped>
  .status-badge {
    min-height: 24px;
    padding: 3px 9px;
    display: inline-flex;
    align-items: center;
    border-radius: 999px;
    background: var(--desktop-fill);
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 650;
  }
  .status-badge--primary {
    background: var(--desktop-primary-soft);
    color: var(--primary-color);
  }
  .status-badge--success {
    background: color-mix(in srgb, var(--success-color) 12%, transparent);
    color: var(--success-color);
  }
  .status-badge--warning {
    background: color-mix(in srgb, var(--warning-color) 14%, transparent);
    color: #b45309;
  }
  .status-badge--danger {
    background: color-mix(in srgb, var(--danger-color) 12%, transparent);
    color: var(--danger-color);
  }
</style>
