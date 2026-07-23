<template>
  <div class="segmented-control" :class="{ 'segmented-control--stretch': stretch }">
    <div class="segmented-control__track" :role="role" :aria-label="ariaLabel">
      <button
        v-for="(item, index) in items"
        :key="item.value"
        type="button"
        class="segmented-control__item"
        :class="{ 'is-active': item.value === modelValue }"
        :role="role === 'tablist' ? 'tab' : 'radio'"
        :aria-selected="role === 'tablist' ? item.value === modelValue : undefined"
        :aria-checked="role === 'radiogroup' ? item.value === modelValue : undefined"
        :tabindex="item.value === modelValue ? 0 : -1"
        :disabled="item.disabled"
        @click="selectItem(item.value)"
        @keydown="handleKeydown($event, index)"
      >
        <el-icon v-if="item.icon" class="segmented-control__icon" :size="16">
          <component :is="item.icon" />
        </el-icon>
        <span class="segmented-control__label">{{ item.label }}</span>
        <span v-if="item.badge !== undefined" class="segmented-control__badge">{{ item.badge }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
  import type { Component } from 'vue'

  interface SegmentedControlItem {
    value: string
    label: string
    icon?: Component
    badge?: string | number
    disabled?: boolean
  }

  const props = withDefaults(
    defineProps<{
      modelValue: string
      items: SegmentedControlItem[]
      ariaLabel: string
      role?: 'tablist' | 'radiogroup'
      stretch?: boolean
    }>(),
    {
      role: 'radiogroup',
      stretch: false
    }
  )

  const emit = defineEmits<{
    'update:modelValue': [value: string]
    change: [value: string]
  }>()

  const selectItem = (value: string) => {
    if (value === props.modelValue) {
      return
    }
    emit('update:modelValue', value)
    emit('change', value)
  }

  const handleKeydown = (event: KeyboardEvent, currentIndex: number) => {
    const availableItems = props.items.map((item, index) => ({ item, index })).filter(({ item }) => !item.disabled)
    const availableIndex = availableItems.findIndex(({ index }) => index === currentIndex)
    if (availableIndex === -1) {
      return
    }

    let nextAvailableIndex: number
    switch (event.key) {
      case 'ArrowRight':
      case 'ArrowDown':
        nextAvailableIndex = (availableIndex + 1) % availableItems.length
        break
      case 'ArrowLeft':
      case 'ArrowUp':
        nextAvailableIndex = (availableIndex - 1 + availableItems.length) % availableItems.length
        break
      case 'Home':
        nextAvailableIndex = 0
        break
      case 'End':
        nextAvailableIndex = availableItems.length - 1
        break
      default:
        return
    }

    event.preventDefault()
    const nextItem = availableItems[nextAvailableIndex]
    selectItem(nextItem.item.value)
    const buttons = (event.currentTarget as HTMLElement).parentElement?.querySelectorAll<HTMLButtonElement>(
      '.segmented-control__item:not(:disabled)'
    )
    buttons?.[nextAvailableIndex]?.focus()
  }
</script>

<style scoped>
  .segmented-control {
    width: max-content;
    max-width: 100%;
    padding: 2px;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .segmented-control::-webkit-scrollbar {
    display: none;
  }

  .segmented-control__track {
    width: max-content;
    min-width: 100%;
    padding: 4px;
    display: flex;
    align-items: center;
    gap: 4px;
    border: 1px solid color-mix(in srgb, var(--desktop-border, var(--el-border-color-lighter)) 78%, transparent);
    border-radius: 14px;
    background: color-mix(in srgb, var(--desktop-fill, var(--el-fill-color-light)) 88%, var(--card-bg));
    box-shadow: inset 0 1px 2px rgba(15, 23, 42, 0.04);
  }

  .segmented-control__item {
    min-width: 0;
    height: 40px;
    padding: 0 15px;
    border: 1px solid transparent;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    color: var(--el-text-color-regular);
    background: transparent;
    font: inherit;
    font-size: 14px;
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
    cursor: pointer;
    transition:
      color 180ms ease,
      background-color 180ms ease,
      border-color 180ms ease,
      box-shadow 180ms ease,
      transform 180ms ease;
  }

  .segmented-control__item:not(.is-active):not(:disabled):hover {
    color: var(--text-primary);
    background: color-mix(in srgb, var(--card-bg) 72%, transparent);
  }

  .segmented-control__item:not(.is-active):not(:disabled):active {
    transform: scale(0.98);
  }

  .segmented-control__item.is-active {
    color: #ffffff;
    border-color: color-mix(in srgb, var(--primary-color) 82%, #ffffff);
    background: linear-gradient(135deg, var(--primary-color), color-mix(in srgb, var(--primary-color) 78%, #7aa7ff));
    box-shadow:
      0 1px 2px rgba(15, 23, 42, 0.08),
      0 6px 14px color-mix(in srgb, var(--primary-color) 24%, transparent);
  }

  .segmented-control__item:focus-visible {
    outline: 3px solid color-mix(in srgb, var(--primary-color) 24%, transparent);
    outline-offset: 2px;
  }

  .segmented-control__item:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .segmented-control__icon {
    flex: 0 0 auto;
  }

  .segmented-control__label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .segmented-control__badge {
    min-width: 20px;
    height: 20px;
    padding: 0 6px;
    border-radius: 999px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    color: var(--el-text-color-secondary);
    background: color-mix(in srgb, var(--card-bg) 82%, transparent);
    font-size: 11px;
    font-weight: 700;
  }

  .segmented-control__item.is-active .segmented-control__badge {
    color: #ffffff;
    background: rgba(255, 255, 255, 0.2);
  }

  .segmented-control--stretch {
    width: min(100%, 420px);
  }

  .segmented-control--stretch .segmented-control__track {
    width: 100%;
  }

  .segmented-control--stretch .segmented-control__item {
    flex: 1 1 0;
  }

  html.dark .segmented-control__track {
    border-color: var(--desktop-border, var(--el-border-color));
    background: color-mix(in srgb, var(--desktop-fill, var(--el-fill-color)) 90%, #000000);
    box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.2);
  }

  html.dark .segmented-control__item:not(.is-active):not(:disabled):hover {
    color: var(--el-text-color-primary);
    background: rgba(255, 255, 255, 0.06);
  }

  @media (max-width: 767px) {
    .segmented-control {
      width: 100%;
    }

    .segmented-control__item {
      height: 38px;
      padding: 0 13px;
      font-size: 13px;
    }

    .segmented-control--stretch {
      width: 100%;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .segmented-control__item {
      transition-duration: 0.01ms;
    }
  }
</style>
