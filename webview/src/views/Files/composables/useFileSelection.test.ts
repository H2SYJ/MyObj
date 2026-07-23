import { describe, expect, it } from 'vitest'
import { ref } from 'vue'
import type { FileItem, FolderItem } from '@/types'
import { fileEntry, folderEntry, getFileSelectionCapabilities, type FileEntry } from '../types'
import { useFileSelection } from './useFileSelection'

const folder = (id: number): FolderItem => ({
  id,
  name: `目录 ${id}`,
  parent_id: 1,
  absolute_path: `/目录 ${id}`,
  created_at: '2026-01-01T00:00:00Z'
})

const file = (id: string): FileItem => ({
  file_id: id,
  file_name: `文件 ${id}`,
  file_size: 1,
  mime_type: 'text/plain',
  is_enc: false,
  has_thumbnail: false,
  public: false,
  created_at: '2026-01-01T00:00:00Z'
})

const createEntries = (): FileEntry[] => [
  folderEntry(folder(1)),
  folderEntry(folder(2)),
  fileEntry(file('a')),
  fileEntry(file('b'))
]
const click = (modifiers: Partial<MouseEvent> = {}) => modifiers as MouseEvent

describe('useFileSelection', () => {
  it('支持单选与 Ctrl/Meta 切换', () => {
    const entries = ref(createEntries())
    const selection = useFileSelection(entries)
    selection.handleEntryClick(entries.value[0], click())
    expect(selection.selectedKeys.value).toEqual(['folder:1'])

    selection.handleEntryClick(entries.value[2], click({ ctrlKey: true }))
    expect(new Set(selection.selectedKeys.value)).toEqual(new Set(['folder:1', 'file:a']))
    selection.handleEntryClick(entries.value[0], click({ metaKey: true }))
    expect(selection.selectedKeys.value).toEqual(['file:a'])
  })

  it('支持 Shift 连选和 Ctrl+Shift 追加连选', () => {
    const entries = ref(createEntries())
    const selection = useFileSelection(entries)
    selection.setSingle(entries.value[0])
    selection.handleEntryClick(entries.value[2], click({ shiftKey: true }))
    expect(new Set(selection.selectedKeys.value)).toEqual(new Set(['folder:1', 'folder:2', 'file:a']))

    selection.setSingle(entries.value[3])
    selection.handleEntryClick(entries.value[2], click({ shiftKey: true, ctrlKey: true }))
    expect(new Set(selection.selectedKeys.value)).toEqual(new Set(['file:a', 'file:b']))
  })

  it('支持全选、框选替换与追加', () => {
    const entries = ref(createEntries())
    const selection = useFileSelection(entries)
    selection.selectAll()
    expect(selection.selectedCount.value).toBe(4)
    selection.addKeys(['folder:2'], false)
    expect(selection.selectedKeys.value).toEqual(['folder:2'])
    selection.addKeys(['file:b'], true)
    expect(new Set(selection.selectedKeys.value)).toEqual(new Set(['folder:2', 'file:b']))
  })

  it('右击已选项保留选区，右击未选项切换为单选', () => {
    const entries = ref(createEntries())
    const selection = useFileSelection(entries)
    selection.applyKeys(['folder:1', 'file:a'])
    selection.prepareContextSelection(entries.value[2])
    expect(new Set(selection.selectedKeys.value)).toEqual(new Set(['folder:1', 'file:a']))
    selection.prepareContextSelection(entries.value[3])
    expect(selection.selectedKeys.value).toEqual(['file:b'])
  })

  it('当前页数据变化时清空隐藏选区', () => {
    const entries = ref(createEntries())
    const selection = useFileSelection(entries)
    selection.selectAll()
    entries.value = [fileEntry(file('next'))]
    expect(selection.selectedCount.value).toBe(0)
  })

  it('按文件、目录和混合选区计算共同菜单能力', () => {
    const entries = createEntries()
    expect(getFileSelectionCapabilities([entries[2]])).toMatchObject({
      canDownload: true,
      canShare: true,
      canRename: true,
      canSetPublic: true
    })
    expect(getFileSelectionCapabilities([entries[0]])).toMatchObject({
      canDownload: true,
      canShare: false,
      canRename: true,
      canSetPublic: false
    })
    expect(getFileSelectionCapabilities([entries[0], entries[2]])).toMatchObject({
      canDownload: true,
      canMove: true,
      canDelete: true,
      canShare: false,
      canRename: false
    })
  })
})
