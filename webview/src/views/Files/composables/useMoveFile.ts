import { moveItems, getDirectories, type DirectoryItem } from '@/api/file'
import { useI18n } from '@/composables'

export function useMoveFile(
  currentDirectoryId: Ref<number>,
  selectedFileIds: Ref<string[]>,
  selectedFolderIds: Ref<number[]>,
  clearSelection: () => void,
  loadFileList: () => Promise<void>
) {
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()

  const showMoveDialog = ref(false)
  const moving = ref(false)
  const targetFolderId = ref<number>()
  const folderTreeData = ref<any[]>([])
  const loadingTree = ref(false)

  const buildFolderTree = async () => {
    loadingTree.value = true
    try {
      const res = await getDirectories()

      if (res.code !== 200 || !res.data) {
        proxy?.$modal.msgError(t('files.getFolderTreeFailed'))
        return
      }

      const directories = res.data

      const pathMap = new Map<number, any>()
      const rawPathMap = new Map(directories.map(directory => [directory.id, directory]))
      const selectedDirSet = new Set(selectedFolderIds.value)
      const isBlockedTarget = (directory: DirectoryItem) => {
        let current: DirectoryItem | undefined = directory
        while (current) {
          if (selectedDirSet.has(current.id)) return true
          current = current.parent_id > 0 ? rawPathMap.get(current.parent_id) : undefined
        }
        return false
      }
      const rootNodes: any[] = []

      directories.forEach(directory => {
        pathMap.set(directory.id, {
          value: directory.id,
          label: directory.name || t('files.rootDir'),
          disabled: isBlockedTarget(directory),
          children: [],
          _raw: directory
        })
      })

      directories.forEach(directory => {
        const node = pathMap.get(directory.id)

        if (!node) return

        if (directory.parent_id > 0) {
          const parentNode = pathMap.get(directory.parent_id)
          if (parentNode) {
            parentNode.children.push(node)
          } else {
            rootNodes.push(node)
          }
        } else {
          rootNodes.push(node)
        }
      })

      const cleanEmptyChildren = (nodes: any[]) => {
        nodes.forEach(node => {
          if (node.children && node.children.length === 0) {
            delete node.children
          } else if (node.children) {
            cleanEmptyChildren(node.children)
          }
        })
      }
      cleanEmptyChildren(rootNodes)

      folderTreeData.value = rootNodes
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('files.getFolderTreeFailed'))
    } finally {
      loadingTree.value = false
    }
  }

  const getFileName = (fileId: string, fileListData: any): string => {
    const file = fileListData.value?.files?.find((f: any) => f.file_id === fileId)
    return file?.file_name || ''
  }

  const handleMoveFile = async () => {
    if (selectedFileIds.value.length === 0 && selectedFolderIds.value.length === 0) {
      proxy?.$modal.msgWarning(t('files.selectItemsFirst'))
      return
    }

    showMoveDialog.value = true
    targetFolderId.value = undefined
    await buildFolderTree()
  }

  const handleConfirmMove = async () => {
    if (!targetFolderId.value) {
      proxy?.$modal.msgWarning(t('files.selectTargetDir'))
      return
    }

    if (targetFolderId.value === currentDirectoryId.value) {
      proxy?.$modal.msgWarning(t('files.sameDir'))
      return
    }

    moving.value = true
    try {
      const count = selectedFileIds.value.length + selectedFolderIds.value.length
      const res = await moveItems({
        file_ids: selectedFileIds.value,
        directory_ids: selectedFolderIds.value,
        target_directory_id: targetFolderId.value
      })
      if (res.code !== 200) {
        proxy?.$modal.msgError(res.message || t('files.moveFileFailed'))
        return
      }

      proxy?.$modal.msgSuccess(t('files.moveItemsSuccess', { count }))
      showMoveDialog.value = false
      clearSelection()
      await loadFileList()
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('files.moveFileFailed'))
    } finally {
      moving.value = false
    }
  }

  return {
    showMoveDialog,
    moving,
    targetFolderId,
    folderTreeData,
    loadingTree,
    getFileName,
    handleMoveFile,
    handleConfirmMove
  }
}
