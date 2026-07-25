<template>
  <div
    class="table-selection-actions"
    :class="`table-selection-actions--${mode}`"
    role="toolbar"
    :aria-label="selectedText"
  >
    <span class="table-selection-actions__count">{{ selectedText }}</span>
    <div class="table-selection-actions__buttons"><slot></slot></div>
    <el-button class="table-selection-actions__clear" text data-test="selection-clear" @click="$emit('clear')">
      {{ clearText }}
    </el-button>
  </div>
</template>

<script setup lang="ts">
  withDefaults(
    defineProps<{
      mode?: 'inline' | 'floating'
      selectedText: string
      clearText: string
    }>(),
    {
      mode: 'inline'
    }
  )

  defineEmits<{
    clear: []
  }>()
</script>

<style scoped>
  .table-selection-actions {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .table-selection-actions__count {
    color: var(--primary-color);
    font-size: 13px;
    font-weight: 700;
    white-space: nowrap;
  }

  .table-selection-actions__buttons {
    min-width: 0;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }

  .table-selection-actions__buttons :deep(.el-button + .el-button) {
    margin-left: 0;
  }

  .table-selection-actions--inline {
    justify-content: flex-end;
    flex-wrap: wrap;
  }

  .table-selection-actions--floating {
    position: fixed;
    left: 12px;
    right: 12px;
    bottom: calc(74px + env(safe-area-inset-bottom));
    z-index: 1100;
    min-height: 62px;
    justify-content: space-around;
    padding: 7px 10px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 16px;
    background: var(--el-bg-color-overlay);
    box-shadow: var(--el-box-shadow-light);
  }

  .table-selection-actions--floating .table-selection-actions__buttons {
    justify-content: center;
  }

  .table-selection-actions--floating :deep(.el-button) {
    min-width: 48px;
    margin-left: 0;
  }

  @media (max-width: 480px) {
    .table-selection-actions--floating {
      gap: 4px;
      padding-inline: 8px;
    }

    .table-selection-actions--floating .table-selection-actions__count {
      font-size: 12px;
    }

    .table-selection-actions--floating :deep(.el-button) {
      padding-inline: 6px;
      font-size: 12px;
    }
  }
</style>
