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

    <div
      v-if="activeTab === 'upload'"
      class="task-panel"
      role="tabpanel"
      :aria-label="t('tasks.upload')"
    >
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

    <div
      v-else
      class="task-panel"
      role="tabpanel"
      :aria-label="t('tasks.download')"
    >
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
  import { useI18n } from '@/composables'
  import UploadTaskTable from './components/UploadTaskTable.vue'
  import DownloadTaskTable from './components/DownloadTaskTable.vue'
  import ExpiredTasksDialog from './components/ExpiredTasksDialog.vue'
  import SegmentedControl from '@/components/SegmentedControl/index.vue'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import { useUploadTasks } from './composables/useUploadTasks'
  import { useDownloadTasks } from './composables/useDownloadTasks'

  const { t } = useI18n()

  const route = useRoute()
  const router = useRouter()

  const activeTab = ref<string>((route.query.tab as string) || 'upload')
  let refreshTimer: number | null = null
  let syncTimer: number | null = null

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

  onMounted(() => {
    loadUploadTasks()
    loadDownloadTasks(true, 1, 20) // 初始加载，第一页，每页20条
    getExpiredTaskCount() // 加载过期任务数量

    // 订阅上传任务更新（保持当前分页）
    unsubscribe = uploadTaskManager.subscribe(() => {
      // 重新加载以更新分页数据，保持当前页
      loadUploadTasks(false)
    })

    // 启动自动同步（30秒）
    const startAutoSync = () => {
      if (syncTimer) {
        clearInterval(syncTimer)
      }
      syncTimer = window.setInterval(() => {
        if (activeTab.value === 'upload') {
          loadUploadTasks(false) // 自动刷新时不显示loading
          getExpiredTaskCount() // 定期更新过期任务数量
        }
      }, 30000)
    }

    startAutoSync()

    // 启动下载任务自动刷新（3秒，智能刷新不显示loading）
    refreshTimer = window.setInterval(() => {
      // 自动刷新时不显示loading，保持当前分页
      if (activeTab.value === 'download') {
        refreshDownloadTasks(false)
      }
    }, 3000)
  })

  onBeforeUnmount(() => {
    if (unsubscribe) {
      unsubscribe()
    }
    if (syncTimer) {
      clearInterval(syncTimer)
    }
    if (refreshTimer) {
      clearInterval(refreshTimer)
    }
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
