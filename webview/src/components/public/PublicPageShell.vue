<template>
  <main class="public-shell">
    <div class="public-shell__glow public-shell__glow--start"></div>
    <div class="public-shell__glow public-shell__glow--end"></div>
    <section class="public-shell__content">
      <header v-if="showBrand" class="public-shell__brand">
        <div class="public-shell__logo-row">
          <span class="public-shell__logo">MyObj</span>
          <span v-if="badge" class="public-shell__badge">{{ badge }}</span>
        </div>
        <p v-if="subtitle">{{ subtitle }}</p>
      </header>
      <slot />
      <footer v-if="$slots.footer" class="public-shell__footer"><slot name="footer" /></footer>
    </section>
  </main>
</template>

<script setup lang="ts">
  withDefaults(defineProps<{ subtitle?: string; badge?: string; showBrand?: boolean }>(), {
    subtitle: '',
    badge: '',
    showBrand: true
  })
</script>

<style scoped>
  .public-shell {
    position: relative;
    isolation: isolate;
    min-height: 100vh;
    min-height: 100dvh;
    padding: 40px 24px;
    display: grid;
    place-items: center;
    overflow: hidden;
    background: #f6f8fc;
    color: var(--text-primary);
  }
  html.dark .public-shell {
    background: #0d1421;
  }
  .public-shell__glow {
    position: absolute;
    z-index: -1;
    width: min(48vw, 680px);
    aspect-ratio: 1;
    border-radius: 50%;
    filter: blur(100px);
    opacity: 0.18;
    pointer-events: none;
  }
  .public-shell__glow--start {
    top: -28%;
    left: -14%;
    background: #2563eb;
  }
  .public-shell__glow--end {
    right: -16%;
    bottom: -32%;
    background: #7c3aed;
  }
  .public-shell__content {
    width: min(100%, 640px);
    min-width: 0;
  }
  .public-shell__brand {
    margin-bottom: 28px;
    text-align: center;
  }
  .public-shell__logo-row {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
  }
  .public-shell__logo {
    color: var(--text-primary);
    font-size: 34px;
    font-weight: 800;
    letter-spacing: -0.04em;
  }
  .public-shell__badge {
    padding: 4px 8px;
    border-radius: 7px;
    background: var(--el-color-primary);
    color: white;
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.08em;
  }
  .public-shell__brand p {
    margin: 8px 0 0;
    color: var(--text-secondary);
    font-size: 14px;
  }
  .public-shell__footer {
    margin-top: 18px;
    color: var(--text-secondary);
    text-align: center;
    font-size: 12px;
  }
  @media (max-width: 767px) {
    .public-shell {
      padding: calc(20px + env(safe-area-inset-top)) max(12px, env(safe-area-inset-right))
        calc(20px + env(safe-area-inset-bottom)) max(12px, env(safe-area-inset-left));
      align-items: start;
    }
    .public-shell__brand {
      margin: 18px 0 24px;
    }
  }
</style>
