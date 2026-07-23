<template>
  <Teleport to="body">
    <Transition name="screen-slide">
      <section v-if="modelValue" class="full-screen-layer" role="dialog" aria-modal="true" :aria-label="title">
        <header>
          <button type="button" aria-label="返回" @click="close"><el-icon><ArrowLeft /></el-icon></button>
          <h2>{{ title }}</h2>
          <div class="header-actions"><slot name="actions" /></div>
        </header>
        <main><slot /></main>
      </section>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
  import { useMobileLayerHistory } from '@/composables/ui/useMobileLayerHistory'

  const props = defineProps<{ modelValue: boolean; title: string; historyKey?: string }>()
  const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
  const opened = computed({ get: () => props.modelValue, set: value => emit('update:modelValue', value) })
  useMobileLayerHistory(opened, props.historyKey || 'full-screen-layer')
  const close = () => (opened.value = false)
</script>

<style scoped>
  .full-screen-layer {
    position: fixed;
    inset: 0;
    z-index: 2900;
    display: flex;
    flex-direction: column;
    background: var(--bg-color);
  }
  header {
    height: calc(56px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) 12px 0;
    display: grid;
    grid-template-columns: 44px 1fr 44px;
    align-items: center;
    background: var(--card-bg);
    border-bottom: 1px solid var(--border-light);
  }
  header button {
    width: 44px;
    height: 44px;
    border: 0;
    border-radius: 14px;
    background: transparent;
    color: var(--text-primary);
    font-size: 20px;
  }
  h2 { margin: 0; text-align: center; font-size: 18px; color: var(--text-primary); }
  main { flex: 1; min-height: 0; overflow-y: auto; padding: 16px 16px calc(24px + env(safe-area-inset-bottom)); }
  .header-actions { display: flex; justify-content: flex-end; }
  .screen-slide-enter-active,
  .screen-slide-leave-active { transition: transform 200ms ease, opacity 180ms ease; }
  .screen-slide-enter-from,
  .screen-slide-leave-to { transform: translateX(100%); opacity: 0.7; }
</style>
