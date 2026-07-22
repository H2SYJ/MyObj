<template>
  <Teleport to="body">
    <Transition name="context-menu">
      <div
        v-if="visible"
        ref="menuRef"
        class="file-context-menu"
        :style="menuStyle"
        role="menu"
        tabindex="-1"
        @keydown="handleKeydown"
        @contextmenu.prevent
      >
        <div v-if="title" class="context-menu-title">{{ title }}</div>
        <button
          v-for="(item, index) in items"
          :key="item.key"
          :ref="element => setItemRef(element, index)"
          type="button"
          role="menuitem"
          class="context-menu-item"
          :class="{ danger: item.danger, divided: item.divided, active: item.active }"
          :disabled="item.disabled"
          @click="selectItem(item)"
        >
          <el-icon v-if="item.icon"><component :is="item.icon" /></el-icon>
          <span>{{ item.label }}</span>
          <el-icon v-if="item.active" class="active-mark"><Check /></el-icon>
        </button>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
  import type { ComponentPublicInstance, CSSProperties } from 'vue'
  import type { ContextMenuItem } from '../types'

  const props = defineProps<{
    visible: boolean
    x: number
    y: number
    title?: string
    items: ContextMenuItem[]
  }>()

  const emit = defineEmits<{
    action: [key: string]
    close: []
  }>()

  const menuRef = ref<HTMLElement>()
  const itemRefs = ref<HTMLElement[]>([])
  const position = reactive({ x: 0, y: 0 })
  const menuStyle = computed<CSSProperties>(() => ({ left: `${position.x}px`, top: `${position.y}px` }))

  const setItemRef = (element: Element | ComponentPublicInstance | null, index: number) => {
    if (element instanceof HTMLElement) itemRefs.value[index] = element
  }

  const updatePosition = async () => {
    position.x = props.x
    position.y = props.y
    await nextTick()
    const rect = menuRef.value?.getBoundingClientRect()
    if (!rect) return
    position.x = Math.max(8, Math.min(props.x, window.innerWidth - rect.width - 8))
    position.y = Math.max(8, Math.min(props.y, window.innerHeight - rect.height - 8))
    menuRef.value?.focus()
  }

  const selectItem = (item: ContextMenuItem) => {
    if (item.disabled) return
    emit('action', item.key)
  }

  const handleKeydown = (event: KeyboardEvent) => {
    const enabled = itemRefs.value.filter(item => !item.hasAttribute('disabled'))
    if (event.key === 'Escape') {
      event.preventDefault()
      emit('close')
      return
    }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const current = document.activeElement as HTMLElement
    let index = enabled.indexOf(current)
    if (event.key === 'Home') index = 0
    else if (event.key === 'End') index = enabled.length - 1
    else if (event.key === 'ArrowDown') index = (index + 1 + enabled.length) % enabled.length
    else index = (index - 1 + enabled.length) % enabled.length
    enabled[index]?.focus()
  }

  const closeOnOutside = (event: Event) => {
    if (!menuRef.value?.contains(event.target as Node)) emit('close')
  }
  const closeMenu = () => emit('close')
  const closeOnScroll = (event: Event) => {
    if (!menuRef.value?.contains(event.target as Node)) emit('close')
  }

  watch(
    () => props.visible,
    visible => {
      itemRefs.value = []
      if (visible) updatePosition()
    }
  )
  watch(
    () => [props.x, props.y],
    () => props.visible && updatePosition()
  )

  onMounted(() => {
    document.addEventListener('pointerdown', closeOnOutside, true)
    window.addEventListener('resize', closeMenu)
    window.addEventListener('scroll', closeOnScroll, true)
  })
  onBeforeUnmount(() => {
    document.removeEventListener('pointerdown', closeOnOutside, true)
    window.removeEventListener('resize', closeMenu)
    window.removeEventListener('scroll', closeOnScroll, true)
  })
</script>

<style scoped>
  .file-context-menu {
    position: fixed;
    z-index: 4000;
    min-width: 208px;
    max-height: min(520px, calc(100vh - 16px));
    overflow-y: auto;
    padding: 6px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 12px;
    background: var(--el-bg-color-overlay);
    box-shadow: var(--el-box-shadow-light);
    outline: none;
    backdrop-filter: blur(18px);
  }

  .context-menu-title {
    padding: 8px 10px 6px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    font-weight: 600;
  }

  .context-menu-item {
    width: 100%;
    min-height: 36px;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 10px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: var(--el-text-color-primary);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .context-menu-item:hover,
  .context-menu-item:focus-visible,
  .context-menu-item.active {
    background: var(--el-fill-color-light);
    outline: none;
  }

  .context-menu-item.divided {
    margin-top: 6px;
    border-top: 1px solid var(--el-border-color-lighter);
    border-radius: 0 0 8px 8px;
    padding-top: 12px;
  }

  .context-menu-item.danger {
    color: var(--el-color-danger);
  }

  .context-menu-item:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .active-mark {
    margin-left: auto;
    color: var(--el-color-primary);
  }

  .context-menu-enter-active,
  .context-menu-leave-active {
    transition:
      opacity 0.12s ease,
      transform 0.12s ease;
    transform-origin: top left;
  }

  .context-menu-enter-from,
  .context-menu-leave-to {
    opacity: 0;
    transform: scale(0.96);
  }
</style>
