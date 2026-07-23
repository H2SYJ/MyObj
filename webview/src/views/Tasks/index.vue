<template>
  <WorkspacePage :title="t('route.tasks')">
    <template #icon>
      <el-icon :size="24">
        <List />
      </el-icon>
    </template>
    <template #meta>{{ t('tasks.taskCount', { count: activeTaskTotal }) }}</template>
    <template v-if="activeTab === 'upload'" #actions>
      <el-button
        v-if="uploadTotal > 0"
        type="danger"
        icon="Delete"
        :loading="cleanLoading"
        @click="clearAllUploadTasks"
      >
        {{ t('tasks.clearAll') }}
      </el-button>
      <el-button type="warning" icon="View" @click="showExpiredDialog = true">
        {{ t('tasks.viewExpired') }}
        <el-badge v-if="expiredTaskCount > 0" :value="expiredTaskCount" class="expired-badge" />
      </el-button>
    </template>

    <template #toolbar>
      <SegmentedControl
        v-model="activeTab"
        :items="taskTabItems"
        :aria-label="t('route.tasks')"
        role="tablist"
        stretch
      />
    </template>

    <div v-if="activeTab === 'upload'" class="task-panel" role="tabpanel" :aria-label="t('tasks.upload')">
      <UploadTaskTable
        :tasks="uploadTasks"
        :loading="uploadLoading"
        :current-page="uploadCurrentPage"
        :page-size="uploadPageSize"
        :total="uploadTotal"
        :has-more="uploadHasMore"
        @pause="pauseUpload"
        @resume="resumeUpload"
        @cancel="cancelUpload"
        @delete="deleteUpload"
        @retry="retryFinalize"
        @pagination="handleUploadPagination"
        @load-more="loadMoreUploadTasks"
      />
    </div>

    <div v-else class="task-panel" role="tabpanel" :aria-label="t('tasks.download')">
      <DownloadTaskTable
        :tasks="downloadTasks"
        :loading="downloadLoading"
        :current-page="downloadCurrentPage"
        :page-size="downloadPageSize"
        :total="downloadTotal"
        :has-more="downloadHasMore"
        @pause="pauseDownloadTask"
        @resume="resumeDownloadTask"
        @cancel="cancelDownload"
        @delete="deleteDownloadTask"
        @pagination="handleDownloadPagination"
        @load-more="loadMoreDownloadTasks"
      />
    </div>

    <template #overlays>
      <ExpiredTasksDialog v-model="showExpiredDialog" @refresh="handleExpiredRefresh" />
    </template>
  </WorkspacePage>
</template>

