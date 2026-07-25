<template>
  <WorkspacePage :title="t('offline.title')">
    <template #icon>
      <el-icon :size="24">
        <Download />
      </el-icon>
    </template>
    <template #meta>{{ t('offline.taskCount', { count: taskTotal }) }}</template>
    <template #actions>
      <TableSelectionActions
        v-if="!isMobile && selectedTaskIds.length > 0"
        :selected-text="t('offline.selectedTasks', { count: selectedTaskIds.length })"
        :clear-text="t('common.clearSelection')"
        @clear="clearTaskSelection"
      >
        <el-button
          type="warning"
          icon="Close"
          :loading="batchCanceling"
          :disabled="selectedCancelableTaskIds.length === 0 || batchDeleting"
          @click="batchCancelTasks"
        >
          {{ t('offline.batchCancel', { count: selectedCancelableTaskIds.length }) }}
        </el-button>
        <el-button
          type="danger"
          icon="Delete"
          :loading="batchDeleting"
          :disabled="selectedDeletableTaskIds.length === 0 || batchCanceling"
          @click="batchDeleteTasks"
        >
          {{ t('offline.batchDelete', { count: selectedDeletableTaskIds.length }) }}
        </el-button>
      </TableSelectionActions>
      <template v-else>
        <el-button type="primary" icon="Plus" @click="showDownloadDialog = true">{{
          t('offline.newDownload')
        }}</el-button>
        <el-button icon="Refresh" @click="refreshTaskList">{{ t('common.refresh') }}</el-button>
      </template>
    </template>

    <!-- PC端：表格布局 -->
    <el-table
      ref="taskTableRef"
      :data="taskList"
      row-key="id"
      v-loading="loading"
      class="offline-table desktop-table"
      @selection-change="handleTaskSelectionChange"
    >
      <el-table-column type="selection" width="44" :reserve-selection="true" />
      <el-table-column :label="t('tasks.fileName')" min-width="180" class-name="mobile-name-column">
        <template #default="{ row }">
          <div class="file-name-cell">
            <el-icon :size="24" class="offline-icon"><Document /></el-icon>
            <div class="file-info">
              <file-name-tooltip
                :file-name="row.file_name || t('offline.unknownFile')"
                view-mode="table"
                custom-class="file-name"
              />
              <div class="file-url mobile-hide" v-if="row.url">{{ truncateUrl(row.url) }}</div>
            </div>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="t('tasks.status')" width="105" class-name="mobile-hide">
        <template #default="{ row }">
          <el-tag :type="getStatusType(row.state)">{{ row.state_text }}</el-tag>
        </template>
      </el-table-column>

      <el-table-column :label="t('tasks.progress')" width="160" class-name="mobile-progress-column">
        <template #default="{ row }">
          <div class="progress-cell">
            <el-progress
              :percentage="row.progress"
              :status="row.state === 3 ? 'success' : row.state === 4 || row.state === 5 ? 'exception' : undefined"
            />
            <span class="progress-text">
              {{
                row.file_size > 0
                  ? `${formatSize(row.downloaded_size)} / ${formatSize(row.file_size)}`
                  : t('offline.downloadedOnly', { size: formatSize(row.downloaded_size) })
              }}
            </span>
          </div>
        </template>
      </el-table-column>

      <el-table-column :label="t('offline.speed')" width="80" class-name="mobile-hide">
        <template #default="{ row }">
          <span v-if="row.state === 1">{{ formatSpeed(row.speed) }}</span>
          <span v-else>-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('tasks.createTime')" width="145" class-name="mobile-hide">
        <template #default="{ row }">
          {{ formatDate(row.create_time) }}
        </template>
      </el-table-column>

      <el-table-column :label="t('offline.errorInfo')" min-width="95" class-name="mobile-hide">
        <template #default="{ row }">
          <el-tooltip v-if="row.error_msg" :content="row.error_msg" placement="top">
            <span class="error-msg-text">{{ row.error_msg }}</span>
          </el-tooltip>
          <span v-else class="no-error-text">-</span>
        </template>
      </el-table-column>

      <el-table-column :label="t('tasks.operation')" width="180" class-name="mobile-actions-column">
        <template #default="{ row }">
          <div class="action-buttons">
            <el-button
              v-if="row.state === 1"
              link
              icon="VideoPause"
              type="warning"
              @click="pauseTask(row.id)"
              size="small"
            >
              {{ t('tasks.pause') }}
            </el-button>
            <el-button
              v-if="row.state === 2"
              link
              icon="VideoPlay"
              type="primary"
              @click="resumeTask(row)"
              size="small"
            >
              {{ t('tasks.resume') }}
            </el-button>
            <el-button
              v-if="row.state === 0 || row.state === 1 || row.state === 2"
              link
              icon="Close"
              type="danger"
              @click="cancelTask(row.id)"
              size="small"
            >
              {{ t('tasks.cancel') }}
            </el-button>
            <el-button
              v-if="row.state === 4 || row.state === 5"
              link
              icon="RefreshRight"
              type="primary"
              @click="retryTask(row)"
              size="small"
            >
              {{ t('tasks.retry') }}
            </el-button>
            <el-button
              v-if="row.state === 3 || row.state === 4 || row.state === 5"
              link
              icon="Delete"
              type="danger"
              @click="deleteTask(row.id)"
              size="small"
            >
              {{ t('tasks.delete') }}
            </el-button>
          </div>
        </template>
      </el-table-column>
    </el-table>

    <!-- 移动端：卡片布局 -->
    <div class="mobile-task-list" v-loading="loading">
      <div
        v-for="row in taskList"
        :key="row.id"
        class="mobile-task-item"
        :class="{ selected: selectedTaskIds.includes(row.id) }"
      >
        <div class="task-item-header">
          <div class="task-item-info">
            <el-checkbox
              :model-value="selectedTaskIds.includes(row.id)"
              class="task-checkbox"
              @change="() => toggleMobileTaskSelection(row)"
            />
            <el-icon :size="24" class="task-icon offline-icon"><Document /></el-icon>
            <div class="task-name-wrapper">
              <file-name-tooltip
                :file-name="row.file_name || row.url || t('offline.unknownFile')"
                view-mode="list"
                custom-class="task-name"
              />
              <div class="task-meta">
                <el-tag :type="getStatusType(row.state)" size="small" effect="plain">
                  {{ row.state_text }}
                </el-tag>
                <span class="task-size">
                  {{
                    row.file_size > 0
                      ? `${formatSize(row.downloaded_size)} / ${formatSize(row.file_size)}`
                      : t('offline.downloadedOnly', { size: formatSize(row.downloaded_size) })
                  }}
                </span>
                <span v-if="row.state === 1" class="task-speed">{{ formatSpeed(row.speed) }}</span>
              </div>
              <div v-if="row.url" class="task-url">{{ truncateUrl(row.url, 40) }}</div>
            </div>
          </div>
          <div class="task-actions">
            <el-button v-if="row.state === 1" link type="warning" @click.stop="pauseTask(row.id)" class="action-btn">
              <el-icon><VideoPause /></el-icon>
            </el-button>
            <el-button v-if="row.state === 2" link type="primary" @click.stop="resumeTask(row)" class="action-btn">
              <el-icon><VideoPlay /></el-icon>
            </el-button>
            <el-button
              v-if="row.state === 0 || row.state === 1 || row.state === 2"
              link
              type="danger"
              @click.stop="cancelTask(row.id)"
              class="action-btn"
            >
              <el-icon><Close /></el-icon>
            </el-button>
            <el-button
              v-if="row.state === 4 || row.state === 5"
              link
              type="primary"
              @click.stop="retryTask(row)"
              class="action-btn"
            >
              <el-icon><RefreshRight /></el-icon>
            </el-button>
            <el-button
              v-if="row.state === 3 || row.state === 4 || row.state === 5"
              link
              type="danger"
              @click.stop="deleteTask(row.id)"
              class="action-btn"
            >
              <el-icon><Delete /></el-icon>
            </el-button>
          </div>
        </div>
        <div class="task-progress-wrapper">
          <el-progress
            :percentage="row.progress"
            :status="row.state === 3 ? 'success' : row.state === 4 || row.state === 5 ? 'exception' : undefined"
            :stroke-width="6"
            text-inside
            class="task-progress"
          />
        </div>
      </div>
    </div>

    <el-empty v-if="taskList.length === 0 && !loading" :description="t('offline.noDownloads')" />
    <MobileInfiniteList
      v-if="isMobile && taskList.length > 0"
      :loading="loading"
      :has-more="taskList.length < taskTotal"
      @load-more="loadNextMobileTaskPage"
      @retry="loadNextMobileTaskPage"
    />

    <template v-if="!isMobile && taskTotal > taskPageSize" #footer>
      <el-pagination
        v-model:current-page="taskPage"
        v-model:page-size="taskPageSize"
        :total="taskTotal"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        class="task-pagination"
        @current-change="handleTaskPageChange"
        @size-change="handleTaskPageSizeChange"
      />
    </template>

    <template #floating>
      <TableSelectionActions
        v-if="isMobile && selectedTaskIds.length > 0"
        mode="floating"
        :selected-text="t('offline.selectedTasks', { count: selectedTaskIds.length })"
        :clear-text="t('common.clearSelection')"
        @clear="clearTaskSelection"
      >
        <el-button
          link
          type="warning"
          icon="Close"
          :loading="batchCanceling"
          :disabled="selectedCancelableTaskIds.length === 0 || batchDeleting"
          @click="batchCancelTasks"
        >
          {{ t('tasks.cancel') }}
        </el-button>
        <el-button
          link
          type="danger"
          icon="Delete"
          :loading="batchDeleting"
          :disabled="selectedDeletableTaskIds.length === 0 || batchCanceling"
          @click="batchDeleteTasks"
        >
          {{ t('tasks.delete') }}
        </el-button>
      </TableSelectionActions>
    </template>

    <template #overlays>
      <!-- 统一下载对话框 -->
      <el-dialog
        v-model="showDownloadDialog"
        :title="t('offline.newDownload')"
        :width="isMobile ? '95%' : '800px'"
        :fullscreen="isMobile"
        @open="handleDownloadDialogOpen"
        @close="handleDownloadDialogClose"
        :destroy-on-close="true"
        class="download-dialog"
      >
        <template v-if="showDownloadDialog">
          <!-- 输入区域：支持文本输入和文件上传 -->
          <div class="input-section">
            <el-tabs v-model="inputType" class="input-tabs">
              <el-tab-pane :label="t('offline.inputLink')" name="text">
                <el-form-item :label="t('offline.downloadLink')">
                  <el-input
                    v-model="downloadForm.inputText"
                    :placeholder="t('offline.downloadLinkPlaceholder')"
                    type="textarea"
                    :rows="3"
                    @input="handleInputTextChange"
                  />
                  <div class="input-tip">
                    <el-icon><InfoFilled /></el-icon>
                    <span>{{ t('offline.downloadTip') }}</span>
                  </div>
                </el-form-item>
              </el-tab-pane>
              <el-tab-pane :label="t('offline.uploadTorrent')" name="file">
                <el-upload
                  ref="torrentUploadRef"
                  :auto-upload="false"
                  :on-change="handleTorrentFileChange"
                  :limit="1"
                  accept=".torrent"
                  drag
                  class="torrent-upload"
                >
                  <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
                  <div class="el-upload__text">
                    {{ t('offline.dragTorrentHere') }}
                  </div>
                  <template #tip>
                    <div class="el-upload__tip">
                      {{ t('offline.torrentFileTip') }}
                    </div>
                  </template>
                </el-upload>
                <div v-if="torrentFileName" class="torrent-file-info">
                  <el-icon><Document /></el-icon>
                  <span>{{ torrentFileName }}</span>
                  <el-button link type="danger" @click="clearTorrentFile">{{ t('offline.clear') }}</el-button>
                </div>
              </el-tab-pane>
            </el-tabs>

            <!-- 输入类型提示 -->
            <div v-if="detectedInputType" class="detected-type-tip">
              <el-icon :class="detectedInputType === 'url' ? 'input-icon-success' : 'input-icon-primary'" :size="16">
                <component :is="detectedInputType === 'url' ? 'Check' : 'InfoFilled'" />
              </el-icon>
              <span v-if="detectedInputType === 'url'">{{ t('offline.detectedAsUrl') }}</span>
              <span v-else-if="detectedInputType === 'magnet'">{{ t('offline.detectedAsMagnet') }}</span>
              <span v-else-if="detectedInputType === 'torrent'">{{ t('offline.detectedAsTorrent') }}</span>
            </div>
          </div>

          <!-- URL 下载模式：直接显示表单 -->
          <el-form
            v-if="detectedInputType === 'url' && !torrentParseResult"
            :model="downloadForm"
            :rules="downloadRules"
            ref="downloadFormRef"
            label-width="100px"
            style="margin-top: 20px"
          >
            <el-form-item :label="t('offline.saveLocation')">
              <el-tree-select
                v-model="downloadForm.save_path"
                :data="folderTreeData"
                :render-after-expand="false"
                :placeholder="t('offline.selectSaveDirectory')"
                :loading="loadingTree"
                style="width: 100%"
                check-strictly
                :props="{ label: 'label', children: 'children' }"
                :default-expand-all="true"
                node-key="value"
              />
            </el-form-item>
            <el-form-item :label="t('offline.downloadType')">
              <el-select v-model="downloadForm.download_type" style="width: 100%">
                <el-option :label="t('offline.downloadTypeAuto')" value="auto" />
                <el-option :label="t('offline.downloadTypeHttp')" value="http" />
                <el-option :label="t('offline.downloadTypeHls')" value="hls" />
              </el-select>
            </el-form-item>
            <template v-if="showHLSOptions">
              <el-form-item v-if="showHLSFileName" :label="t('offline.outputFileName')">
                <el-input v-model="downloadForm.file_name" :placeholder="t('offline.outputFileNamePlaceholder')" />
              </el-form-item>
              <el-form-item :label="t('offline.requestHeaders')">
                <div class="hls-header-editor">
                  <div v-for="(header, index) in hlsHeaderRows" :key="header.id" class="hls-header-row">
                    <el-input
                      v-model="header.name"
                      :placeholder="t('offline.headerName')"
                      @paste="handleHeaderPaste($event, index, 'create')"
                    />
                    <el-input v-model="header.value" :placeholder="t('offline.headerValue')" />
                    <el-button link type="danger" @click="removeHeaderRow(index, 'create')">{{
                      t('common.delete')
                    }}</el-button>
                  </div>
                  <el-button link type="primary" @click="addHeaderRow('create')"
                    >+ {{ t('offline.addHeader') }}</el-button
                  >
                  <div class="input-tip">{{ t('offline.headerPasteTip') }}</div>
                </div>
              </el-form-item>
              <el-form-item :label="t('offline.headerHosts')">
                <el-select
                  v-model="downloadForm.header_hosts"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  :placeholder="t('offline.headerHostsPlaceholder')"
                  style="width: 100%"
                />
              </el-form-item>
            </template>
            <el-form-item :label="t('offline.encryptStorage')">
              <el-switch v-model="downloadForm.enable_encryption" />
            </el-form-item>
            <el-form-item
              v-if="downloadForm.enable_encryption"
              :label="t('offline.encryptPassword')"
              prop="file_password"
            >
              <el-input
                v-model="downloadForm.file_password"
                type="password"
                :placeholder="t('offline.encryptPasswordPlaceholder')"
                show-password
                maxlength="32"
              />
              <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px">
                {{ t('offline.encryptPasswordTip') }}
              </div>
            </el-form-item>
          </el-form>

          <!-- 种子/磁力链接模式：解析按钮 -->
          <div
            v-if="(detectedInputType === 'magnet' || detectedInputType === 'torrent') && !torrentParseResult"
            class="parse-section"
          >
            <el-button
              type="primary"
              :loading="parsing"
              :disabled="!canParse"
              @click="handleParseTorrent"
              style="width: 100%"
            >
              {{ t('offline.parseTorrent') }}
            </el-button>
          </div>

          <!-- 解析结果：文件列表 -->
          <div v-if="torrentParseResult" class="parse-result-section">
            <div class="torrent-info">
              <h4>{{ torrentParseResult.name }}</h4>
              <div class="torrent-meta">
                <el-tag type="info">{{ t('offline.fileCount', { count: torrentParseResult.files.length }) }}</el-tag>
                <el-tag type="info">{{ formatSize(torrentParseResult.total_size) }}</el-tag>
              </div>
            </div>
            <el-divider />
            <div class="file-selection-section">
              <div class="selection-header">
                <el-checkbox v-model="selectAllFiles" :indeterminate="isIndeterminate" @change="handleSelectAll">
                  {{ t('offline.allSelect') }}
                </el-checkbox>
                <span class="selected-count">{{
                  t('offline.selectedFiles', { count: selectedFileIndexes.length })
                }}</span>
              </div>
              <el-scrollbar height="300px" class="file-list-scrollbar">
                <el-table
                  ref="torrentFileTableRef"
                  :data="torrentParseResult.files"
                  @selection-change="handleFileSelectionChange"
                  :row-key="(row: any) => row.index"
                >
                  <el-table-column type="selection" width="55" :reserve-selection="true" />
                  <el-table-column :label="t('tasks.fileName')" min-width="200">
                    <template #default="{ row }">
                      <file-name-tooltip :file-name="row.name" view-mode="table" custom-class="torrent-file-name" />
                    </template>
                  </el-table-column>
                  <el-table-column :label="t('tasks.fileSize')" width="120">
                    <template #default="{ row }">
                      {{ formatSize(row.size) }}
                    </template>
                  </el-table-column>
                  <el-table-column :label="t('offline.filePath')" min-width="150" class-name="mobile-hide">
                    <template #default="{ row }">
                      <span class="file-path">{{ row.path }}</span>
                    </template>
                  </el-table-column>
                </el-table>
              </el-scrollbar>
            </div>
          </div>

          <!-- 种子下载配置表单（解析后显示） -->
          <el-form
            v-if="torrentParseResult"
            :model="downloadForm"
            :rules="downloadRules"
            ref="downloadFormRef"
            label-width="100px"
            style="margin-top: 20px"
          >
            <el-form-item :label="t('offline.saveLocation')">
              <el-tree-select
                v-model="downloadForm.save_path"
                :data="folderTreeData"
                :render-after-expand="false"
                :placeholder="t('offline.selectSaveDirectory')"
                :loading="loadingTree"
                style="width: 100%"
                check-strictly
                :props="{ label: 'label', children: 'children' }"
                :default-expand-all="true"
                node-key="value"
              />
            </el-form-item>
            <el-form-item :label="t('offline.encryptStorage')">
              <el-switch v-model="downloadForm.enable_encryption" />
            </el-form-item>
            <el-form-item
              v-if="downloadForm.enable_encryption"
              :label="t('offline.encryptPassword')"
              prop="file_password"
            >
              <el-input
                v-model="downloadForm.file_password"
                type="password"
                :placeholder="t('offline.encryptPasswordPlaceholder')"
                show-password
                maxlength="32"
              />
              <div style="font-size: 12px; color: var(--el-text-color-secondary); margin-top: 4px">
                {{ t('offline.encryptPasswordTip') }}
              </div>
            </el-form-item>
          </el-form>
        </template>

        <template #footer>
          <el-button @click="showDownloadDialog = false">{{ t('common.cancel') }}</el-button>
          <!-- URL 下载模式 -->
          <el-button
            v-if="detectedInputType === 'url' && !torrentParseResult"
            type="primary"
            :loading="creating"
            @click="handleCreateUrlDownload"
          >
            {{ t('offline.createTask') }}
          </el-button>
          <!-- 种子/磁力链接模式：解析按钮 -->
          <el-button
            v-else-if="(detectedInputType === 'magnet' || detectedInputType === 'torrent') && !torrentParseResult"
            type="primary"
            :loading="parsing"
            :disabled="!canParse"
            @click="handleParseTorrent"
          >
            {{ t('offline.parseTorrent') }}
          </el-button>
          <!-- 种子/磁力链接模式：开始下载按钮 -->
          <el-button
            v-else-if="torrentParseResult"
            type="primary"
            :loading="creatingTorrent"
            :disabled="selectedFileIndexes.length === 0"
            @click="handleStartTorrentDownload"
          >
            {{ t('offline.startDownload', { count: selectedFileIndexes.length }) }}
          </el-button>
        </template>
      </el-dialog>

      <el-dialog
        v-model="showResumeHeadersDialog"
        :title="headerDialogMode === 'retry' ? t('offline.updateHeadersRetryTitle') : t('offline.updateHeadersTitle')"
        width="680px"
      >
        <el-form label-width="110px">
          <el-form-item :label="t('offline.requestHeaders')">
            <div class="hls-header-editor">
              <div v-for="(header, index) in resumeHeaderRows" :key="header.id" class="hls-header-row">
                <el-input
                  v-model="header.name"
                  :placeholder="t('offline.headerName')"
                  @paste="handleHeaderPaste($event, index, 'resume')"
                />
                <el-input v-model="header.value" :placeholder="t('offline.headerValue')" />
                <el-button link type="danger" @click="removeHeaderRow(index, 'resume')">{{
                  t('common.delete')
                }}</el-button>
              </div>
              <el-button link type="primary" @click="addHeaderRow('resume')">+ {{ t('offline.addHeader') }}</el-button>
              <div class="input-tip">{{ t('offline.headerPasteTip') }}</div>
            </div>
          </el-form-item>
          <el-form-item :label="t('offline.headerHosts')">
            <el-select
              v-model="resumeHeaderHosts"
              multiple
              filterable
              allow-create
              default-first-option
              :placeholder="t('offline.headerHostsPlaceholder')"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item v-if="resumeTargetTask?.requires_password" :label="t('offline.encryptPassword')">
            <el-input v-model="resumeFilePassword" type="password" show-password />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="showResumeHeadersDialog = false">{{ t('common.cancel') }}</el-button>
          <el-button type="primary" :loading="resumingWithHeaders" @click="confirmResumeWithHeaders">
            {{ headerDialogMode === 'retry' ? t('tasks.retry') : t('tasks.resume') }}
          </el-button>
        </template>
      </el-dialog>
    </template>
  </WorkspacePage>
