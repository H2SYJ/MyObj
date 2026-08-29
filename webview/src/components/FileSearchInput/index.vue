<template>
  <div ref="rootRef" class="file-search-input" :class="{ 'is-focused': focused }">
    <div class="file-search-input__control" @click="focusInput">
      <el-icon class="file-search-input__icon"><Search /></el-icon>
      <div class="file-search-input__tokens">
        <el-tag
          v-for="tag in tags"
          :key="tag.id"
          class="file-search-input__tag myobj-tag"
          closable
          disable-transitions
          :style="tagStyle(tag.color)"
          @close="removeTag(tag.id)"
        >
          #{{ tag.name }}
        </el-tag>
        <input
          ref="inputRef"
          class="file-search-input__native"
          :value="modelValue"
          :placeholder="tags.length ? '' : placeholder"
          :aria-label="placeholder"
          role="combobox"
          :aria-expanded="dropdownVisible"
          :aria-controls="dropdownVisible ? listboxId : undefined"
          :aria-activedescendant="activeOptionId"
          autocomplete="off"
          @input="handleInput"
          @focus="handleFocus"
          @blur="handleBlur"
          @keydown="handleKeydown"
        />
      </div>
      <button
        v-if="modelValue || tags.length"
        type="button"
        class="file-search-input__clear"
        :aria-label="t('tags.clearSearch')"
        @mousedown.prevent
        @click.stop="clearAll"
      >
        <el-icon><CircleClose /></el-icon>
      </button>
    </div>

    <div v-if="dropdownVisible" :id="listboxId" class="file-search-input__dropdown" role="listbox">
      <template v-if="tagTrigger">
        <div class="file-search-input__heading">{{ t('tags.existingTags') }}</div>
        <div v-if="loading" class="file-search-input__empty">{{ t('tags.loadingSuggestions') }}</div>
        <button
          v-for="(tag, index) in availableTags"
          :id="optionId(index)"
          :key="tag.id"
          type="button"
          class="file-search-input__option"
          :class="{ 'is-active': activeIndex === index }"
          role="option"
          :aria-selected="activeIndex === index"
          @mousedown.prevent
          @mouseenter="activeIndex = index"
          @click="selectTag(tag)"
        >
          <span
            class="file-search-input__dot"
            :style="{ backgroundColor: tag.color || 'var(--el-color-primary)' }"
          ></span>
          <span class="file-search-input__option-name">#{{ tag.name }}</span>
          <small>{{ tag.category_code }}</small>
        </button>
        <div v-if="!loading && availableTags.length === 0" class="file-search-input__empty">
          {{ t('tags.noMatchingTags') }}
        </div>
      </template>
      <template v-else-if="filteredHistory.length">
        <div class="file-search-input__heading">
          <span>{{ t('searchSuggestions.title') }}</span>
          <button v-if="showHistoryActions" type="button" @mousedown.prevent @click="$emit('clear-history')">
            {{ t('searchSuggestions.clearHistory') }}
          </button>
        </div>
        <button
          v-for="(item, index) in filteredHistory"
          :id="optionId(index)"
          :key="item"
          type="button"
          class="file-search-input__option"
          :class="{ 'is-active': activeIndex === index }"
          role="option"
          :aria-selected="activeIndex === index"
          @mousedown.prevent
          @mouseenter="activeIndex = index"
          @click="selectHistory(item)"
        >
          <el-icon><Clock /></el-icon>
          <span class="file-search-input__option-name">{{ item }}</span>
          <el-icon
            v-if="showHistoryActions"
            class="file-search-input__history-delete"
            @click.stop="$emit('delete-history', item)"
          >
            <Close />
          </el-icon>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
  import { getTagSuggestions, type TagSuggestionScope } from '@/api/tag'
  import { useI18n } from '@/composables'
  import type { CompactTag } from '@/types'
  import { getTagStyle } from '@/utils/ui'

  interface SearchSubmitPayload {
    keyword: string
    tags: CompactTag[]
  }

  interface TagTrigger {
    start: number
    end: number
    keyword: string
  }

  const props = withDefaults(
    defineProps<{
      modelValue: string
      tags: CompactTag[]
      scope?: TagSuggestionScope
      placeholder?: string
      history?: string[]
      showHistoryActions?: boolean
    }>(),
    {
      scope: 'user',
      placeholder: '搜索文件',
      history: () => [],
      showHistoryActions: true
    }
  )

  const emit = defineEmits<{
    'update:modelValue': [value: string]
    'update:tags': [value: CompactTag[]]
    submit: [payload: SearchSubmitPayload]
    clear: []
    'clear-history': []
    'delete-history': [keyword: string]
  }>()

  const { t } = useI18n()
  const tagStyle = (color?: string) => getTagStyle(color || '')
  const rootRef = ref<HTMLElement>()
  const inputRef = ref<HTMLInputElement>()
  const focused = ref(false)
  const loading = ref(false)
  const suggestions = ref<CompactTag[]>([])
  const tagTrigger = ref<TagTrigger | null>(null)
  const activeIndex = ref(0)
  const dropdownDismissed = ref(false)
  const listboxId = `file-search-${useId()}`
  let requestSerial = 0
  let debounceTimer: number | null = null
  let blurTimer: number | null = null

  const availableTags = computed(() => {
    const selected = new Set(props.tags.map(tag => tag.id))
    return suggestions.value.filter(tag => !selected.has(tag.id))
  })
  const filteredHistory = computed(() => {
    if (tagTrigger.value) return []
    const keyword = props.modelValue.trim().toLowerCase()
    const source = keyword ? props.history.filter(item => item.toLowerCase().includes(keyword)) : props.history
    return source.slice(0, 5)
  })
  const dropdownVisible = computed(
    () => focused.value && !dropdownDismissed.value && Boolean(tagTrigger.value || filteredHistory.value.length > 0)
  )
  const activeOptionId = computed(() => (dropdownVisible.value ? optionId(activeIndex.value) : undefined))

  const optionId = (index: number) => `${listboxId}-option-${index}`

  const detectTagTrigger = (value: string, caret: number): TagTrigger | null => {
    const prefix = value.slice(0, caret)
    const match = prefix.match(/(^|\s)#([^\s#]*)$/u)
    if (!match || match.index === undefined) return null
    const start = match.index + match[1].length
    return { start, end: caret, keyword: match[2] }
  }

  const cancelDebounce = () => {
    if (debounceTimer !== null) {
      window.clearTimeout(debounceTimer)
      debounceTimer = null
    }
  }

  const loadSuggestions = (trigger: TagTrigger) => {
    cancelDebounce()
    const serial = ++requestSerial
    loading.value = true
    debounceTimer = window.setTimeout(async () => {
      try {
        const response = await getTagSuggestions({ keyword: trigger.keyword, scope: props.scope, limit: 50 })
        if (serial === requestSerial && response.code === 200) {
          suggestions.value = response.data || []
          activeIndex.value = 0
        }
      } catch {
        if (serial === requestSerial) suggestions.value = []
      } finally {
        if (serial === requestSerial) loading.value = false
      }
    }, 200)
  }

  const refreshTrigger = (
    value = props.modelValue,
    caret = inputRef.value?.selectionStart ?? props.modelValue.length
  ) => {
    const input = inputRef.value
    const trigger = detectTagTrigger(value, input?.selectionStart ?? caret)
    tagTrigger.value = trigger
    if (trigger) {
      loadSuggestions(trigger)
    } else {
      cancelDebounce()
      requestSerial++
      loading.value = false
      suggestions.value = []
      activeIndex.value = 0
    }
  }

  const handleInput = (event: Event) => {
    const input = event.target as HTMLInputElement
    dropdownDismissed.value = false
    emit('update:modelValue', input.value)
    refreshTrigger(input.value, input.selectionStart ?? input.value.length)
  }

  const focusInput = () => inputRef.value?.focus()
  const handleFocus = () => {
    if (blurTimer !== null) window.clearTimeout(blurTimer)
    focused.value = true
    dropdownDismissed.value = false
    nextTick(refreshTrigger)
  }
  const handleBlur = () => {
    blurTimer = window.setTimeout(() => {
      focused.value = false
      tagTrigger.value = null
    }, 120)
  }

  const selectTag = (tag: CompactTag) => {
    const trigger = tagTrigger.value
    if (!trigger) return
    const nextKeyword = props.modelValue.slice(0, trigger.start) + props.modelValue.slice(trigger.end)
    const nextTags = props.tags.some(item => item.id === tag.id) ? props.tags : [...props.tags, tag]
    emit('update:modelValue', nextKeyword)
    emit('update:tags', nextTags)
    tagTrigger.value = null
    suggestions.value = []
    nextTick(() => {
      focusInput()
      inputRef.value?.setSelectionRange(trigger.start, trigger.start)
    })
  }

  const removeTag = (tagID: string) =>
    emit(
      'update:tags',
      props.tags.filter(tag => tag.id !== tagID)
    )
  const submit = () => emit('submit', { keyword: props.modelValue.trim(), tags: props.tags })
  const clearAll = () => {
    emit('update:modelValue', '')
    emit('update:tags', [])
    emit('clear')
    suggestions.value = []
    tagTrigger.value = null
    nextTick(focusInput)
  }
  const selectHistory = (keyword: string) => {
    emit('update:modelValue', keyword)
    emit('submit', { keyword, tags: props.tags })
  }

  const handleKeydown = (event: KeyboardEvent) => {
    const itemsLength = tagTrigger.value ? availableTags.value.length : filteredHistory.value.length
    if (dropdownVisible.value && itemsLength > 0 && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      const offset = event.key === 'ArrowDown' ? 1 : -1
      activeIndex.value = (activeIndex.value + offset + itemsLength) % itemsLength
      return
    }
    if (event.key === 'Escape' && dropdownVisible.value) {
      event.preventDefault()
      cancelDebounce()
      requestSerial++
      tagTrigger.value = null
      suggestions.value = []
      dropdownDismissed.value = true
      return
    }
    if (event.key === 'Enter') {
      event.preventDefault()
      if (tagTrigger.value && availableTags.value.length > 0) {
        selectTag(availableTags.value[Math.min(activeIndex.value, availableTags.value.length - 1)])
      } else {
        submit()
      }
      return
    }
    if (event.key === 'Backspace' && !props.modelValue && props.tags.length > 0) {
      removeTag(props.tags[props.tags.length - 1].id)
    }
  }

  watch(
    () => [props.modelValue, props.scope],
    () => {
      if (focused.value) nextTick(refreshTrigger)
    }
  )

  onBeforeUnmount(() => {
    cancelDebounce()
    if (blurTimer !== null) window.clearTimeout(blurTimer)
  })

  defineExpose({ focus: focusInput, submit })
</script>

<style scoped>
  .file-search-input {
    position: relative;
    width: 100%;
    min-width: 0;
  }
  .file-search-input__control {
    min-height: 40px;
    padding: 4px 10px;
    display: flex;
    align-items: center;
    gap: 8px;
    border: 1px solid var(--el-border-color);
    border-radius: var(--el-border-radius-base);
    background: var(--el-fill-color-blank);
    box-shadow: 0 0 0 1px transparent inset;
    cursor: text;
    transition:
      border-color 0.2s,
      box-shadow 0.2s;
  }
  .file-search-input.is-focused .file-search-input__control {
    border-color: var(--el-color-primary);
    box-shadow: 0 0 0 1px var(--el-color-primary) inset;
  }
  .file-search-input__icon,
  .file-search-input__clear {
    flex: 0 0 auto;
    color: var(--el-text-color-placeholder);
  }
  .file-search-input__tokens {
    min-width: 0;
    flex: 1;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .file-search-input__tokens::-webkit-scrollbar {
    display: none;
  }
  .file-search-input__tag {
    flex: 0 0 auto;
    color: var(--el-tag-text-color, var(--el-text-color-primary));
  }
  .file-search-input__native {
    min-width: 90px;
    flex: 1 0 90px;
    padding: 0;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--el-text-color-primary);
    font: inherit;
  }
  .file-search-input__native::placeholder {
    color: var(--el-text-color-placeholder);
  }
  .file-search-input__clear {
    padding: 2px;
    display: inline-flex;
    border: 0;
    background: transparent;
    cursor: pointer;
  }
  .file-search-input__dropdown {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    right: 0;
    z-index: 2200;
    max-height: 320px;
    overflow-y: auto;
    padding: 6px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 10px;
    background: var(--el-bg-color-overlay);
    box-shadow: var(--el-box-shadow-light);
  }
  .file-search-input__heading {
    padding: 7px 9px;
    display: flex;
    justify-content: space-between;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .file-search-input__heading button {
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--el-color-primary);
    cursor: pointer;
  }
  .file-search-input__option {
    width: 100%;
    padding: 9px 10px;
    display: flex;
    align-items: center;
    gap: 8px;
    border: 0;
    border-radius: 7px;
    background: transparent;
    color: var(--el-text-color-regular);
    cursor: pointer;
    text-align: left;
  }
  .file-search-input__option:hover,
  .file-search-input__option.is-active {
    background: var(--el-fill-color-light);
  }
  .file-search-input__dot {
    width: 8px;
    height: 8px;
    flex: 0 0 auto;
    border-radius: 50%;
  }
  .file-search-input__option-name {
    min-width: 0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .file-search-input__option small {
    color: var(--el-text-color-secondary);
  }
  .file-search-input__history-delete {
    color: var(--el-text-color-placeholder);
  }
  .file-search-input__empty {
    padding: 18px 10px;
    color: var(--el-text-color-secondary);
    text-align: center;
  }
</style>
