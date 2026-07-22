import { moveItems, getVirtualPathTree } from '@/api/file'
import { useI18n } from '@/composables'

export function useMoveFile(
  currentPath: Ref<string>,
  selectedFileIds: Ref<string[]>,
  selectedFolderIds: Ref<number[]>,
  clearSelection: () => void,
  loadFileList: () => Promise<void>
) {
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()

  const showMoveDialog = ref(false)
  const moving = ref(false)
  const targetFolderId = ref<string>('')
  const folderTreeData = ref<any[]>([])
  const loadingTree = ref(false)

  const buildFolderTree = async () => {
    loadingTree.value = true
    try {
      const res = await getVirtualPathTree()

      if (res.code !== 200 || !res.data) {
        proxy?.$modal.msgError(t('files.getFolderTreeFailed'))
        return
      }

      const virtualPaths = res.data as Array<{
        id: number
        path: string
        parent_level: string
        is_dir: boolean
      }>

      const pathMap = new Map<string, any>()
      const rawPathMap = new Map(virtualPaths.map(path => [path.id, path]))
      const selectedDirSet = new Set(selectedFolderIds.value)
      const isBlockedTarget = (path: (typeof virtualPaths)[number]) => {
        let current: typeof path | undefined = path
        while (current) {
          if (selectedDirSet.has(current.id)) return true
          const parentID = Number(current.parent_level)
          current = parentID > 0 ? rawPathMap.get(parentID) : undefined
        }
        return false
      }
      const rootNodes: any[] = []

      virtualPaths.forEach(vp => {
        const nodeId = String(vp.id)
        pathMap.set(nodeId, {
          value: nodeId,
          label: vp.path.replace(/^\//, '') || t('files.rootDir'),
          disabled: isBlockedTarget(vp),
          children: [],
          _raw: vp
        })
      })

      virtualPaths.forEach(vp => {
        const nodeId = String(vp.id)
        const node = pathMap.get(nodeId)

        if (!node) return

        if (vp.parent_level && vp.parent_level !== '' && vp.parent_level !== '0') {
          const parentNode = pathMap.get(vp.parent_level)
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
    targetFolderId.value = ''
    await buildFolderTree()
  }

  const handleConfirmMove = async () => {
    if (!targetFolderId.value) {
      proxy?.$modal.msgWarning(t('files.selectTargetDir'))
      return
    }

    if (targetFolderId.value === currentPath.value) {
      proxy?.$modal.msgWarning(t('files.sameDir'))
      return
    }

    moving.value = true
    try {
      const count = selectedFileIds.value.length + selectedFolderIds.value.length
      const res = await moveItems({
        file_ids: selectedFileIds.value,
        dir_ids: selectedFolderIds.value,
        target_path: targetFolderId.value
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
