<template>
  <Teleport to="body">
    <Transition name="sheet-fade">
      <div v-if="modelValue" class="sheet-overlay" role="presentation" @click.self="close">
        <section class="action-sheet" role="dialog" aria-modal="true" :aria-label="title || '操作菜单'">
          <div class="sheet-handle" />
          <h2 v-if="title">{{ title }}</h2>
          <button
            v-for="action in actions"
            :key="action.key"
            type="button"
            class="sheet-action"
            :class="[`tone-${action.tone || 'default'}`]"
            :disabled="action.disabled"
            @click="select(action.key)"
          >
            <el-icon v-if="action.icon"><component :is="action.icon" /></el-icon>
            <span>{{ action.label }}</span>
          </button>
          <button type="button" class="sheet-cancel" @click="close">取消</button>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
  import { useMobileLayerHistory } from '@/composables/ui/useMobileLayerHistory'
  import type { MobileSheetAction } from './types'

  const props = defineProps<{ modelValue: boolean; title?: string; actions: MobileSheetAction[]; historyKey?: string }>()
  const emit = defineEmits<{ 'update:modelValue': [value: boolean]; select: [key: string] }>()
  const opened = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
  useMobileLayerHistory(opened, props.historyKey || 'action-sheet')

  const close = () => (opened.value = false)
  const select = (key: string) => {
    emit('select', key)
    close()
  }
</script>

<style scoped>
  .sheet-overlay {
    position: fixed;
    inset: 0;
    z-index: 3000;
    display: flex;
    align-items: flex-end;
    background: rgba(15, 23, 42, 0.42);
    backdrop-filter: blur(3px);
  }

  .action-sheet {
    width: 100%;
    max-height: min(78dvh, 680px);
    padding: 8px 12px calc(12px + env(safe-area-inset-bottom));
    overflow-y: auto;
    background: var(--card-bg);
    border-radius: 24px 24px 0 0;
    box-shadow: 0 -16px 40px rgba(15, 23, 42, 0.18);
  }

  .sheet-handle {
    width: 38px;
    height: 4px;
    margin: 2px auto 12px;
    border-radius: 99px;
    background: var(--border-color);
  }

  h2 {
    margin: 0 8px 8px;
    font-size: 16px;
    color: var(--text-primary);
  }

  .sheet-action,
  .sheet-cancel {
    width: 100%;
    min-height: 52px;
    border: 0;
    border-radius: 14px;
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 0 16px;
    color: var(--text-primary);
    background: transparent;
    font-size: 15px;
    text-align: left;
  }

  .sheet-action:active,
  .sheet-cancel:active {
    background: var(--border-light);
  }

  .tone-primary { color: var(--primary-color); }
  .tone-danger { color: var(--danger-color); }
  .sheet-action:disabled { opacity: 0.4; }
  .sheet-cancel {
    margin-top: 8px;
    justify-content: center;
    background: var(--border-light);
    font-weight: 600;
  }

  .sheet-fade-enter-active,
  .sheet-fade-leave-active { transition: opacity 180ms ease; }
  .sheet-fade-enter-active .action-sheet,
  .sheet-fade-leave-active .action-sheet { transition: transform 200ms ease; }
  .sheet-fade-enter-from,
  .sheet-fade-leave-to { opacity: 0; }
  .sheet-fade-enter-from .action-sheet,
  .sheet-fade-leave-to .action-sheet { transform: translateY(100%); }
</style>
