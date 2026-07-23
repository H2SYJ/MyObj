<template>
  <section class="workspace-page" :class="{ 'workspace-page--floating': $slots.floating }">
    <header class="workspace-page__header">
      <div class="workspace-page__heading">
        <div class="workspace-page__title-row">
          <span v-if="$slots.icon" class="workspace-page__icon"><slot name="icon" /></span>
          <h2>{{ title }}</h2>
          <span v-if="$slots.meta" class="workspace-page__meta"><slot name="meta" /></span>
        </div>
        <p v-if="description" class="workspace-page__description">{{ description }}</p>
        <div v-if="$slots['header-extra']" class="workspace-page__header-extra">
          <slot name="header-extra" />
        </div>
      </div>
      <div v-if="$slots.actions" class="workspace-page__actions"><slot name="actions" /></div>
    </header>

    <section class="workspace-page__panel">
      <div v-if="$slots.toolbar" class="workspace-page__toolbar"><slot name="toolbar" /></div>
      <div class="workspace-page__content"><slot /></div>
      <footer v-if="$slots.footer" class="workspace-page__footer"><slot name="footer" /></footer>
    </section>

    <slot name="floating" />
    <slot name="overlays" />
  </section>
</template>

<script setup lang="ts">
  defineProps<{
    title: string
    description?: string
  }>()
</script>

<style scoped>
  .workspace-page {
    width: min(100%, var(--desktop-content-max, 1680px));
    height: 100%;
    min-width: 0;
    min-height: 0;
    margin: 0 auto;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 20px;
  }

  .workspace-page__header,
  .workspace-page__panel {
    border: 1px solid var(--desktop-border, var(--border-light));
    border-radius: 16px;
    background: var(--desktop-surface, var(--card-bg));
    box-shadow: var(--desktop-shadow-sm, 0 8px 24px rgba(15, 23, 42, 0.05));
  }

  .workspace-page__header {
    min-height: 70px;
    flex-shrink: 0;
    padding: 16px 24px;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 20px;
  }

  .workspace-page__heading {
    min-width: 0;
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 8px;
  }

  .workspace-page__title-row {
    min-width: 0;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
  }

  .workspace-page__icon {
    width: 24px;
    height: 24px;
    flex: 0 0 24px;
    display: grid;
    place-items: center;
    color: var(--primary-color);
  }

  .workspace-page__title-row h2 {
    margin: 0;
    color: var(--text-primary);
    font-size: 20px;
    font-weight: 700;
    line-height: 1.35;
    letter-spacing: -0.015em;
  }

  .workspace-page__meta,
  .workspace-page__description {
    color: var(--text-secondary);
    font-size: 14px;
  }

  .workspace-page__description {
    max-width: 760px;
    margin: 0;
    line-height: 1.55;
  }

  .workspace-page__header-extra {
    min-width: 0;
  }

  .workspace-page__actions {
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 10px;
  }

  .workspace-page__panel {
    flex: 1;
    min-width: 0;
    min-height: 0;
    padding: 8px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .workspace-page__toolbar {
    flex-shrink: 0;
    margin-bottom: 12px;
  }

  .workspace-page__content {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: auto;
  }

  .workspace-page__footer {
    flex-shrink: 0;
    padding: 16px 8px 8px;
    display: flex;
    justify-content: center;
  }

  @media (max-width: 767px) {
    .workspace-page {
      height: auto;
      min-height: 100%;
      padding: 8px;
      gap: 12px;
    }

    .workspace-page__header {
      min-height: 0;
      padding: 10px 12px;
      flex-direction: column;
      gap: 10px;
    }

    .workspace-page__title-row {
      gap: 8px;
    }

    .workspace-page__title-row h2 {
      font-size: 16px;
    }

    .workspace-page__meta,
    .workspace-page__description {
      font-size: 12px;
    }

    .workspace-page__actions {
      width: 100%;
      gap: 8px;
    }

    .workspace-page__actions :deep(.el-button:only-child) {
      width: 100%;
    }

    .workspace-page--floating {
      padding-bottom: 86px;
    }

    .workspace-page__panel {
      flex: 1 0 auto;
      padding: 4px;
      overflow: visible;
    }

    .workspace-page__content {
      overflow: visible;
    }

    .workspace-page__footer {
      padding: 12px 4px 4px;
    }
  }
</style>