</template>

<script setup lang="ts">
  import {
    getDownloadTaskList,
    createOfflineDownload,
    pauseDownload,
    resumeDownload,
    retryDownload,
    cancelDownload,
    batchCancelDownloads,
    deleteDownload,
    batchDeleteDownloads,
    parseTorrent,
    startTorrentDownload,
    type OfflineDownloadTask,
    type ParseTorrentResponse,
    type TorrentFileInfo
  } from '@/api/download'
  import { getDirectories } from '@/api/file'
  import { formatSize, formatDate, formatSpeed, truncateUrl, getTaskStatusType, getDownloadStatusText } from '@/utils'
  import { useResponsive, useI18n, useMobileLayerHistory } from '@/composables'
  import { useLatestRequest } from '@/composables/core/useLatestRequest'
  import { MobileInfiniteList } from '@/components/mobile'
  import TableSelectionActions from '@/components/TableSelectionActions/index.vue'
  import WorkspacePage from '@/components/WorkspacePage/index.vue'
  import { taskEventClient, type TaskEvent } from '@/utils/taskEvents'

  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()

  // 使用响应式检测 composable
  const { isHandheld: isMobile } = useResponsive()

  const loading = ref(false)
  const creating = ref(false)
  const taskList = ref<OfflineDownloadTask[]>([])
  const taskPage = ref(1)
  const taskPageSize = ref(20)
  const taskTotal = ref(0)
  const taskTableRef = ref()
  const selectedTaskIds = ref<string[]>([])
  const batchCanceling = ref(false)
  const batchDeleting = ref(false)
  let syncingTaskSelection = false
  const showDownloadDialog = ref(false) // 统一的下载对话框
  useMobileLayerHistory(showDownloadDialog, 'offline-create', isMobile)
  let eventRefreshTimer: number | null = null
  let eventRefreshRunning = false
  let eventRefreshPending = false
  let eventRefreshStopped = false
  let unsubscribeDownloadEvents: (() => void) | null = null
  let unsubscribeSyncEvents: (() => void) | null = null
  const taskRequest = useLatestRequest()
  const loadingTree = ref(false)
  const folderTreeData = ref<any[]>([])

  interface HeaderRow {
    id: number
    name: string
    value: string
  }

  let nextHeaderRowId = 1
  const createHeaderRow = (name = '', value = ''): HeaderRow => ({ id: nextHeaderRowId++, name, value })
  const hlsHeaderRows = ref<HeaderRow[]>([createHeaderRow()])
  const resumeHeaderRows = ref<HeaderRow[]>([createHeaderRow()])
  const showResumeHeadersDialog = ref(false)
  const resumeTargetTask = ref<OfflineDownloadTask | null>(null)
  const resumeHeaderHosts = ref<string[]>([])
  const resumeFilePassword = ref('')
  const resumingWithHeaders = ref(false)
  const headerDialogMode = ref<'resume' | 'retry'>('resume')

  const selectedCancelableTaskIds = computed(() => {
    const selected = new Set(selectedTaskIds.value)
    return taskList.value.filter(task => selected.has(task.id) && [0, 1, 2].includes(task.state)).map(task => task.id)
  })

  const selectedDeletableTaskIds = computed(() => {
    const selected = new Set(selectedTaskIds.value)
    return taskList.value.filter(task => selected.has(task.id) && [3, 4, 5].includes(task.state)).map(task => task.id)
  })

  const downloadFormRef = ref<FormInstance>()
  const torrentUploadRef = ref()
  const torrentFileTableRef = ref()

  // 输入类型：文本输入或文件上传
  const inputType = ref<'text' | 'file'>('text')

  // 统一的下载表单
  const downloadForm = reactive({
    inputText: '', // 文本输入（URL 或磁力链接）
    save_path: '',
    enable_encryption: false,
    file_password: '',
    download_type: 'auto' as 'auto' | 'http' | 'hls',
    file_name: '',
    header_hosts: [] as string[]
  })

  const showHLSOptions = computed(() => {
    return detectedInputType.value === 'url'
  })

  const showHLSFileName = computed(() => {
    if (downloadForm.download_type === 'hls') return true
    if (downloadForm.download_type === 'http') return false
    return /\.m3u8(?:$|[?#])/i.test(downloadForm.inputText.trim())
  })

  const blockedHeaderNames = new Set([
    'accept-encoding',
    'connection',
    'content-length',
    'forwarded',
    'host',
    'if-range',
    'keep-alive',
    'proxy-authenticate',
    'proxy-authorization',
    'proxy-connection',
    'range',
    'te',
    'trailer',
    'transfer-encoding',
    'upgrade',
    'x-forwarded-for',
    'x-forwarded-host',
    'x-forwarded-proto'
  ])

  const getHeaderRows = (mode: 'create' | 'resume') => (mode === 'create' ? hlsHeaderRows : resumeHeaderRows)

  const addHeaderRow = (mode: 'create' | 'resume') => {
    const rows = getHeaderRows(mode)
    if (rows.value.length >= 32) {
      proxy?.$modal.msgWarning(t('offline.headerLimit'))
      return
    }
    rows.value.push(createHeaderRow())
  }

  const removeHeaderRow = (index: number, mode: 'create' | 'resume') => {
    const rows = getHeaderRows(mode)
    rows.value.splice(index, 1)
    if (rows.value.length === 0) rows.value.push(createHeaderRow())
  }

  const validateHeaderRows = (rows: HeaderRow[]): Record<string, string> => {
    const result: Record<string, string> = {}
    const seen = new Set<string>()
    for (const row of rows) {
      const name = row.name.trim()
      const value = row.value.trim()
      if (!name && !value) continue
      if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name)) throw new Error(t('offline.invalidHeaderName', { name }))
      const lowerName = name.toLowerCase()
      if (blockedHeaderNames.has(lowerName) || lowerName.startsWith('proxy-') || lowerName.startsWith('x-forwarded-')) {
        throw new Error(t('offline.blockedHeaderName', { name }))
      }
      if (seen.has(lowerName)) throw new Error(t('offline.duplicateHeaderName', { name }))
      if (/[\r\n]/.test(value)) throw new Error(t('offline.invalidHeaderValue', { name }))
      seen.add(lowerName)
      result[name] = value
    }
    return result
  }

  const handleHeaderPaste = (event: ClipboardEvent, index: number, mode: 'create' | 'resume') => {
    const text = event.clipboardData?.getData('text') || ''
    if (!/[\r\n]/.test(text)) return
    const parsed: HeaderRow[] = []
    for (const line of text.split(/\r?\n/).filter(item => item.trim())) {
      const delimiter = line.indexOf(':')
      if (delimiter <= 0) {
        proxy?.$modal.msgError(t('offline.headerPasteInvalid'))
        return
      }
      parsed.push(createHeaderRow(line.slice(0, delimiter).trim(), line.slice(delimiter + 1).trim()))
    }
    const rows = getHeaderRows(mode)
    const candidate = [...rows.value]
    candidate.splice(index, 1, ...parsed)
    if (candidate.length > 32) {
      proxy?.$modal.msgError(t('offline.headerLimit'))
      return
    }
    try {
      validateHeaderRows(candidate)
    } catch (error: any) {
      proxy?.$modal.msgError(error.message)
      return
    }
    event.preventDefault()
    rows.value = candidate.length > 0 ? candidate : [createHeaderRow()]
  }

  // 种子下载相关状态
  const torrentFileName = ref('')
  const torrentFileContent = ref('') // Base64 编码的种子文件内容
  const parsing = ref(false)
  const creatingTorrent = ref(false)
  const torrentParseResult = ref<ParseTorrentResponse | null>(null)
  const selectedFileIndexes = ref<number[]>([])
  const selectAllFiles = ref(false)
  const isIndeterminate = ref(false)

  // 检测到的输入类型
  const detectedInputType = ref<'url' | 'magnet' | 'torrent' | null>(null)

  // 统一的表单验证规则
  const downloadRules: FormRules = {
    inputText: [
      {
        validator: (_rule: any, value: any, callback: any) => {
          if (inputType.value === 'text' && !value?.trim()) {
            callback(new Error(t('offline.enterDownloadLink')))
          } else if (
            inputType.value === 'text' &&
            detectedInputType.value === 'url' &&
            !/^https?:\/\//.test(value?.trim())
          ) {
            callback(new Error(t('files.formatError')))
          } else {
            callback()
          }
        },
        trigger: 'blur'
      }
    ],
    file_password: [
      {
        validator: (_rule: any, value: any, callback: any) => {
          if (downloadForm.enable_encryption && !value) {
            callback(new Error(t('offline.encryptPasswordRequired')))
          } else if (value && value.length < 6) {
            callback(new Error(t('offline.passwordMinLength')))
          } else {
            callback()
          }
        },
        trigger: 'blur'
      }
    ]
  }

  // 输入类型识别函数
  const detectInputType = (input: string | File | null): 'url' | 'magnet' | 'torrent' | null => {
    if (!input) return null

    if (input instanceof File) {
      return input.name.toLowerCase().endsWith('.torrent') ? 'torrent' : null
    }

    const text = input.trim()
    if (text.startsWith('magnet:')) {
      return 'magnet'
    }
    if (text.startsWith('http://') || text.startsWith('https://')) {
      return 'url'
    }

    return null
  }

  // 监听输入变化，自动识别类型
  const handleInputTextChange = () => {
    if (inputType.value === 'text' && downloadForm.inputText) {
      detectedInputType.value = detectInputType(downloadForm.inputText)
    } else if (inputType.value === 'file') {
      detectedInputType.value = torrentFileContent.value ? 'torrent' : null
    } else {
      detectedInputType.value = null
    }
  }

  // 计算是否可以解析种子
  const canParse = computed(() => {
    if (inputType.value === 'file') {
      return !!torrentFileContent.value
    } else {
      return !!downloadForm.inputText && (detectedInputType.value === 'magnet' || detectedInputType.value === 'url')
    }
  })

  const syncTaskTableSelection = async () => {
    await nextTick()
    if (!taskTableRef.value) return
    syncingTaskSelection = true
    taskTableRef.value.clearSelection()
    const selected = new Set(selectedTaskIds.value)
    taskList.value.forEach(task => {
      if (selected.has(task.id)) taskTableRef.value.toggleRowSelection(task, true)
    })
    syncingTaskSelection = false
  }

  const handleTaskSelectionChange = (selection: OfflineDownloadTask[]) => {
    if (syncingTaskSelection) return
    selectedTaskIds.value = selection.map(task => task.id)
  }

  const toggleMobileTaskSelection = (task: OfflineDownloadTask) => {
    const selected = new Set(selectedTaskIds.value)
    if (selected.has(task.id)) selected.delete(task.id)
    else selected.add(task.id)
    selectedTaskIds.value = [...selected]
    void syncTaskTableSelection()
  }

  const clearTaskSelection = () => {
    selectedTaskIds.value = []
    void syncTaskTableSelection()
  }

  // 加载任务列表
  const loadTaskList = async (append = false, showLoading = true) => {
    const requestTicket = taskRequest.begin()
    if (showLoading || append) {
      loading.value = true
    }

    try {
      // 查询受下载管理器管理的HTTP、种子、磁力和HLS任务，不包含网盘文件下载。
      const requestPage = append ? taskPage.value : isMobile.value ? 1 : taskPage.value
      const requestSize = !append && isMobile.value ? taskPageSize.value * taskPage.value : taskPageSize.value
      const res = await getDownloadTaskList(
        { page: requestPage, pageSize: requestSize, state: -1, types: '0,4,5,9' },
        { signal: requestTicket.signal }
      )
      if (!requestTicket.isCurrent()) return
      if (res.code === 200 && res.data) {
        const newTasks = res.data.tasks || []
        taskTotal.value = res.data.total || 0

        // 确保数据更新（即使值相同，也要触发响应式更新）
        // 通过创建新数组来触发 Vue 的响应式更新
        const mergedTasks = append ? [...taskList.value, ...newTasks] : newTasks
        const uniqueTasks = new Map(mergedTasks.map((task: OfflineDownloadTask) => [task.id, { ...task }]))
        taskList.value = Array.from(uniqueTasks.values())
        const visibleTaskIDs = new Set(taskList.value.map(task => task.id))
        selectedTaskIds.value = selectedTaskIds.value.filter(taskID => visibleTaskIDs.has(taskID))
        await syncTaskTableSelection()

        // 调试日志：检查数据更新（仅在开发环境）
        if (import.meta.env.DEV) {
          const downloadingTasks = newTasks.filter((t: any) => t.state === 1)
          if (downloadingTasks.length > 0) {
            downloadingTasks.forEach((task: any) => {
              proxy?.$log?.debug('任务数据更新', {
                id: task.id,
                progress: task.progress,
                speed: task.speed,
                downloaded_size: task.downloaded_size,
                update_time: task.update_time
              })
            })
          }
        }
      }
    } catch (error: any) {
      if (!requestTicket.isCurrent()) return
      if (showLoading) {
        proxy?.$modal.msgError(error.message || t('offline.loadTaskListFailed'))
      } else {
        proxy?.$log.warn('刷新任务列表失败:', error)
      }
    } finally {
      if ((showLoading || append) && requestTicket.isCurrent()) {
        loading.value = false
      }
    }
  }

  const loadNextMobileTaskPage = async () => {
    if (!isMobile.value || loading.value || taskList.value.length >= taskTotal.value) return
    taskPage.value += 1
    await loadTaskList(true)
  }

  // 刷新任务列表
  const refreshTaskList = () => {
    void loadTaskList(false, true)
  }

  const scheduleTaskListRefresh = () => {
    if (eventRefreshStopped) return
    eventRefreshPending = true
    if (eventRefreshTimer !== null || eventRefreshRunning) return
    eventRefreshTimer = window.setTimeout(async () => {
      eventRefreshTimer = null
      if (!eventRefreshPending || eventRefreshRunning) return
      eventRefreshPending = false
      eventRefreshRunning = true
      try {
        await loadTaskList(false, false)
      } finally {
        eventRefreshRunning = false
        if (eventRefreshPending) scheduleTaskListRefresh()
      }
    }, 200)
  }

  const applyDownloadEvent = (event: TaskEvent) => {
    const payload = event.payload as Partial<OfflineDownloadTask> | undefined
    if (payload?.type !== undefined && ![0, 4, 5, 9].includes(payload.type)) return
    if (event.action === 'created' || event.action === 'deleted') {
      scheduleTaskListRefresh()
      return
    }
    const index = taskList.value.findIndex(task => task.id === event.resource_id)
    if (index < 0 || !payload) {
      scheduleTaskListRefresh()
      return
    }
    const current = taskList.value[index]
    const updated = {
      ...current,
      ...payload,
      state_text: payload.state === undefined ? current.state_text : getDownloadStatusText(payload.state)
    }
    taskList.value.splice(index, 1, updated)
  }

  const handleTaskPageSizeChange = () => {
    taskPage.value = 1
    clearTaskSelection()
    loadTaskList()
  }

  const handleTaskPageChange = () => {
    clearTaskSelection()
    loadTaskList()
  }

  // 构建文件夹树结构
  const buildFolderTree = async () => {
    loadingTree.value = true
    try {
      const res = await getDirectories()

      if (res.code !== 200 || !res.data) {
        proxy?.$modal.msgError(t('offline.getFolderTreeFailed'))
        return
      }

      const directories = res.data

      // 构建树形结构
      const pathMap = new Map<number, any>()
      const rootNodes: any[] = []

      // 第一步：创建所有节点
      directories.forEach(directory => {
        pathMap.set(directory.id, {
          value: directory.absolute_path,
          label: directory.name || t('offline.rootDir'),
          children: [],
          _raw: directory
        })
      })

      // 第二步：构建父子关系
      directories.forEach(directory => {
        const node = pathMap.get(directory.id)

        if (!node) return

        if (directory.parent_id > 0) {
          const parentNode = pathMap.get(directory.parent_id)
          if (parentNode) {
            parentNode.children.push(node)
          } else {
            // 父节点不存在，作为根节点
            rootNodes.push(node)
          }
        } else {
          // 没有父级，是根节点
          rootNodes.push(node)
        }
      })

      // 清理空 children 数组
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
      proxy?.$modal.msgError(error.message || t('offline.getFolderTreeFailed'))
    } finally {
      loadingTree.value = false
    }
  }

  // 创建 URL 下载任务
  const handleCreateUrlDownload = async () => {
    if (!downloadFormRef.value) return

    await downloadFormRef.value.validate(async (valid: boolean) => {
      if (valid) {
        if (detectedInputType.value !== 'url') {
          proxy?.$modal.msgWarning(t('offline.enterValidUrl'))
          return
        }

        creating.value = true
        try {
          const res = await createOfflineDownload({
            url: downloadForm.inputText.trim(),
            save_path: downloadForm.save_path || undefined,
            enable_encryption: downloadForm.enable_encryption,
            file_password: downloadForm.enable_encryption ? downloadForm.file_password : undefined,
            download_type: downloadForm.download_type,
            file_name:
              showHLSFileName.value && downloadForm.file_name.trim() ? downloadForm.file_name.trim() : undefined,
            request_headers: showHLSOptions.value ? validateHeaderRows(hlsHeaderRows.value) : undefined,
            header_hosts:
              showHLSOptions.value && downloadForm.header_hosts.length ? downloadForm.header_hosts : undefined
          })

          if (res.code === 200) {
            proxy?.$modal.msgSuccess('任务创建成功')
            showDownloadDialog.value = false
            loadTaskList()
          }
        } catch (error: any) {
          proxy?.$modal.msgError(error.message || '创建任务失败')
        } finally {
          creating.value = false
        }
      }
    })
  }

  // 暂停任务
  const pauseTask = async (taskId: string) => {
    try {
      await pauseDownload(taskId)
      proxy?.$modal.msgSuccess(t('tasks.pauseSuccess'))
      loadTaskList()
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('tasks.pauseFailed'))
    }
  }

  // 恢复任务
  const resumeTask = async (task: OfflineDownloadTask) => {
    try {
      if (task.requires_headers) {
        openHeadersDialog(task, 'resume')
        return
      }
      let filePassword: string | undefined
      if (task.requires_password) {
        const promptResult: any = await proxy?.$modal.prompt(t('offline.resumePasswordPrompt'))
        filePassword = promptResult?.value
        if (!filePassword) {
          proxy?.$modal.msgWarning(t('offline.passwordRequired'))
          return
        }
      }
      await resumeDownload(task.id, filePassword)
      proxy?.$modal.msgSuccess(t('tasks.resumeSuccess'))
      loadTaskList()
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || t('tasks.resumeFailed'))
    }
  }

  // 重试失败或已取消任务
  const retryTask = async (task: OfflineDownloadTask) => {
    try {
      if (task.requires_headers) {
        openHeadersDialog(task, 'retry')
        return
      }
      let filePassword: string | undefined
      if (task.requires_password) {
        const promptResult: any = await proxy?.$modal.prompt(t('offline.retryPasswordPrompt'))
        filePassword = promptResult?.value
        if (!filePassword) {
          proxy?.$modal.msgWarning(t('offline.passwordRequired'))
          return
        }
      }
      await retryDownload(task.id, filePassword)
      proxy?.$modal.msgSuccess(t('tasks.retrySuccess'))
      loadTaskList()
    } catch (error: any) {
      if (error === 'cancel' || error?.message === 'cancel') return
      proxy?.$modal.msgError(error.message || t('tasks.retryFailed'))
    }
  }

  const openHeadersDialog = (task: OfflineDownloadTask, mode: 'resume' | 'retry') => {
    resumeTargetTask.value = task
    resumeHeaderRows.value = [createHeaderRow()]
    resumeHeaderHosts.value = []
    resumeFilePassword.value = ''
    headerDialogMode.value = mode
    showResumeHeadersDialog.value = true
  }

  const confirmResumeWithHeaders = async () => {
    if (!resumeTargetTask.value) return
    if (resumeTargetTask.value.requires_password && !resumeFilePassword.value) {
      proxy?.$modal.msgWarning(t('offline.passwordRequired'))
      return
    }
    try {
      const headers = validateHeaderRows(resumeHeaderRows.value)
      resumingWithHeaders.value = true
      const filePassword = resumeTargetTask.value.requires_password ? resumeFilePassword.value : undefined
      if (headerDialogMode.value === 'retry') {
        await retryDownload(resumeTargetTask.value.id, filePassword, headers, resumeHeaderHosts.value)
      } else {
        await resumeDownload(resumeTargetTask.value.id, filePassword, headers, resumeHeaderHosts.value)
      }
      showResumeHeadersDialog.value = false
      proxy?.$modal.msgSuccess(headerDialogMode.value === 'retry' ? t('tasks.retrySuccess') : t('tasks.resumeSuccess'))
      loadTaskList()
    } catch (error: any) {
      proxy?.$modal.msgError(
        error.message || (headerDialogMode.value === 'retry' ? t('tasks.retryFailed') : t('tasks.resumeFailed'))
      )
    } finally {
      resumingWithHeaders.value = false
    }
  }

  // 取消任务
  const cancelTask = async (taskId: string) => {
    try {
      await proxy?.$modal.confirm(t('offline.confirmCancelTask'))

      await cancelDownload(taskId)
      proxy?.$modal.msgSuccess(t('tasks.cancelSuccess'))
      loadTaskList()
    } catch (error: any) {
      if (error !== 'cancel') {
        proxy?.$modal.msgError(error.message || t('tasks.cancelFailed'))
      }
    }
  }

  // 批量取消当前选中且状态允许取消的任务
  const batchCancelTasks = async () => {
    const taskIDs = [...selectedCancelableTaskIds.value]
    if (taskIDs.length === 0) return
    try {
      await proxy?.$modal.confirm(t('offline.confirmBatchCancelTasks', { count: taskIDs.length }))
      batchCanceling.value = true
      const res = await batchCancelDownloads(taskIDs)
      if (res.code !== 200 || !res.data) throw new Error(res.message || t('offline.batchCancelFailed'))

      if (res.data.failed_count === 0) {
        proxy?.$modal.msgSuccess(t('offline.batchCancelSuccess', { count: res.data.success_count }))
      } else if (res.data.success_count > 0) {
        proxy?.$modal.msgWarning(
          t('offline.batchCancelPartial', {
            success: res.data.success_count,
            failed: res.data.failed_count
          })
        )
      } else {
        proxy?.$modal.msgError(t('offline.batchCancelFailedWithCount', { count: res.data.failed_count }))
      }
      selectedTaskIds.value = res.data.failed_items.map(item => item.task_id)
      await loadTaskList()
    } catch (error: any) {
      if (error !== 'cancel' && error?.message !== 'cancel') {
        proxy?.$modal.msgError(error.message || t('offline.batchCancelFailed'))
      }
    } finally {
      batchCanceling.value = false
    }
  }

  // 删除任务
  const deleteTask = async (taskId: string) => {
    try {
      await proxy?.$modal.confirm(t('offline.confirmDeleteTask'))

      await deleteDownload(taskId)
      proxy?.$modal.msgSuccess(t('tasks.deleteSuccess'))
      loadTaskList()
    } catch (error: any) {
      if (error !== 'cancel') {
        proxy?.$modal.msgError(error.message || t('tasks.deleteFailed'))
      }
    }
  }

  // 批量删除当前选中且处于终态的任务
  const batchDeleteTasks = async () => {
    const taskIDs = [...selectedDeletableTaskIds.value]
    if (taskIDs.length === 0) return
    try {
      await proxy?.$modal.confirm(t('offline.confirmBatchDeleteTasks', { count: taskIDs.length }))
      batchDeleting.value = true
      const res = await batchDeleteDownloads(taskIDs)
      if (res.code !== 200 || !res.data) throw new Error(res.message || t('offline.batchDeleteFailed'))

      if (res.data.failed_count === 0) {
        proxy?.$modal.msgSuccess(t('offline.batchDeleteSuccess', { count: res.data.success_count }))
      } else if (res.data.success_count > 0) {
        proxy?.$modal.msgWarning(
          t('offline.batchDeletePartial', {
            success: res.data.success_count,
            failed: res.data.failed_count
          })
        )
      } else {
        proxy?.$modal.msgError(t('offline.batchDeleteFailedWithCount', { count: res.data.failed_count }))
      }
      selectedTaskIds.value = res.data.failed_items.map(item => item.task_id)
      await loadTaskList()
    } catch (error: any) {
      if (error !== 'cancel' && error?.message !== 'cancel') {
        proxy?.$modal.msgError(error.message || t('offline.batchDeleteFailed'))
      }
    } finally {
      batchDeleting.value = false
    }
  }

  // 使用 getTaskStatusType 作为 getStatusType 的别名
  const getStatusType = getTaskStatusType

  // 处理种子文件选择
  const handleTorrentFileChange = (file: any) => {
    const reader = new FileReader()
    reader.onload = e => {
      const result = e.target?.result as string
      // 移除 data URL 前缀（如 "data:application/x-bittorrent;base64,"）
      const base64Content = result.includes(',') ? result.split(',')[1] : result
      torrentFileContent.value = base64Content
      torrentFileName.value = file.name
      // 自动识别类型
      detectedInputType.value = detectInputType(file.raw)
    }
    reader.onerror = () => {
      proxy?.$modal.msgError(t('offline.readTorrentFailed'))
    }
    reader.readAsDataURL(file.raw)
  }

  // 清除种子文件
  const clearTorrentFile = () => {
    torrentFileContent.value = ''
    torrentFileName.value = ''
    detectedInputType.value = null
    if (torrentUploadRef.value) {
      torrentUploadRef.value.clearFiles()
    }
  }

  // 处理下载对话框打开
  const handleDownloadDialogOpen = () => {
    buildFolderTree()
    // 重置状态
    inputType.value = 'text'
    detectedInputType.value = null
  }

  // 处理下载对话框关闭
  const handleDownloadDialogClose = () => {
    // 重置所有状态
    inputType.value = 'text'
    downloadForm.inputText = ''
    downloadForm.save_path = ''
    downloadForm.enable_encryption = false
    downloadForm.file_password = ''
    downloadForm.download_type = 'auto'
    downloadForm.file_name = ''
    downloadForm.header_hosts = []
    hlsHeaderRows.value = [createHeaderRow()]
    torrentFileName.value = ''
    torrentFileContent.value = ''
    torrentParseResult.value = null
    selectedFileIndexes.value = []
    selectAllFiles.value = false
    isIndeterminate.value = false
    detectedInputType.value = null
    if (torrentUploadRef.value) {
      torrentUploadRef.value.clearFiles()
    }
  }

  // 解析种子
  const handleParseTorrent = async () => {
    if (!canParse.value) {
      proxy?.$modal.msgWarning('请先上传种子文件或输入磁力链接')
      return
    }

    if (detectedInputType.value !== 'magnet' && detectedInputType.value !== 'torrent') {
      proxy?.$modal.msgWarning('请输入磁力链接或上传种子文件')
      return
    }

    parsing.value = true
    try {
      const content = inputType.value === 'file' ? torrentFileContent.value : downloadForm.inputText.trim()

      const res = await parseTorrent({ content })

      if (res.code === 200 && res.data) {
        torrentParseResult.value = res.data
        // 等待 DOM 更新后设置默认全选
        await nextTick()
        // 默认全选所有文件
        if (torrentFileTableRef.value && res.data.files.length > 0) {
          res.data.files.forEach((file: TorrentFileInfo) => {
            torrentFileTableRef.value.toggleRowSelection(file, true)
          })
        }
        selectedFileIndexes.value = res.data.files.map((f: TorrentFileInfo) => f.index)
        selectAllFiles.value = true
        isIndeterminate.value = false
        proxy?.$modal.msgSuccess('解析成功')
      } else {
        proxy?.$modal.msgError(res.message || '解析失败')
      }
    } catch (error: any) {
      proxy?.$modal.msgError(error.message || '解析失败')
    } finally {
      parsing.value = false
    }
  }

  // 处理文件选择变化
  const handleFileSelectionChange = (selection: TorrentFileInfo[]) => {
    selectedFileIndexes.value = selection.map((f: TorrentFileInfo) => f.index)
    const total = torrentParseResult.value?.files.length || 0
    const selected = selectedFileIndexes.value.length
    selectAllFiles.value = selected === total && total > 0
    isIndeterminate.value = selected > 0 && selected < total
  }

  // 处理全选
  const handleSelectAll = (val: boolean | string | number) => {
    if (!torrentParseResult.value || !torrentFileTableRef.value) return

    const checked = Boolean(val)

    if (checked) {
      // 全选所有行
      torrentParseResult.value.files.forEach((file: TorrentFileInfo) => {
        torrentFileTableRef.value.toggleRowSelection(file, true)
      })
      selectedFileIndexes.value = torrentParseResult.value.files.map(f => f.index)
    } else {
      // 取消全选
      torrentParseResult.value.files.forEach((file: TorrentFileInfo) => {
        torrentFileTableRef.value.toggleRowSelection(file, false)
      })
      selectedFileIndexes.value = []
    }
    isIndeterminate.value = false
  }

  // 开始种子下载
  const handleStartTorrentDownload = async () => {
    if (!downloadFormRef.value || !torrentParseResult.value) return

    if (selectedFileIndexes.value.length === 0) {
      proxy?.$modal.msgWarning('请至少选择一个文件')
      return
    }

    await downloadFormRef.value.validate(async (valid: boolean) => {
      if (valid) {
        creatingTorrent.value = true
        try {
          const content = inputType.value === 'file' ? torrentFileContent.value : downloadForm.inputText.trim()

          const res = await startTorrentDownload({
            content,
            file_indexes: selectedFileIndexes.value,
            save_path: downloadForm.save_path || undefined,
            enable_encryption: downloadForm.enable_encryption,
            file_password: downloadForm.enable_encryption ? downloadForm.file_password : undefined
          })

          if (res.code === 200 && res.data) {
            proxy?.$modal.msgSuccess(t('offline.taskCreatedWithCount', { count: res.data.task_count }))
            showDownloadDialog.value = false
            loadTaskList()
          } else {
            proxy?.$modal.msgError(res.message || t('offline.taskCreatedFailed'))
          }
        } catch (error: any) {
          proxy?.$modal.msgError(error.message || t('offline.taskCreatedFailed'))
        } finally {
          creatingTorrent.value = false
        }
      }
    })
  }

  // 页面加载时获取任务列表
  onMounted(() => {
    eventRefreshStopped = false
    void loadTaskList()
    unsubscribeDownloadEvents = taskEventClient.subscribe('download.task', undefined, applyDownloadEvent)
    unsubscribeSyncEvents = taskEventClient.subscribe('sync', undefined, scheduleTaskListRefresh)
  })

  onBeforeUnmount(() => {
    eventRefreshStopped = true
    eventRefreshPending = false
    unsubscribeDownloadEvents?.()
    unsubscribeSyncEvents?.()
    if (eventRefreshTimer !== null) window.clearTimeout(eventRefreshTimer)
  })
