import { computed, ref, watch, type Ref } from 'vue'
import type { FileEntry } from '../types'

export function useFileSelection(entries: Ref<FileEntry[]>) {
  const selectedFolderIds = ref<number[]>([])
  const selectedFileIds = ref<string[]>([])
  const anchorKey = ref<string>('')

  const selectedKeys = computed(() => [
    ...selectedFolderIds.value.map(id => `folder:${id}`),
    ...selectedFileIds.value.map(id => `file:${id}`)
  ])
  const selectedKeySet = computed(() => new Set(selectedKeys.value))
  const selectedCount = computed(() => selectedKeys.value.length)
  const selectedEntries = computed(() => entries.value.filter(entry => selectedKeySet.value.has(entry.key)))

  const isSelectedFolder = (id: number) => selectedFolderIds.value.includes(id)
  const isSelectedFile = (id: string) => selectedFileIds.value.includes(id)
  const isSelectedEntry = (entry: FileEntry) => selectedKeySet.value.has(entry.key)

  const applyKeys = (keys: Iterable<string>) => {
    const keySet = new Set(keys)
    selectedFolderIds.value = []
    selectedFileIds.value = []
    for (const entry of entries.value) {
      if (!keySet.has(entry.key)) continue
      if (entry.type === 'folder') selectedFolderIds.value.push(entry.folder.id)
      else selectedFileIds.value.push(entry.file.file_id)
    }
  }

  const setSingle = (entry: FileEntry) => {
    applyKeys([entry.key])
    anchorKey.value = entry.key
  }

  const toggleEntry = (entry: FileEntry) => {
    const next = new Set(selectedKeys.value)
    if (next.has(entry.key)) next.delete(entry.key)
    else next.add(entry.key)
    applyKeys(next)
    anchorKey.value = entry.key
  }

  const selectRange = (entry: FileEntry, additive = false) => {
    const targetIndex = entries.value.findIndex(item => item.key === entry.key)
    const anchorIndex = entries.value.findIndex(item => item.key === anchorKey.value)
    if (targetIndex < 0 || anchorIndex < 0) {
      setSingle(entry)
      return
    }
    const start = Math.min(targetIndex, anchorIndex)
    const end = Math.max(targetIndex, anchorIndex)
    const next = additive ? new Set(selectedKeys.value) : new Set<string>()
    entries.value.slice(start, end + 1).forEach(item => next.add(item.key))
    applyKeys(next)
  }

  const handleEntryClick = (entry: FileEntry, event: MouseEvent) => {
    if (event.shiftKey) selectRange(entry, event.ctrlKey || event.metaKey)
    else if (event.ctrlKey || event.metaKey) toggleEntry(entry)
    else setSingle(entry)
  }

  const prepareContextSelection = (entry: FileEntry) => {
    if (!isSelectedEntry(entry)) setSingle(entry)
  }

  const selectAll = () => {
    applyKeys(entries.value.map(entry => entry.key))
    anchorKey.value = entries.value[entries.value.length - 1]?.key || ''
  }

  const clearSelection = () => {
    selectedFolderIds.value = []
    selectedFileIds.value = []
    anchorKey.value = ''
  }

  const addKeys = (keys: Iterable<string>, additive: boolean) => {
    const next = additive ? new Set(selectedKeys.value) : new Set<string>()
    for (const key of keys) next.add(key)
    applyKeys(next)
  }

  const toggleSelectFolder = (id: number) => {
    const entry = entries.value.find(item => item.type === 'folder' && item.folder.id === id)
    if (entry) toggleEntry(entry)
  }
  const toggleSelectFile = (id: string) => {
    const entry = entries.value.find(item => item.type === 'file' && item.file.file_id === id)
    if (entry) toggleEntry(entry)
  }

  watch(
    () => entries.value.map(entry => entry.key).join('|'),
    (signature, previousSignature) => {
      if (previousSignature && signature !== previousSignature) clearSelection()
    },
    { flush: 'sync' }
  )

  return {
    selectedFolderIds,
    selectedFileIds,
    selectedKeys,
    selectedCount,
    selectedEntries,
    isSelectedFolder,
    isSelectedFile,
    isSelectedEntry,
    applyKeys,
    setSingle,
    toggleEntry,
    selectRange,
    handleEntryClick,
    prepareContextSelection,
    selectAll,
    clearSelection,
    addKeys,
    toggleSelectFolder,
    toggleSelectFile
  }
}
