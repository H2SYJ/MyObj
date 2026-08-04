<template>
  <WorkspacePage :title="t('square.title')" :description="t('square.desc')">
    <template #icon>
      <el-icon :size="24">
        <Grid />
      </el-icon>
    </template>
    <template #actions>
      <el-button-group>
        <el-button :type="viewMode === 'grid' ? 'primary' : ''" icon="Grid" @click="viewMode = 'grid'" />
        <el-button :type="viewMode === 'list' ? 'primary' : ''" icon="List" @click="viewMode = 'list'" />
      </el-button-group>
    </template>

    <template #toolbar>
      <!-- 筛选栏 -->
      <div class="filter-bar">
        <div class="filter-type-group">
          <SegmentedControl
            v-model="fileTypeFilter"
            :items="filterTypeItems"
            :aria-label="t('square.title')"
            @change="handleFilterChange"
          />
        </div>

        <div class="filter-sort-group">
          <TagFilter
            :model-value="tagIds"
            :mode="tagMode"
            @update:model-value="updateTagIds"
            @update:mode="updateTagMode"
          />
          <el-select
            v-model="sortBy"
            :placeholder="t('square.sortByPlaceholder')"
            class="sort-select"
            @change="handleSortChange"
          >
            <el-option :label="t('square.sortLatest')" value="time" />
            <el-option :label="t('square.sortSize')" value="size" />
            <el-option :label="t('square.sortName')" value="name" />
          </el-select>
        </div>
      </div>
    </template>

    <!-- 文件网格视图 -->
    <div v-if="viewMode === 'grid'" v-loading="loading" class="file-grid">
      <el-card
        v-for="file in filteredFiles"
        :key="file.uf_id"
        shadow="hover"
        class="file-card"
        @click="handleFileClick(file)"
        @dblclick="handleFileClick(file)"
      >
        <div class="file-icon">
          <el-icon :size="64" :color="getFileIconColor(file.mime_type)">
            <component :is="getFileIconName(file.mime_type)" />
          </el-icon>
        </div>
        <file-name-tooltip :file-name="file.file_name" view-mode="grid" tag="div" custom-class="file-name" />
        <FileTags :tags="file.tags" :limit="3" compact @tag-click="handleTagClick" />
        <div class="file-meta">
          <div class="file-info">
            <span>{{ formatFileSize(file.file_size) }}</span>
            <span>·</span>
            <span>{{ file.owner_name }}</span>
          </div>
          <div class="file-stats">
            <span>{{ formatTime(file.created_at) }}</span>
          </div>
        </div>
        <div class="file-actions">
          <el-button type="primary" size="small" icon="Download" @click.stop="handleDownload(file)">
            {{ t('square.download') }}
          </el-button>
        </div>
      </el-card>

      <!-- 空状态 -->
      <div v-if="filteredFiles.length === 0 && !loading" class="empty-state">
        <el-empty :description="t('square.noPublicFiles')" />
      </div>
    </div>

    <!-- 文件列表视图 -->
    <!-- PC端：表格布局 -->
    <el-table
      v-else-if="!isMobile"
      v-loading="loading"
      :data="filteredFiles"
      style="width: 100%"
      @row-click="handleFileClick"
    >
      <el-table-column :label="t('square.fileName')" min-width="300">
        <template #default="{ row }">
          <div class="file-name-cell">
            <el-icon :size="24" :color="getFileIconColor(row.mime_type)">
              <component :is="getFileIconName(row.mime_type)" />
            </el-icon>
            <file-name-tooltip :file-name="row.file_name" view-mode="table" />
            <FileTags :tags="row.tags" :limit="3" compact @tag-click="handleTagClick" />
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('square.size')" width="120">
        <template #default="{ row }">
          {{ formatFileSize(row.file_size) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('square.uploader')" width="150" prop="owner_name" />
      <el-table-column :label="t('square.uploadTime')" width="180">
        <template #default="{ row }">
          {{ formatTime(row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column :label="t('square.operation')" width="150" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" size="small" icon="Download" @click.stop="handleDownload(row)">
            {{ t('square.download') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 移动端：卡片列表布局 -->
    <div v-else v-loading="loading" class="mobile-file-list">
      <div v-for="file in filteredFiles" :key="file.uf_id" class="mobile-file-item" @click="handleFileClick(file)">
        <div class="mobile-item-content">
          <div class="mobile-item-icon">
            <el-icon :size="40" :color="getFileIconColor(file.mime_type)">
              <component :is="getFileIconName(file.mime_type)" />
            </el-icon>
          </div>
          <div class="mobile-item-info">
            <div class="mobile-item-name-row">
              <file-name-tooltip :file-name="file.file_name" view-mode="list" custom-class="mobile-item-name" />
            </div>
            <FileTags :tags="file.tags" :limit="1" compact @tag-click="handleTagClick" />
            <div class="mobile-item-meta">
              <span class="mobile-item-size">{{ formatFileSize(file.file_size) }}</span>
              <span class="mobile-item-owner">{{ file.owner_name }}</span>
              <span class="mobile-item-time">{{ formatTime(file.created_at) }}</span>
            </div>
          </div>
          <div class="mobile-item-actions" @click.stop>
            <el-button
              type="primary"
              size="small"
              icon="Download"
              class="mobile-download-btn"
              @click.stop="handleDownload(file)"
            >
              {{ t('square.download') }}
            </el-button>
          </div>
        </div>
      </div>

      <!-- 空状态 -->
      <div v-if="filteredFiles.length === 0 && !loading" class="mobile-empty-state">
        <el-empty :description="t('square.noPublicFiles')" />
      </div>
      <MobileInfiniteList
        v-if="filteredFiles.length > 0"
        class="mobile-load-state"
        :loading="loading || isSearching"
        :has-more="filteredFiles.length < total"
        @load-more="loadNextMobilePage"
        @retry="loadNextMobilePage"
      />
    </div>

    <template v-if="!isMobile" #footer>
      <pagination
        v-model:page="currentPage"
        v-model:limit="pageSize"
        :total="total"
        :page-sizes="[20, 50, 100]"
        @pagination="handlePagination"
      />
    </template>

    <template #overlays>
      <!-- 文件预览组件 -->
      <preview v-model="previewVisible" :file="previewFile" />
    </template>
  </WorkspacePage>
</template>

<script setup lang="ts">
  import { Box, Document, Headset, Menu, MoreFilled, Picture, VideoPlay } from '@element-plus/icons-vue'
  import { formatSize, formatDate } from '@/utils'
  import { useResponsive } from '@/composables/ui/useResponsive'
  import {
    getPublicFileList,
    searchPublicFiles,
    type PublicFileItem,
    type PublicFileListParams,
    type SearchFileItem
  } from '@/api/file'
  import { useSearch } from '@/composables/business/useSearch'
  import { getFileIcon } from '@/utils/file/fileIcon'
  import { useFileDownload } from '@/composables/business/useFileDownload'
  import { useI18n } from '@/composables/core/useI18n'
  import { MobileInfiniteList } from '@/components/mobile'
  import SegmentedControl from '@/components/SegmentedControl/index.vue'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import TagFilter from '@/components/TagFilter/index.vue'
  import FileTags from '@/components/FileTags/index.vue'
  import type { CompactTag, FileItem } from '@/types'

  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const route = useRoute()
  const { t } = useI18n()

  // 使用响应式检测 composable
  const { isHandheld: isMobile } = useResponsive()

  // 响应式数据
  const viewMode = ref<'grid' | 'list'>(isMobile.value ? 'list' : 'grid')
  const fileTypeFilter = ref('all')
  const sortBy = ref('time')
  const loading = ref(false)
  const isSearchMode = ref(false) // 是否处于搜索模式
  const parseTagIds = (value: unknown) =>
    typeof value === 'string'
      ? [
          ...new Set(
            value
              .split(',')
              .map(item => item.trim())
              .filter(Boolean)
          )
        ]
      : []
  const tagIds = ref<string[]>(parseTagIds(route.query.tags))
  const tagMode = ref<'all' | 'any'>(route.query.tagMode === 'any' ? 'any' : 'all')

  const filterTypeItems = computed(() => [
    { value: 'all', label: t('square.filterAll'), icon: Menu },
    { value: 'image', label: t('square.filterImage'), icon: Picture },
    { value: 'video', label: t('square.filterVideo'), icon: VideoPlay },
    { value: 'doc', label: t('square.filterDoc'), icon: Document },
    { value: 'audio', label: t('square.filterAudio'), icon: Headset },
    { value: 'archive', label: t('square.filterArchive'), icon: Box },
    { value: 'other', label: t('square.filterOther'), icon: MoreFilled }
  ])

  // 公开文件列表
  const publicFiles = ref<PublicFileItem[]>([])

  // 使用通用搜索 composable
  const transformResult = (files: SearchFileItem[]): PublicFileItem[] =>
    files.map(file => ({
      uf_id: file.uf_id || file.id,
      file_name: file.file_name || file.name,
      file_size: file.size,
      mime_type: file.mime,
      created_at: file.created_at,
      owner_name: file.owner_name || 'Unknown',
      has_thumbnail: Boolean(file.thumbnail_img),
      tags: file.tags || []
    }))

  const search = useSearch<PublicFileItem, SearchFileItem>(
    searchPublicFiles,
    transformResult,
    () => {
      // 清空搜索时的回调：切换到正常模式
      isSearchMode.value = false
      currentPage.value = 1
      loadPublicFiles()
    },
    true,
    () => ({
      type: fileTypeFilter.value === 'all' ? undefined : fileTypeFilter.value,
      sortBy: sortBy.value,
      tag_ids: tagIds.value.join(',') || undefined,
      tag_mode: tagIds.value.length ? tagMode.value : undefined
    })
  )

  // 兼容现有代码的变量
  const searchKeyword = search.searchKeyword
  const isSearching = search.isSearching
  const currentPage = search.currentPage
  const pageSize = search.pageSize
  const total = search.total

  // 筛选后的文件列表（搜索模式或正常模式）
  const filteredFiles = computed(() => {
    if (isSearchMode.value) {
      return search.searchResults.value
    }
    return publicFiles.value || []
  })

  // 获取文件图标名称
  const getFileIconName = (mimeType: string) => getFileIcon(mimeType).icon

  // 获取文件图标颜色
  const getFileIconColor = (mimeType: string) => getFileIcon(mimeType).color

  const formatFileSize = formatSize

  const formatTime = (time: string): string => formatDate(time, { showTime: true })

  // 搜索处理（使用后端搜索 API）
  const performSearch = async (keyword: string, pageNum: number = 1, pageSizeNum: number = 20, append = false) => {
    if (!keyword.trim() && tagIds.value.length === 0) {
      // 如果关键词为空，切换到正常模式
      isSearchMode.value = false
      currentPage.value = 1
      loadPublicFiles()
      return
    }

    isSearchMode.value = true
    await search.performSearch(keyword, pageNum, pageSizeNum, append)
  }

  // 筛选处理
  const handleFilterChange = () => {
    currentPage.value = 1
    loadPublicFiles()
  }

  // 排序处理
  const handleSortChange = () => {
    currentPage.value = 1
    loadPublicFiles()
  }

  const pushTagRoute = (nextIds: string[], nextMode = tagMode.value) => {
    router.push({
      path: route.path,
      query: {
        ...route.query,
        tags: nextIds.length ? nextIds.join(',') : undefined,
        tagMode: nextIds.length && nextMode !== 'all' ? nextMode : undefined
      }
    })
  }
  const updateTagIds = (value: string[]) => pushTagRoute(value)
  const updateTagMode = (value: 'all' | 'any') => pushTagRoute(tagIds.value, value)
  const handleTagClick = (tag: CompactTag) => {
    if (!tagIds.value.includes(tag.id)) {
      pushTagRoute([...tagIds.value, tag.id])
    }
  }

  // 文件预览
  const previewVisible = ref(false)
  const previewFile = ref<FileItem | null>(null)

  // 文件下载
  const { handleDownload: handleFileDownload } = useFileDownload()

  // 点击文件
  const handleFileClick = (file: PublicFileItem) => {
    // 将 Square 的文件格式转换为 Preview 组件需要的格式
    previewFile.value = {
      file_id: file.uf_id,
      file_name: file.file_name,
      file_size: file.file_size,
      mime_type: file.mime_type,
      is_enc: false,
      has_thumbnail: file.has_thumbnail,
      created_at: file.created_at,
      public: true,
      tags: file.tags
    }
    previewVisible.value = true
  }

  // 下载文件
  const handleDownload = async (file: PublicFileItem) => {
    // 将 PublicFileItem 转换为 FileItem 格式
    const fileItem: FileItem = {
      file_id: file.uf_id,
      file_name: file.file_name,
      file_size: file.file_size,
      mime_type: file.mime_type,
      is_enc: false, // 公开文件默认不加密
      has_thumbnail: file.has_thumbnail,
      created_at: file.created_at,
      public: true // 标记为公开文件
    }
    await handleFileDownload(fileItem)
  }

  // 分页处理
  const handlePagination = ({ page, limit }: { page: number; limit: number }) => {
    if (isSearchMode.value && searchKeyword.value.trim()) {
      // 搜索模式下的分页
      performSearch(searchKeyword.value, page, limit)
    } else {
      // 正常模式下的分页
      currentPage.value = page
      pageSize.value = limit
      loadPublicFiles()
    }
  }

  // 加载公开文件列表
  const loadPublicFiles = async (append = false) => {
    // 如果正在搜索，不显示加载状态（避免冲突）
    if (!isSearchMode.value) {
      loading.value = true
    }
    try {
      // 构建请求参数，只有当类型不是 'all' 时才传递 type
      const params: PublicFileListParams = {
        sortBy: sortBy.value,
        page: currentPage.value,
        pageSize: pageSize.value
      }
      // 只有当类型不是 'all' 时才添加 type 参数
      if (fileTypeFilter.value !== 'all') {
        params.type = fileTypeFilter.value
      }
      if (tagIds.value.length > 0) {
        params.tag_ids = tagIds.value.join(',')
        params.tag_mode = tagMode.value
      }

      const response = await getPublicFileList(params)

      if (response.code === 200 && response.data) {
        // 确保 files 是数组，如果为 null 或 undefined 则使用空数组
        const nextFiles = response.data.files || []
        publicFiles.value = append ? [...publicFiles.value, ...nextFiles] : nextFiles
        total.value = response.data.total || 0
      } else {
        proxy?.$modal.msgError(response.message || t('square.loadFailed'))
        // 加载失败时也确保是空数组
        publicFiles.value = []
      }
    } catch (error) {
      proxy?.$log.error('加载公开文件列表失败:', error)
      proxy?.$modal.msgError(t('square.loadFailed'))
    } finally {
      if (!isSearchMode.value) {
        loading.value = false
      }
    }
  }

  const loadNextMobilePage = async () => {
    if (!isMobile.value || loading.value || isSearching.value || filteredFiles.value.length >= total.value) {
      return
    }
    const nextPage = currentPage.value + 1
    if (isSearchMode.value && (searchKeyword.value.trim() || tagIds.value.length > 0)) {
      await performSearch(searchKeyword.value, nextPage, pageSize.value, true)
    } else {
      currentPage.value = nextPage
      await loadPublicFiles(true)
    }
  }

  onMounted(() => {
    // 监听全局搜索事件
    const handleGlobalSearch = (event: Event) => {
      const customEvent = event as CustomEvent<{ keyword: string }>
      const keyword = customEvent.detail.keyword.trim()

      if (keyword) {
        // 只有当关键词变化时才执行搜索，避免重复请求
        if (searchKeyword.value !== keyword) {
          searchKeyword.value = keyword
          performSearch(keyword, 1, pageSize.value)
        }
      } else {
        // 清空搜索
        search.clearSearch()
      }
    }

    window.addEventListener('square-search', handleGlobalSearch)

    // 检查路由参数中是否有搜索关键词
    const keyword = route.query.search as string
    if (keyword) {
      searchKeyword.value = keyword
      performSearch(keyword, 1, pageSize.value)
    } else {
      loadPublicFiles()
    }

    // 清理事件监听
    onBeforeUnmount(() => {
      window.removeEventListener('square-search', handleGlobalSearch)
    })
  })

  // 监听路由参数变化
  watch(
    () => route.query.search,
    newKeyword => {
      if (newKeyword) {
        searchKeyword.value = newKeyword as string
        performSearch(newKeyword as string, 1, pageSize.value)
      } else if (isSearchMode.value) {
        // 如果路由参数被清空，且当前在搜索模式，则切换到正常模式
        isSearchMode.value = false
        searchKeyword.value = ''
        loadPublicFiles()
      }
    }
  )
  watch(
    () => [route.query.tags, route.query.tagMode],
    async () => {
      const nextIds = parseTagIds(route.query.tags)
      const nextMode = route.query.tagMode === 'any' ? 'any' : 'all'
      if (nextIds.join(',') === tagIds.value.join(',') && nextMode === tagMode.value) {
        return
      }
      tagIds.value = nextIds
      tagMode.value = nextMode
      currentPage.value = 1
      if (searchKeyword.value.trim()) {
        await performSearch(searchKeyword.value, 1, pageSize.value)
      } else {
        isSearchMode.value = false
        await loadPublicFiles()
      }
    }
  )
</script>

<style scoped>
  .filter-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 4px 0;
    gap: 12px;
  }

  .filter-type-group {
    flex: 1;
    min-width: 0;
  }

  .filter-sort-group {
    flex-shrink: 0;
  }

  .sort-select {
    width: 160px;
  }

  .sort-select :deep(.el-select__wrapper) {
    min-height: 48px;
    border-radius: 12px;
  }

  .file-grid {
    flex: 1;
    padding: 24px;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 20px;
    overflow-y: auto;
    align-content: start;
  }

  .file-card {
    cursor: pointer;
    transition: all 0.3s;
  }

  .file-card:hover {
    transform: translateY(-4px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
  }

  .file-card {
    height: 100%;
    display: flex;
    flex-direction: column;
  }

  .file-card :deep(.el-card__body) {
    padding: 20px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    flex: 1;
    min-height: 0;
  }

  .file-icon {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 12px;
    flex-shrink: 0;
  }

  .file-name {
    font-size: 14px;
    font-weight: 500;
    color: var(--el-text-color-primary);
    text-align: center;
    width: 100%;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    word-break: break-word;
    line-height: 1.4;
    min-height: 2.8em; /* 固定高度：2行 * 1.4行高 */
    max-height: 2.8em;
  }

  .file-meta {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex-shrink: 0;
  }

  .file-info {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    text-align: center;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4px;
  }

  .file-stats {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }

  .file-stats .el-icon {
    font-size: 14px;
  }

  .file-stats span {
    margin-left: 4px;
  }

  .file-actions {
    width: 100%;
    margin-top: auto;
    flex-shrink: 0;
    padding-top: 8px;
  }

  .file-actions .el-button {
    width: 100%;
  }

  .file-name-cell {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .file-name-text {
    display: inline-block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 250px;
    vertical-align: middle;
  }

  .empty-state {
    grid-column: 1 / -1;
    display: flex;
    justify-content: center;
    padding: 60px 0;
  }

  .pagination {
    padding: 20px;
    border-top: 1px solid var(--el-border-color);
    display: flex;
    justify-content: center;
  }

  /* 移动端响应式 - 组件特定样式 */
  @media (max-width: 767px) {
    .filter-bar {
      padding: 0;
      flex-direction: column;
      align-items: stretch;
      gap: 10px;
    }

    .filter-type-group {
      width: 100%;
    }

    .filter-sort-group {
      width: 100%;
    }

    .sort-select {
      width: 100%;
    }

    .file-grid {
      padding: 12px;
      grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
      gap: 12px;
    }

    .file-card :deep(.el-card__body) {
      padding: 12px;
      gap: 8px;
    }

    .file-icon {
      padding: 8px;
    }

    .file-name {
      font-size: 12px;
      min-height: 2.52em; /* 2行 * 1.26行高（12px * 1.05） */
      max-height: 2.52em;
    }

    .file-info {
      font-size: 11px;
    }

    .file-stats {
      font-size: 11px;
    }

    .pagination {
      padding: 12px;
    }
  }

  @media (max-width: 480px) {
    .file-grid {
      grid-template-columns: repeat(auto-fill, minmax(100px, 1fr));
      gap: 8px;
      padding: 8px;
    }

    .file-card :deep(.el-card__body) {
      padding: 8px;
      gap: 6px;
    }

    .file-icon {
      padding: 6px;
    }

    .file-name {
      font-size: 11px;
      min-height: 2.31em; /* 2行 * 1.155行高（11px * 1.05） */
      max-height: 2.31em;
    }

    .toolbar {
      padding: 10px 12px;
    }
  }

  @media (max-width: 767px) {
    .mobile-file-list {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      align-content: start;
      gap: 10px;
      padding: 12px;
      overflow: visible;
    }
    .mobile-file-item {
      min-width: 0;
      padding: 14px 10px 12px;
      border-radius: 18px;
    }
    .mobile-item-content {
      min-height: 156px;
      flex-direction: column;
      align-items: stretch;
      gap: 10px;
    }
    .mobile-item-icon {
      width: 64px;
      height: 64px;
      margin: 0 auto;
      border-radius: 18px;
      background: color-mix(in srgb, var(--primary-color) 8%, transparent);
    }
    .mobile-item-info {
      text-align: center;
      gap: 5px;
    }
    .mobile-item-name-row {
      justify-content: center;
    }
    .mobile-item-name {
      font-size: 13px;
      font-weight: 650;
    }
    .mobile-item-meta {
      justify-content: center;
      gap: 6px;
      font-size: 10px;
    }
    .mobile-item-time {
      display: none;
    }
    .mobile-item-actions {
      margin-top: auto;
    }
    .mobile-download-btn {
      width: 100%;
      min-height: 36px;
      border-radius: 12px;
    }
    .mobile-load-state,
    .mobile-empty-state {
      grid-column: 1 / -1;
    }
  }

  /* 移动端卡片列表布局 */
  .mobile-file-list {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    overflow-y: auto;
  }

  .mobile-file-item {
    background: var(--card-bg);
    border-radius: 12px;
    padding: 12px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
    transition: all 0.2s ease;
    border: 1px solid var(--el-border-color-lighter);
    cursor: pointer;
  }

  .mobile-file-item:active {
    transform: scale(0.98);
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.08);
    background: var(--el-fill-color-light);
  }

  html.dark .mobile-file-item {
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
  }

  html.dark .mobile-file-item:active {
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.3);
  }

  .mobile-item-content {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .mobile-item-icon {
    flex-shrink: 0;
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .mobile-item-info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .mobile-item-name-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  .mobile-item-name {
    flex: 1;
    min-width: 0;
    font-size: 15px;
    font-weight: 500;
    color: var(--el-text-color-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mobile-item-meta {
    display: flex;
    align-items: center;
    gap: 12px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    flex-wrap: wrap;
  }

  .mobile-item-size {
    font-weight: 500;
    color: var(--el-text-color-regular);
  }

  .mobile-item-owner {
    color: var(--el-text-color-secondary);
  }

  .mobile-item-time {
    color: var(--el-text-color-placeholder);
  }

  .mobile-item-actions {
    flex-shrink: 0;
  }

  .mobile-download-btn {
    padding: 8px 16px;
  }

  .mobile-empty-state {
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 60px 0;
    min-height: 200px;
  }

  @media (max-width: 480px) {
    .mobile-file-list {
      padding: 8px;
      gap: 6px;
    }

    .mobile-file-item {
      padding: 10px;
      border-radius: 10px;
    }

    .mobile-item-icon {
      width: 36px;
      height: 36px;
    }

    .mobile-item-name {
      font-size: 14px;
    }

    .mobile-item-meta {
      font-size: 11px;
      gap: 8px;
    }

    .mobile-download-btn {
      padding: 6px 12px;
      font-size: 12px;
    }
  }

  /* 深色模式样式 */
  html.dark .square-container {
    background: var(--card-bg);
  }

  html.dark .toolbar {
    border-bottom-color: var(--el-border-color);
  }

  html.dark .file-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .file-card:hover {
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
  }

  html.dark .file-card :deep(.el-card__body) {
    background: var(--card-bg);
  }

  html.dark .toolbar-actions :deep(.el-button-group .el-button) {
    background-color: var(--el-bg-color);
    color: var(--el-text-color-primary);
    border-color: var(--el-border-color);
  }

  html.dark .toolbar-actions :deep(.el-button-group .el-button.is-active) {
    background-color: var(--primary-color);
    border-color: var(--primary-color);
    color: var(--el-text-color-primary);
  }

  html.dark .pagination {
    border-top-color: var(--el-border-color);
  }
</style>