</script>

<style scoped>
  .hls-header-editor {
    width: 100%;
  }

  .hls-header-row {
    display: grid;
    grid-template-columns: minmax(120px, 0.8fr) minmax(180px, 1.4fr) auto;
    gap: 8px;
    align-items: center;
    margin-bottom: 8px;
  }

  .file-name-cell {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .file-info {
    flex: 1;
    overflow: hidden;
  }

  .file-name {
    font-size: 14px;
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .file-url {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-top: 2px;
  }

  .progress-cell {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .progress-text {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }

  .action-buttons {
    display: flex;
    gap: 8px;
    justify-content: center;
  }

  .error-msg-text {
    color: var(--el-color-danger);
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: block;
    cursor: pointer;
  }

  .no-error-text {
    color: var(--el-text-color-placeholder);
    font-size: 13px;
  }

  /* PC端表格样式 */
  .desktop-table {
    display: table;
  }

  /* 隐藏表格自带的空状态显示，使用手动的 el-empty */
  .offline-table :deep(.el-table__empty-block) {
    display: none;
  }

  /* 表格移动端隐藏列 */
  .offline-table :deep(.mobile-hide) {
    display: table-cell;
  }

  .offline-table :deep(.mobile-name-column) {
    min-width: 180px;
  }

  .offline-table :deep(.mobile-progress-column) {
    min-width: 150px;
  }

  .offline-table :deep(.mobile-actions-column) {
    width: auto;
    min-width: 170px;
  }

  /* 移动端卡片列表 */
  .mobile-task-list {
    display: none;
  }

  .mobile-task-item {
    padding: 16px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    background: var(--el-bg-color-overlay);
    transition: background-color 0.2s;
    border-radius: 8px;
    margin-bottom: 12px;
  }

  .mobile-task-item:last-child {
    border-bottom: none;
    margin-bottom: 0;
  }

  .mobile-task-item:active {
    background-color: var(--el-fill-color-light);
  }

  .mobile-task-item.selected {
    box-shadow: inset 0 0 0 1px var(--el-color-primary);
    background-color: var(--el-color-primary-light-9);
  }

  .task-item-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 12px;
  }

  .task-item-info {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    flex: 1;
    min-width: 0;
  }

  .task-checkbox {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .task-icon {
    flex-shrink: 0;
    margin-top: 2px;
  }

  .task-name-wrapper {
    flex: 1;
    min-width: 0;
  }

  .task-name {
    font-size: 15px;
    font-weight: 500;
    color: var(--el-text-color-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-bottom: 6px;
  }

  .task-meta {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    margin-bottom: 4px;
  }

  .task-size {
    white-space: nowrap;
  }

  .task-speed {
    color: var(--el-color-primary);
    font-weight: 500;
    white-space: nowrap;
  }

  .task-url {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-top: 4px;
  }

  .task-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    flex-shrink: 0;
    margin-left: 8px;
  }

  .action-btn {
    padding: 4px;
    min-width: auto;
  }

  .action-btn :deep(.el-icon) {
    font-size: 18px;
  }

  .task-progress-wrapper {
    width: 100%;
  }

  .task-progress {
    width: 100%;
  }

  /* 移动端响应式 */
  @media (max-width: 1280px) {
    .desktop-table {
      display: none !important;
    }

    .mobile-task-list {
      display: block;
    }

    .file-info {
      min-width: 0;
    }

    .file-url {
      font-size: 11px;
    }

    .download-dialog :deep(.el-dialog) {
      width: 95% !important;
      margin: 0 auto;
    }

    .download-dialog :deep(.el-form-item__label) {
      font-size: 14px;
    }
  }

  @media (max-width: 480px) {
    .mobile-task-item {
      padding: 12px;
    }

    .task-name {
      font-size: 14px;
    }

    .task-meta {
      font-size: 11px;
    }

    .task-url {
      font-size: 10px;
    }

    .download-dialog :deep(.el-dialog) {
      width: 100% !important;
      margin: 0;
      border-radius: 0;
    }

    .download-dialog :deep(.el-form-item__label) {
      font-size: 13px;
    }
  }

  /* 统一下载对话框样式 */
  .download-dialog :deep(.el-dialog) {
    border-radius: 8px;
  }

  .input-section {
    margin-bottom: 20px;
  }

  .input-tabs {
    margin-bottom: 16px;
  }

  .input-tip {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 8px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }

  .input-tip .el-icon {
    font-size: 14px;
    color: var(--el-color-info);
  }

  .detected-type-tip {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    margin-top: 12px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    font-size: 13px;
    color: var(--el-text-color-primary);
  }

  .detected-type-tip .el-icon {
    font-size: 16px;
  }

  .torrent-tabs {
    margin-bottom: 20px;
  }

  .torrent-upload {
    width: 100%;
  }

  .torrent-upload :deep(.el-upload) {
    width: 100%;
  }

  .torrent-upload :deep(.el-upload-dragger) {
    width: 100%;
    padding: 40px 20px;
  }

  .torrent-file-info {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
    padding: 12px;
    background: var(--el-fill-color-light);
    border-radius: 4px;
    font-size: 14px;
  }

  .torrent-file-info .el-icon {
    color: var(--el-color-primary);
  }

  .parse-section {
    margin-top: 20px;
  }

  .parse-result-section {
    margin-top: 20px;
  }

  .torrent-info {
    margin-bottom: 16px;
  }

  .torrent-info h4 {
    margin: 0 0 8px 0;
    font-size: 16px;
    font-weight: 600;
    color: var(--el-text-color-primary);
  }

  .torrent-meta {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .file-selection-section {
    margin-top: 16px;
  }

  .selection-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;
    padding: 8px 0;
  }

  .selected-count {
    font-size: 14px;
    color: var(--el-text-color-secondary);
  }

  .file-list-scrollbar {
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 4px;
  }

  .torrent-file-name {
    font-size: 14px;
  }

  .file-path {
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* 移动端响应式 */
  @media (max-width: 1024px) {
    .download-dialog :deep(.el-dialog) {
      width: 95% !important;
      margin: 0 auto;
    }

    .download-dialog :deep(.el-form-item__label) {
      font-size: 14px;
    }

    .file-list-scrollbar {
      height: 200px !important;
    }
  }

  @media (max-width: 480px) {
    .download-dialog :deep(.el-dialog) {
      width: 100% !important;
      margin: 0;
      border-radius: 0;
    }

    .download-dialog :deep(.el-form-item__label) {
      font-size: 13px;
    }

    .torrent-info h4 {
      font-size: 14px;
    }

    .file-list-scrollbar {
      height: 150px !important;
    }
  }

  /* 深色模式样式 */
  html.dark .offline-page {
    background: var(--card-bg);
  }

  html.dark .header-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .header-card :deep(.el-card__body) {
    background: var(--card-bg);
  }

  html.dark .task-list-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .task-list-card :deep(.el-card__body) {
    background: var(--card-bg);
  }

  html.dark .offline-table {
    background: var(--card-bg);
  }

  html.dark .mobile-task-item {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .download-dialog :deep(.el-dialog) {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .download-dialog :deep(.el-dialog__header) {
    background: var(--card-bg);
    border-bottom-color: var(--el-border-color);
  }

  html.dark .download-dialog :deep(.el-dialog__title) {
    color: var(--el-text-color-primary);
  }

  html.dark .download-dialog :deep(.el-dialog__body) {
    background: var(--card-bg);
    color: var(--el-text-color-primary);
  }

  html.dark .download-dialog :deep(.el-form-item__label) {
    color: var(--el-text-color-primary);
  }

  html.dark .download-dialog :deep(.el-input__wrapper) {
    background-color: var(--el-bg-color);
    border-color: var(--el-border-color);
  }

  html.dark .download-dialog :deep(.el-input__inner) {
    color: var(--el-text-color-primary);
  }
</style>
