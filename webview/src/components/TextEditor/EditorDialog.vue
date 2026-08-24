<template>
  <el-dialog
    v-model="visible"
    :title="dialogTitle"
    width="80%"
    top="6vh"
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="!dirty"
    :show-close="!dirty"
    class="file-editor-dialog"
    destroy-on-close
    @closed="handleClosed"
  >
    <!-- 加密文件：密码解锁 -->
    <div v-if="phase === 'unlock'" class="editor-unlock">
      <el-icon :size="48" class="unlock-icon"><Lock /></el-icon>
      <p class="unlock-title">{{ t('editor.unlockTitle') }}</p>
      <p class="unlock-desc">{{ t('editor.unlockDesc') }}</p>
      <el-input
        v-model="password"
        type="password"
        show-password
        :placeholder="t('preview.downloadPassword.placeholder')"
        class="unlock-input"
        @keyup.enter="handleUnlock"
      />
      <div class="unlock-actions">
        <el-button @click="visible = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="loading" @click="handleUnlock">{{ t('common.confirm') }}</el-button>
      </div>
    </div>

    <!-- 加载中 -->
    <div v-else-if="phase === 'loading'" class="editor-loading">
      <el-icon class="is-loading" :size="32"><Loading /></el-icon>
      <p>{{ t('preview.loading') }}</p>
    </div>

    <!-- 编辑区 -->
    <div v-else class="editor-body">
      <div class="editor-meta">
        <span class="meta-item">
          <el-icon><Document /></el-icon>
          {{ file?.file_name }}
        </span>
        <span class="meta-item">
          <el-icon><Cpu /></el-icon>
          {{ t('editor.encoding') }}: {{ encoding }}
        </span>
        <span class="meta-item">
          <el-icon><Files /></el-icon>
          {{ t('editor.size') }}: {{ formatSize(content.length) }}
        </span>
      </div>
      <TextEditor v-model="content" :language="language" class="editor-main" />
    </div>

    <template #footer>
      <template v-if="phase === 'edit'">
        <el-button :disabled="saving" @click="handleClose">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="!dirty" @click="handleSave">
          <el-icon v-if="!saving"><Check /></el-icon>
          <span>{{ t('editor.save') }}</span>
        </el-button>
      </template>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
  import { computed, ref, watch } from 'vue'
  import type { FileItem } from '@/types'
  import { loadFileContent, saveFileContent } from '@/api/file'
  import { getCodeLanguage } from '@/utils/ui/preview'
  import { useI18n } from '@/composables/core/useI18n'
  import TextEditor from './index.vue'
  import { Check, Cpu, Document, Files, Lock } from '@element-plus/icons-vue'

  interface Props {
    modelValue: boolean
    file: FileItem | null
    /** 加密文件的预填密码（如从预览解锁处传入，可跳过解锁步骤直接加载） */
    initialPassword?: string
  }

  const props = withDefaults(defineProps<Props>(), {
    modelValue: false,
    file: null,
    initialPassword: ''
  })

  const emit = defineEmits<{
    'update:modelValue': [value: boolean]
    /** 保存成功后触发（调用方可刷新预览/列表） */
    saved: []
  }>()

  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const { t } = useI18n()

  const visible = computed({
    get: () => props.modelValue,
    set: val => emit('update:modelValue', val)
  })

  const dialogTitle = computed(() => `${t('editor.title')}${props.file ? ' - ' + props.file.file_name : ''}`)

  type Phase = 'unlock' | 'loading' | 'edit'
  const phase = ref<Phase>('unlock')
  const password = ref('')
  const loading = ref(false)
  const saving = ref(false)
  const content = ref('')
  const initialContent = ref('')
  const encoding = ref('utf-8')
  const baseHash = ref('')
  const language = ref('')

  const dirty = computed(() => content.value !== initialContent.value)

  /** 打开时初始化 */
  watch(
    () => props.modelValue,
    async open => {
      if (!open || !props.file) return
      reset()
      language.value = getCodeLanguage(props.file.file_name)
      if (props.file.is_enc) {
        // 已有预填密码（如预览解锁过）则直接加载，否则显示解锁界面
        if (props.initialPassword) {
          password.value = props.initialPassword
          phase.value = 'loading'
          await doLoad(props.initialPassword)
        } else {
          phase.value = 'unlock'
        }
      } else {
        await doLoad('')
      }
    }
  )

  const reset = () => {
    phase.value = 'unlock'
    password.value = ''
    loading.value = false
    saving.value = false
    content.value = ''
    initialContent.value = ''
    encoding.value = 'utf-8'
    baseHash.value = ''
    language.value = ''
  }

  const doLoad = async (filePassword: string) => {
    if (!props.file) return
    loading.value = true
    try {
      const result = await loadFileContent(props.file.file_id, filePassword)
      content.value = result.content
      encoding.value = result.encoding || 'utf-8'
      baseHash.value = result.baseHash || ''
      initialContent.value = result.content
      phase.value = 'edit'
    } catch (err: any) {
      proxy?.$log.error('加载可编辑内容失败:', err)
      proxy?.$modal.msgError(err?.message || t('editor.loadFailed'))
      // 加密文件加载失败（如密码错误）回到解锁界面，非加密文件直接关闭
      if (props.file.is_enc) {
        phase.value = 'unlock'
      } else {
        visible.value = false
      }
    } finally {
      loading.value = false
    }
  }

  const handleUnlock = async () => {
    if (!password.value) {
      proxy?.$modal.msgWarning(t('preview.downloadPassword.placeholder'))
      return
    }
    phase.value = 'loading'
    await doLoad(password.value)
  }

  const handleSave = async () => {
    if (!props.file || !dirty.value || saving.value) return
    saving.value = true
    try {
      const res = await saveFileContent({
        file_id: props.file.file_id,
        content: content.value,
        file_password: props.file.is_enc ? password.value : undefined,
        base_hash: baseHash.value
      })
      if (res.code !== 200) {
        proxy?.$modal.msgError(res.message || t('editor.saveFailed'))
        return
      }
      proxy?.$modal.msgSuccess(t('editor.saveSuccess'))
      emit('saved')
      visible.value = false
    } catch (err: any) {
      // 409 内容冲突：base_hash 不匹配，说明文件已被他人修改
      if (err?.status === 409) {
        try {
          await ElMessageBox.confirm(t('editor.conflictMessage'), t('editor.conflictTitle'), {
            type: 'warning',
            confirmButtonText: t('editor.reload'),
            cancelButtonText: t('common.cancel')
          })
          // 重新加载最新内容（丢弃本地修改）
          await doLoad(props.file.is_enc ? password.value : '')
          proxy?.$modal.msgWarning(t('editor.conflictReloaded'))
        } catch {
          // 用户取消：保留当前编辑内容
        }
        return
      }
      proxy?.$log.error('保存文件内容失败:', err)
      proxy?.$modal.msgError(err?.message || t('editor.saveFailed'))
    } finally {
      saving.value = false
    }
  }

  const handleClose = () => {
    if (dirty.value) {
      ElMessageBox.confirm(t('editor.closeConfirm'), t('editor.closeTitle'), {
        type: 'warning',
        confirmButtonText: t('editor.discard'),
        cancelButtonText: t('common.cancel')
      })
        .then(() => {
          visible.value = false
        })
        .catch(() => {
          // 用户取消
        })
      return
    }
    visible.value = false
  }

  const handleClosed = () => {
    reset()
  }

  const formatSize = (bytes: number) => {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
  }
</script>

<style scoped>
  .file-editor-dialog :deep(.el-dialog__body) {
    padding: 20px;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .editor-unlock,
  .editor-loading {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: 280px;
    gap: 16px;
  }

  .unlock-icon {
    color: var(--el-color-warning);
  }

  .unlock-title {
    font-size: 18px;
    font-weight: 600;
    color: var(--text-primary);
    margin: 0;
  }

  .unlock-desc {
    font-size: 14px;
    color: var(--text-secondary);
    margin: 0;
  }

  .unlock-input {
    max-width: 320px;
  }

  .unlock-actions {
    display: flex;
    gap: 12px;
    margin-top: 8px;
  }

  .editor-loading p {
    margin: 0;
    color: var(--text-secondary);
  }

  .editor-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-height: 0;
    height: calc(80vh - 140px);
  }

  .editor-meta {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 8px 12px;
    background: var(--bg-color);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    flex-wrap: wrap;
  }

  .meta-item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 13px;
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 40%;
  }

  .editor-main {
    flex: 1;
    min-height: 0;
  }

  @media (max-width: 768px) {
    .meta-item {
      max-width: 100%;
    }
  }
</style>