<script setup lang="ts">
  import { Download, Upload } from '@element-plus/icons-vue'
  import { uploadTaskManager } from '@/utils/file/uploadTaskManager'
  import { syncBackendTaskToFrontend, type BackendUploadTask } from '@/utils/file/uploadTaskSync'
  import { useI18n } from '@/composables'
  import UploadTaskTable from './components/UploadTaskTable.vue'
  import DownloadTaskTable from './components/DownloadTaskTable.vue'
  import ExpiredTasksDialog from './components/ExpiredTasksDialog.vue'
  import SegmentedControl from '@/components/SegmentedControl/index.vue'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import { useUploadTasks } from './composables/useUploadTasks'
  import { useDownloadTasks } from './composables/useDownloadTasks'
  import { taskEventClient, type TaskEvent } from '@/utils/taskEvents'

  const { t } = useI18n()

  const route = useRoute()
  const router = useRouter()

  const activeTab = ref<string>((route.query.tab as string) || 'upload')
  let eventRefreshTimer: number | null = null
  let eventRefreshRunning = false
  let eventRefreshStopped = false
  let pendingUploadRefresh = false
  let pendingDownloadRefresh = false

  // 监听路由查询参数变化
  watch(
    () => route.query.tab,
    newTab => {
      if (newTab && (newTab === 'upload' || newTab === 'download')) {
        activeTab.value = newTab as string
      }
    }
  )

  watch(activeTab, tab => {
    if (route.query.tab === tab) {
      return
    }
    router.replace({ query: { ...route.query, tab } })
  })

  // 使用 composables
  const {
    uploadTasks,
    uploadLoading,
    cleanLoading,
    expiredTaskCount,
    currentPage: uploadCurrentPage,
    pageSize: uploadPageSize,
    total: uploadTotal,
    hasMore: uploadHasMore,
    loadUploadTasks,
    refreshLocalUploadTasks,
    getExpiredTaskCount,
    pauseUpload,
    resumeUpload,
    cancelUpload,
    deleteUpload,
    retryFinalize,
    clearAllUploadTasks,
    handlePagination: handleUploadPagination,
    loadMore: loadMoreUploadTasks
  } = useUploadTasks()

  const showExpiredDialog = ref(false)

  // 处理过期任务刷新
  const handleExpiredRefresh = () => {
    loadUploadTasks()
    getExpiredTaskCount()
  }

  const {
    downloadTasks,
    downloadLoading,
    currentPage: downloadCurrentPage,
    pageSize: downloadPageSize,
    total: downloadTotal,
    hasMore: downloadHasMore,
    loadDownloadTasks,
    loadMore: loadMoreDownloadTasks,
    refreshDownloadTasks,
    applyDownloadTaskEvent,
    cancelDownload,
    deleteDownloadTask,
    pauseDownloadTask,
    resumeDownloadTask
  } = useDownloadTasks()

  const taskTabItems = computed(() => [
    { value: 'upload', label: t('tasks.upload'), icon: Upload, badge: uploadTotal.value },
    { value: 'download', label: t('tasks.download'), icon: Download, badge: downloadTotal.value }
  ])

  const activeTaskTotal = computed(() => (activeTab.value === 'upload' ? uploadTotal.value : downloadTotal.value))

  // 处理下载任务分页
  const handleDownloadPagination = ({ page, limit }: { page: number; limit: number }) => {
    loadDownloadTasks(true, page, limit)
  }

  // 订阅上传任务更新
  let unsubscribe: (() => void) | null = null
  let unsubscribeDownloadEvents: (() => void) | null = null
  let unsubscribeUploadEvents: (() => void) | null = null
  let unsubscribeSyncEvents: (() => void) | null = null

  const scheduleTaskRefresh = (domain: 'upload' | 'download' | 'all') => {
    if (eventRefreshStopped) return
    if (domain === 'upload' || domain === 'all') pendingUploadRefresh = true
    if (domain === 'download' || domain === 'all') pendingDownloadRefresh = true
    if (eventRefreshTimer !== null || eventRefreshRunning) return
    eventRefreshTimer = window.setTimeout(async () => {
      eventRefreshTimer = null
      if (eventRefreshRunning) return
      const refreshUpload = pendingUploadRefresh
      const refreshDownload = pendingDownloadRefresh
      pendingUploadRefresh = false
      pendingDownloadRefresh = false
      eventRefreshRunning = true
      try {
        const requests: Promise<unknown>[] = []
        if (refreshUpload) {
          requests.push(loadUploadTasks(false), getExpiredTaskCount())
        }
        if (refreshDownload) requests.push(refreshDownloadTasks(false))
        await Promise.all(requests)
      } finally {
        eventRefreshRunning = false
        if (pendingUploadRefresh && pendingDownloadRefresh) scheduleTaskRefresh('all')
        else if (pendingUploadRefresh) scheduleTaskRefresh('upload')
        else if (pendingDownloadRefresh) scheduleTaskRefresh('download')
      }
    }, 200)
  }

  const handleUploadEvent = (event: TaskEvent) => {
    if (event.action === 'created' || event.action === 'deleted' || !event.payload) {
      scheduleTaskRefresh('upload')
      return
    }
    const knownTask = uploadTaskManager
      .getAllTasks()
      .some(task => task.precheckId === event.resource_id || (task.isExternal && task.id === event.resource_id))
    if (!knownTask) {
      scheduleTaskRefresh('upload')
      return
    }
    syncBackendTaskToFrontend(event.payload as unknown as BackendUploadTask)
  }

  onMounted(() => {
    eventRefreshStopped = false
    loadUploadTasks()
    loadDownloadTasks(true, 1, 20) // 初始加载，第一页，每页20条
    getExpiredTaskCount() // 加载过期任务数量

    // 订阅上传任务更新（保持当前分页）
    unsubscribe = uploadTaskManager.subscribe(() => {
      refreshLocalUploadTasks()
    })
    unsubscribeDownloadEvents = taskEventClient.subscribe('download.task', undefined, event => {
      if (applyDownloadTaskEvent(event)) scheduleTaskRefresh('download')
    })
    unsubscribeUploadEvents = taskEventClient.subscribe('upload.task', undefined, handleUploadEvent)
    unsubscribeSyncEvents = taskEventClient.subscribe('sync', undefined, () => scheduleTaskRefresh('all'))
  })

  onBeforeUnmount(() => {
    eventRefreshStopped = true
    pendingUploadRefresh = false
    pendingDownloadRefresh = false
    if (unsubscribe) {
      unsubscribe()
    }
    unsubscribeDownloadEvents?.()
    unsubscribeUploadEvents?.()
    unsubscribeSyncEvents?.()
    if (eventRefreshTimer !== null) window.clearTimeout(eventRefreshTimer)
  })
</script>

<style scoped>
  .task-panel {
    height: 100%;
    display: flex;
    min-height: 0;
    flex-direction: column;
  }

  @media (max-width: 767px) {
    .task-panel {
      height: auto;
    }
  }
</style>
