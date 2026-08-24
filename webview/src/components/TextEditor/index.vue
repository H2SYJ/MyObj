<template>
  <div ref="hostRef" class="text-editor" :class="{ 'is-readonly': readOnly }"></div>
</template>

<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
  import { EditorState } from '@codemirror/state'
  import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, placeholder } from '@codemirror/view'
  import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands'
  import { syntaxHighlighting, defaultHighlightStyle, bracketMatching, indentOnInput } from '@codemirror/language'
  import { autocompletion, closeBrackets, closeBracketsKeymap, completionKeymap } from '@codemirror/autocomplete'
  import { getLanguageExtension } from './languageExtensions'

  interface Props {
    modelValue: string
    language?: string
    readOnly?: boolean
    placeholder?: string
    autofocus?: boolean
  }

  const props = withDefaults(defineProps<Props>(), {
    language: '',
    readOnly: false,
    placeholder: '',
    autofocus: false
  })

  const emit = defineEmits<{
    'update:modelValue': [value: string]
    ready: []
  }>()

  const hostRef = ref<HTMLDivElement | null>(null)
  const view = shallowRef<EditorView | null>(null)

  const buildExtensions = () => {
    return [
      lineNumbers(),
      highlightActiveLineGutter(),
      history(),
      bracketMatching(),
      closeBrackets(),
      autocompletion(),
      indentOnInput(),
      syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
      EditorView.lineWrapping,
      keymap.of([...closeBracketsKeymap, ...defaultKeymap, ...historyKeymap, ...completionKeymap, indentWithTab]),
      getLanguageExtension(props.language),
      EditorState.readOnly.of(props.readOnly),
      EditorState.tabSize.of(2),
      props.placeholder ? placeholder(props.placeholder) : [],
      EditorView.updateListener.of(update => {
        if (update.docChanged) {
          emit('update:modelValue', update.state.doc.toString())
        }
      })
    ]
  }

  onMounted(() => {
    if (!hostRef.value) return
    view.value = new EditorView({
      state: EditorState.create({
        doc: props.modelValue,
        extensions: buildExtensions()
      }),
      parent: hostRef.value
    })
    if (props.autofocus) {
      view.value.focus()
    }
    emit('ready')
  })

  onBeforeUnmount(() => {
    view.value?.destroy()
    view.value = null
  })

  // 外部内容变化（如保存成功回写）时同步到编辑器
  watch(
    () => props.modelValue,
    newValue => {
      const editor = view.value
      if (!editor) return
      const current = editor.state.doc.toString()
      if (current !== newValue) {
        editor.dispatch({
          changes: { from: 0, to: current.length, insert: newValue }
        })
      }
    }
  )

  // 语言/只读状态变化时重配编辑器
  watch([() => props.language, () => props.readOnly], () => {
    const editor = view.value
    if (!editor) return
    editor.dispatch({
      effects: EditorState.reconfigure.of(buildExtensions())
    })
  })
</script>

<style scoped>
  .text-editor {
    height: 100%;
    width: 100%;
    overflow: hidden;
    border: 1px solid var(--border-color, #dcdfe6);
    border-radius: 6px;
    background: var(--bg-color, #ffffff);
  }

  .text-editor :deep(.cm-editor) {
    height: 100%;
    font-size: 14px;
  }

  .text-editor :deep(.cm-editor.cm-focused) {
    outline: none;
  }

  .text-editor :deep(.cm-scroller) {
    font-family: 'JetBrains Mono', 'Fira Code', Monaco, Menlo, Consolas, monospace;
    line-height: 1.6;
  }

  .text-editor :deep(.cm-content) {
    padding: 8px 0;
  }

  .text-editor.is-readonly :deep(.cm-editor) {
    cursor: default;
  }
</style>
