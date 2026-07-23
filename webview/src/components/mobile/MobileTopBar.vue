<template>
  <header class="mobile-top-bar">
    <button v-if="showBack" type="button" class="top-bar-button" aria-label="返回" @click="$emit('back')">
      <el-icon><ArrowLeft /></el-icon>
    </button>
    <div v-else class="top-bar-leading">
      <img :src="logoImage" alt="MyObj" />
    </div>
    <h1>{{ title }}</h1>
    <div class="top-bar-actions">
      <button v-if="searchable" type="button" class="top-bar-button" aria-label="搜索" @click="$emit('search')">
        <el-icon><Search /></el-icon>
      </button>
      <slot name="actions" />
    </div>
  </header>
</template>

<script setup lang="ts">
  import logoImage from '@/assets/images/LOGO.png'

  defineProps<{ title: string; showBack?: boolean; searchable?: boolean }>()
  defineEmits<{ back: []; search: [] }>()
</script>

<style scoped>
  .mobile-top-bar {
    height: calc(56px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) 12px 0;
    display: grid;
    grid-template-columns: 44px minmax(0, 1fr) auto;
    align-items: center;
    gap: 4px;
    background: color-mix(in srgb, var(--card-bg) 92%, transparent);
    border-bottom: 1px solid var(--border-light);
    backdrop-filter: blur(18px);
    position: relative;
    z-index: 30;
  }

  h1 {
    margin: 0;
    font-size: 18px;
    font-weight: 700;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: center;
  }

  .top-bar-leading,
  .top-bar-button {
    width: 44px;
    height: 44px;
    display: grid;
    place-items: center;
  }

  .top-bar-leading img {
    width: 30px;
    height: 30px;
    object-fit: contain;
  }

  .top-bar-button {
    border: 0;
    border-radius: 14px;
    color: var(--text-primary);
    background: transparent;
    font-size: 21px;
  }

  .top-bar-button:active {
    background: var(--border-light);
    transform: scale(0.96);
  }

  .top-bar-actions {
    min-width: 44px;
    display: flex;
    justify-content: flex-end;
  }
</style>
