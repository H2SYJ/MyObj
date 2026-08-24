/**
 * CodeMirror 6 语言扩展映射：将 getCodeLanguage 返回的语言名映射为对应的语法高亮扩展。
 * 未显式安装的语言通过 @codemirror/language-data 的 legacy mode 兜底（bash/ruby/swift/scss/less/powershell 等）。
 */
import type { Extension } from '@codemirror/state'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { html } from '@codemirror/lang-html'
import { vue } from '@codemirror/lang-vue'
import { css } from '@codemirror/lang-css'
import { xml } from '@codemirror/lang-xml'
import { yaml } from '@codemirror/lang-yaml'
import { python } from '@codemirror/lang-python'
import { java } from '@codemirror/lang-java'
import { cpp } from '@codemirror/lang-cpp'
import { go } from '@codemirror/lang-go'
import { rust } from '@codemirror/lang-rust'
import { php } from '@codemirror/lang-php'
import { sql } from '@codemirror/lang-sql'
import { languages } from '@codemirror/language-data'

const languageFactories: Record<string, () => Extension> = {
  javascript: () => javascript(),
  typescript: () => javascript({ typescript: true }),
  jsx: () => javascript({ jsx: true }),
  tsx: () => javascript({ typescript: true, jsx: true }),
  json: () => json(),
  html: () => html(),
  vue: () => vue(),
  css: () => css(),
  // scss/less/ruby/swift/bash/powershell 等未显式安装的语言由 language-data 兜底
  xml: () => xml(),
  yaml: () => yaml(),
  python: () => python(),
  java: () => java(),
  cpp: () => cpp(),
  c: () => cpp(),
  go: () => go(),
  rust: () => rust(),
  php: () => php(),
  sql: () => sql()
}

/**
 * 根据语言名获取 CodeMirror 语言扩展。
 * @param language 语言名（getCodeLanguage 的返回值）
 */
export const getLanguageExtension = (language: string): Extension => {
  const key = (language || '').toLowerCase()
  const factory = languageFactories[key]
  if (factory) {
    const ext = factory()
    if (ext) return ext
  }
  // 兜底：按语言名在 language-data 中查找 legacy mode
  const desc = languages.find(item => item.name.toLowerCase() === key)
  if (desc?.support) return desc.support
  // 未知语言：纯文本
  return []
}
