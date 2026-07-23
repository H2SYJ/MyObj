<template>
  <nav class="mobile-bottom-nav" aria-label="主导航">
    <router-link v-for="item in items" :key="item.key" :to="item.path" class="nav-item">
      <el-icon><component :is="item.icon" /></el-icon>
      <span>{{ item.label }}</span>
    </router-link>
  </nav>
</template>

<script setup lang="ts">
  import type { MobileNavItem } from './types'

  defineProps<{ items: MobileNavItem[] }>()
</script>

<style scoped>
  .mobile-bottom-nav {
    min-height: calc(62px + env(safe-area-inset-bottom));
    padding: 6px 8px env(safe-area-inset-bottom);
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    background: color-mix(in srgb, var(--card-bg) 94%, transparent);
    border-top: 1px solid var(--border-light);
    backdrop-filter: blur(20px);
    z-index: 30;
  }

  .nav-item {
    min-width: 0;
    min-height: 50px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 3px;
    border-radius: 14px;
    color: var(--text-secondary);
    text-decoration: none;
    font-size: 11px;
    font-weight: 600;
    transition: color 180ms ease, background-color 180ms ease, transform 180ms ease;
  }

  .nav-item .el-icon {
    font-size: 21px;
  }

  .nav-item.router-link-active {
    color: var(--primary-color);
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
  }

  .nav-item:active {
    transform: scale(0.95);
  }
</style>
