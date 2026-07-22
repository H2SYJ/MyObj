import type { Component } from 'vue'
import type { FileItem, FolderItem } from '@/types'

export type FileEntry =
  { key: string; type: 'file'; file: FileItem } | { key: string; type: 'folder'; folder: FolderItem }

export interface ContextMenuItem {
  key: string
  label: string
  icon?: Component
  danger?: boolean
  disabled?: boolean
  divided?: boolean
  active?: boolean
}

export const fileEntry = (file: FileItem): FileEntry => ({ key: `file:${file.file_id}`, type: 'file', file })
export const folderEntry = (folder: FolderItem): FileEntry => ({ key: `folder:${folder.id}`, type: 'folder', folder })

export interface FileSelectionCapabilities {
  canOpen: boolean
  canDownload: boolean
  canMove: boolean
  canDelete: boolean
  canPreview: boolean
  canShare: boolean
  canRename: boolean
  canSetPublic: boolean
}

export const getFileSelectionCapabilities = (entries: FileEntry[]): FileSelectionCapabilities => {
  const hasItems = entries.length > 0
  const isSingleFile = entries.length === 1 && entries[0].type === 'file'
  return {
    canOpen: entries.length === 1,
    canDownload: hasItems,
    canMove: hasItems,
    canDelete: hasItems,
    canPreview: isSingleFile,
    canShare: isSingleFile,
    canRename: entries.length === 1,
    canSetPublic: isSingleFile
  }
}
