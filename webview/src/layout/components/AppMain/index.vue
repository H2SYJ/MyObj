<template>
  <main class="desktop-main" aria-live="polite">
    <router-view v-slot="{ Component }">
      <transition name="desktop-page-fade" mode="out-in">
        <component :is="Component" />
      </transition>
    </router-view>
  </main>
</template>

<style scoped>
  .desktop-main {
    grid-area: main;
    min-width: 0;
    min-height: 0;
    overflow: auto;
    background: var(--desktop-surface-muted);
    scrollbar-gutter: stable;
  }

  .desktop-page-fade-enter-active,
  .desktop-page-fade-leave-active {
    transition:
      opacity 140ms ease,
      transform 140ms ease;
  }
  .desktop-page-fade-enter-from {
    opacity: 0;
    transform: translateY(4px);
  }
  .desktop-page-fade-leave-to {
    opacity: 0;
    transform: translateY(-2px);
  }
</style>
