<template>
  <section class="desktop-page" :class="{ 'desktop-page--full': fullHeight }">
    <header v-if="$slots.header || title" class="desktop-page__header">
      <div class="desktop-page__heading">
        <div v-if="$slots.eyebrow" class="desktop-page__eyebrow"><slot name="eyebrow" /></div>
        <h1 v-if="title">{{ title }}</h1>
        <p v-if="description">{{ description }}</p>
        <slot name="header" />
      </div>
      <div v-if="$slots.actions" class="desktop-page__actions"><slot name="actions" /></div>
    </header>

    <div v-if="$slots.toolbar" class="desktop-page__toolbar"><slot name="toolbar" /></div>
    <div class="desktop-page__body"><slot /></div>
  </section>
</template>

<script setup lang="ts">
  withDefaults(
    defineProps<{
      title?: string
      description?: string
      fullHeight?: boolean
    }>(),
    { title: '', description: '', fullHeight: false }
  )
</script>

<style scoped>
  .desktop-page {
    width: min(100%, var(--desktop-content-max));
    min-width: 0;
    margin: 0 auto;
    padding: var(--desktop-page-padding);
  }

  .desktop-page--full {
    min-height: 100%;
    display: flex;
    flex-direction: column;
  }

  .desktop-page__header {
    min-height: 72px;
    margin-bottom: 20px;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 24px;
  }

  .desktop-page__heading {
    min-width: 0;
  }
  .desktop-page__heading h1 {
    margin: 0;
    color: var(--text-primary);
    font-size: 28px;
    line-height: 1.25;
    font-weight: 720;
  }
  .desktop-page__heading p {
    max-width: 720px;
    margin: 8px 0 0;
    color: var(--text-secondary);
    font-size: 14px;
    line-height: 1.6;
  }
  .desktop-page__eyebrow {
    margin-bottom: 6px;
    color: var(--primary-color);
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }
  .desktop-page__actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 10px;
  }
  .desktop-page__toolbar {
    margin-bottom: 16px;
  }
  .desktop-page__body {
    min-width: 0;
  }
  .desktop-page--full .desktop-page__body {
    flex: 1;
    min-height: 0;
  }

  @media (max-width: 991px) {
    .desktop-page__header {
      min-height: 60px;
      margin-bottom: 16px;
    }
    .desktop-page__heading h1 {
      font-size: 24px;
    }
  }
</style>
