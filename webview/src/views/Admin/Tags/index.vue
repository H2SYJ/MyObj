<template>
  <div class="admin-tags">
    <header class="admin-tags__header">
      <div>
        <h2>{{ t('admin.tags.title') }}</h2>
        <p>{{ t('admin.tags.description') }}</p>
      </div>
      <el-button icon="Refresh" :loading="loading" @click="loadAll">{{ t('common.refresh') }}</el-button>
    </header>

    <el-alert v-if="settings?.degraded" type="error" :closable="false" show-icon>
      <template #title>{{ t('admin.tags.degraded') }}</template>
      {{ settings.degraded_reason }}
    </el-alert>

    <el-card shadow="never" class="tag-settings-card">
      <el-form v-if="settings" inline label-position="top">
        <el-form-item :label="t('admin.tags.autoEnabled')">
          <el-switch v-model="settings.enabled" />
        </el-form-item>
        <el-form-item :label="t('admin.tags.autoLimit')">
          <el-input-number v-model="settings.limit" :min="1" :max="100" />
        </el-form-item>
        <el-form-item :label="t('admin.tags.activeVersion')">
          <el-tag type="primary">v{{ settings.active_version }}</el-tag>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="savingSettings" @click="saveSettings">{{ t('common.save') }}</el-button>
        </el-form-item>
      </el-form>
      <div v-if="settings" class="provider-status">
        <span>{{ t('admin.tags.providers') }}</span>
        <el-tag type="success">Basic</el-tag>
        <el-tag type="success">Image</el-tag>
        <el-tag :type="settings.providers.ffprobe.available ? 'success' : 'info'">
          ffprobe · {{ settings.providers.ffprobe.available ? t('common.available') : t('common.unavailable') }}
        </el-tag>
        <el-tag v-for="(count, state) in settings.providers.states" :key="state" effect="plain">
          {{ state }}: {{ count }}
        </el-tag>
      </div>
    </el-card>

    <el-tabs v-model="activeTab" class="admin-tags__tabs">
      <el-tab-pane :label="t('admin.tags.categories')" name="categories">
        <div class="tab-toolbar">
          <el-button type="primary" icon="Plus" @click="openCategory()">{{ t('admin.tags.addCategory') }}</el-button>
        </div>
        <el-table :data="categories" row-key="id">
          <el-table-column :label="t('admin.tags.categoryCode')" prop="code" min-width="130" />
          <el-table-column :label="t('admin.tags.categoryName')" prop="name" min-width="130" />
          <el-table-column :label="t('admin.tags.color')" width="110">
            <template #default="{ row }"
              ><span class="color-dot" :style="{ backgroundColor: row.color }"></span>{{ row.color }}</template
            >
          </el-table-column>
          <el-table-column :label="t('admin.tags.sortOrder')" prop="sort_order" width="90" />
          <el-table-column :label="t('common.status')" width="90">
            <template #default="{ row }"
              ><el-tag :type="row.enabled ? 'success' : 'info'">{{
                row.enabled ? t('common.enabled') : t('common.disabled')
              }}</el-tag></template
            >
          </el-table-column>
          <el-table-column :label="t('admin.tags.builtin')" width="90">
            <template #default="{ row }"
              ><el-tag v-if="row.builtin" effect="plain">{{ t('common.yes') }}</el-tag></template
            >
          </el-table-column>
          <el-table-column :label="t('common.operation')" width="150" fixed="right">
            <template #default="{ row }">
              <el-button text type="primary" @click="openCategory(row)">{{ t('common.edit') }}</el-button>
              <el-button v-if="!row.builtin" text type="danger" @click="removeCategory(row)">{{
                t('common.delete')
              }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('admin.tags.draftEditor')" name="draft">
        <div v-if="!draft" class="empty-draft">
          <el-empty :description="t('admin.tags.noDraft')" />
          <el-button type="primary" :loading="creatingDraft" @click="createDraft">{{
            t('admin.tags.createDraft')
          }}</el-button>
        </div>
        <template v-else>
          <div class="tab-toolbar tab-toolbar--wrap">
            <el-tag type="warning">v{{ draft.version }} · revision {{ draft.revision }}</el-tag>
            <el-button icon="Plus" @click="addRule('word')">{{ t('tags.ruleType.word') }}</el-button>
            <el-button icon="Plus" @click="addRule('stop_word')">{{ t('tags.ruleType.stop_word') }}</el-button>
            <el-button icon="Plus" @click="addRule('alias')">{{ t('tags.ruleType.alias') }}</el-button>
            <el-button icon="Plus" @click="addRule('regex')">{{ t('tags.ruleType.regex') }}</el-button>
            <el-button icon="Upload" @click="importInput?.click()">{{ t('common.import') }}</el-button>
            <input
              ref="importInput"
              class="visually-hidden"
              type="file"
              accept=".json,.csv,application/json,text/csv"
              @change="handleImport"
            />
            <el-dropdown @command="format => exportRules(draft!.id, format)">
              <el-button icon="Download">{{ t('common.export') }}</el-button>
              <template #dropdown
                ><el-dropdown-menu
                  ><el-dropdown-item command="json">JSON</el-dropdown-item
                  ><el-dropdown-item command="csv">CSV</el-dropdown-item></el-dropdown-menu
                ></template
              >
            </el-dropdown>
            <span class="toolbar-spacer"></span>
            <el-button :loading="savingDraft" @click="saveDraft">{{ t('common.saveDraft') }}</el-button>
            <el-button type="primary" :loading="publishing" @click="publishDraft">{{
              t('admin.tags.publish')
            }}</el-button>
          </div>

          <el-table :data="draftRules" row-key="id" class="rules-table">
            <el-table-column :label="t('tags.ruleTypeLabel')" width="125">
              <template #default="{ row }"
                ><el-select v-model="row.type"
                  ><el-option
                    v-for="type in ruleTypes"
                    :key="type"
                    :value="type"
                    :label="t(`tags.ruleType.${type}`)" /></el-select
              ></template>
            </el-table-column>
            <el-table-column :label="t('admin.tags.targetField')" width="125">
              <template #default="{ row }"
                ><el-select v-model="row.target_field"
                  ><el-option label="basename" value="basename" /><el-option
                    label="extension"
                    value="extension" /><el-option label="mime" value="mime" /><el-option
                    label="metadata.resolution"
                    value="metadata.resolution" /></el-select
              ></template>
            </el-table-column>
            <el-table-column :label="t('tags.pattern')" min-width="210"
              ><template #default="{ row }"><el-input v-model="row.pattern" /></template
            ></el-table-column>
            <el-table-column :label="t('tags.replacement')" min-width="160"
              ><template #default="{ row }"
                ><el-input
                  v-model="row.replacement"
                  :disabled="row.type === 'word' || row.type === 'stop_word'" /></template
            ></el-table-column>
            <el-table-column :label="t('tags.category')" width="135"
              ><template #default="{ row }"
                ><el-select v-model="row.category_id"
                  ><el-option
                    v-for="category in categories"
                    :key="category.id"
                    :value="category.id"
                    :label="category.name" /></el-select></template
            ></el-table-column>
            <el-table-column :label="t('tags.priority')" width="105"
              ><template #default="{ row }"
                ><el-input-number v-model="row.priority" :min="-1000" :max="1000" controls-position="right" /></template
            ></el-table-column>
            <el-table-column :label="t('tags.weight')" width="95"
              ><template #default="{ row }"
                ><el-input-number
                  v-model="row.weight"
                  :min="0.001"
                  :max="999"
                  :step="0.1"
                  controls-position="right" /></template
            ></el-table-column>
            <el-table-column :label="t('common.status')" width="75"
              ><template #default="{ row }"><el-switch v-model="row.enabled" /></template
            ></el-table-column>
            <el-table-column width="60" fixed="right"
              ><template #default="{ $index }"
                ><el-button text type="danger" icon="Delete" @click="draftRules.splice($index, 1)" /></template
            ></el-table-column>
          </el-table>

          <section class="draft-preview">
            <div class="section-heading">
              <div>
                <h3>{{ t('admin.tags.previewTitle') }}</h3>
                <p>{{ t('admin.tags.previewDescription') }}</p>
              </div>
              <el-button icon="View" :loading="previewing" @click="previewDraft">{{ t('tags.preview') }}</el-button>
            </div>
            <el-input v-model="sampleText" type="textarea" :rows="4" :placeholder="t('admin.tags.samplePlaceholder')" />
            <div v-if="previewItems.length" class="preview-results">
              <div v-for="item in previewItems" :key="item.input" class="preview-item">
                <strong>{{ item.input }}</strong
                ><FileTags :tags="item.tags" :limit="30" />
              </div>
            </div>
          </section>
        </template>
      </el-tab-pane>

      <el-tab-pane :label="t('admin.tags.versions')" name="versions">
        <el-table :data="ruleSets" row-key="id">
          <el-table-column label="Version" width="90"
            ><template #default="{ row }">v{{ row.version }}</template></el-table-column
          >
          <el-table-column :label="t('common.status')" width="120"
            ><template #default="{ row }"
              ><el-tag :type="ruleStatusType(row.status)">{{ row.status }}</el-tag></template
            ></el-table-column
          >
          <el-table-column :label="t('admin.tags.basedOn')" width="100"
            ><template #default="{ row }">{{
              row.based_on_version ? `v${row.based_on_version}` : '—'
            }}</template></el-table-column
          >
          <el-table-column :label="t('admin.tags.publishedAt')" min-width="170" prop="published_at" />
          <el-table-column :label="t('common.operation')" min-width="260" fixed="right">
            <template #default="{ row }">
              <el-button text @click="showDiff(row)">{{ t('admin.tags.diff') }}</el-button>
              <el-button text @click="exportRules(row.id, 'json')">JSON</el-button>
              <el-button text @click="exportRules(row.id, 'csv')">CSV</el-button>
              <el-button v-if="row.status !== 'draft'" text type="warning" @click="rollback(row)">{{
                t('admin.tags.rollback')
              }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <el-tab-pane :label="t('admin.tags.rebuildJobs')" name="jobs">
        <el-table :data="jobs" row-key="id">
          <el-table-column :label="t('common.status')" width="180"
            ><template #default="{ row }"
              ><el-tag :type="jobStatusType(row.status)">{{ row.status }}</el-tag></template
            ></el-table-column
          >
          <el-table-column label="Version" width="90"
            ><template #default="{ row }">v{{ row.target_version }}</template></el-table-column
          >
          <el-table-column :label="t('admin.tags.progress')" min-width="240"
            ><template #default="{ row }"
              ><el-progress :percentage="jobProgress(row)" :status="row.failed ? 'warning' : undefined" /><small
                >{{ row.processed }}/{{ row.total }} · {{ t('admin.tags.failedCount', { count: row.failed }) }}</small
              ></template
            ></el-table-column
          >
          <el-table-column :label="t('admin.tags.lastError')" min-width="200" show-overflow-tooltip prop="last_error" />
          <el-table-column :label="t('common.operation')" width="220" fixed="right"
            ><template #default="{ row }"
              ><el-button v-if="row.failed > 0" text @click="openJobFailures(row)">{{
                t('admin.tags.failureDetails')
              }}</el-button
              ><el-button
                v-if="row.status === 'pending' || row.status === 'running'"
                text
                type="danger"
                @click="cancelJob(row)"
                >{{ t('common.cancel') }}</el-button
              ><el-button
                v-if="['failed', 'completed_with_errors', 'cancelled'].includes(row.status)"
                text
                type="primary"
                @click="retryJob(row)"
                >{{ t('common.retry') }}</el-button
              ></template
            ></el-table-column
          >
        </el-table>
      </el-tab-pane>
    </el-tabs>

    <el-dialog
      v-model="categoryDialog"
      :title="categoryForm.id ? t('admin.tags.editCategory') : t('admin.tags.addCategory')"
      width="480px"
    >
      <el-form label-position="top"
        ><el-form-item :label="t('admin.tags.categoryCode')"
          ><el-input v-model="categoryForm.code" :disabled="categoryForm.builtin" /></el-form-item
        ><el-form-item :label="t('admin.tags.categoryName')"><el-input v-model="categoryForm.name" /></el-form-item
        ><el-form-item :label="t('admin.tags.color')"
          ><el-color-picker v-model="categoryForm.color" show-alpha /></el-form-item
        ><el-form-item :label="t('admin.tags.sortOrder')"
          ><el-input-number v-model="categoryForm.sort_order" /></el-form-item
        ><el-form-item :label="t('common.status')"><el-switch v-model="categoryForm.enabled" /></el-form-item
      ></el-form>
      <template #footer
        ><el-button @click="categoryDialog = false">{{ t('common.cancel') }}</el-button
        ><el-button type="primary" :loading="savingCategory" @click="saveCategory">{{
          t('common.save')
        }}</el-button></template
      >
    </el-dialog>

    <el-dialog v-model="diffDialog" :title="t('admin.tags.diffTitle')" width="min(900px, 94vw)">
      <div v-if="diffData" class="diff-grid">
        <section>
          <h4>{{ t('admin.tags.baseVersion') }} {{ diffData.base ? `v${diffData.base.version}` : '—' }}</h4>
          <pre>{{ formatRules(diffData.base?.rules || []) }}</pre>
        </section>
        <section>
          <h4>{{ t('admin.tags.targetVersion') }} v{{ diffData.target.version }}</h4>
          <pre>{{ formatRules(diffData.target.rules || []) }}</pre>
        </section>
      </div>
    </el-dialog>

    <el-dialog v-model="failureDialog" :title="t('admin.tags.failureDetails')" width="min(900px, 94vw)">
      <el-table v-loading="loadingFailures" :data="failures" row-key="uf_id">
        <el-table-column label="uf_id" min-width="220" prop="uf_id" />
        <el-table-column :label="t('common.status')" width="110">
          <template #default="{ row }"
            ><el-tag :type="failureStatusType(row.status)">{{ row.status }}</el-tag></template
          >
        </el-table-column>
        <el-table-column :label="t('admin.tags.retryCount')" width="100" prop="retry_count" />
        <el-table-column :label="t('admin.tags.lastError')" min-width="260" prop="error" show-overflow-tooltip />
        <el-table-column :label="t('common.operation')" width="100" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'failed'" text type="primary" @click="retryFailure(row)">{{
              t('common.retry')
            }}</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
  import {
    cancelTagRebuildJob,
    createGlobalDraft,
    deleteTagCategory,
    exportGlobalRuleSet,
    getAdminTagSettings,
    getGlobalRuleSet,
    getGlobalRuleSets,
    getRuleSetDiff,
    getTagCategories,
    getTagRebuildFailures,
    getTagRebuildJobs,
    importGlobalDraft,
    previewGlobalDraft,
    publishGlobalDraft,
    retryTagRebuildFailure,
    retryTagRebuildJob,
    rollbackGlobalRuleSet,
    saveGlobalDraft,
    saveTagCategory,
    updateAdminTagSettings,
    type AdminTagSettings,
    type TagCategory,
    type TagPreviewItem,
    type TagRebuildFailure,
    type TagRebuildJob,
    type TagRuleInput,
    type TagRuleSet
  } from '@/api/tag'
  import FileTags from '@/components/FileTags/index.vue'
  import { useI18n } from '@/composables'

  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const activeTab = ref('draft')
  const loading = ref(false)
  const savingSettings = ref(false)
  const creatingDraft = ref(false)
  const savingDraft = ref(false)
  const publishing = ref(false)
  const previewing = ref(false)
  const savingCategory = ref(false)
  const settings = ref<AdminTagSettings>()
  const categories = ref<TagCategory[]>([])
  const ruleSets = ref<TagRuleSet[]>([])
  const jobs = ref<TagRebuildJob[]>([])
  const failures = ref<TagRebuildFailure[]>([])
  const draft = ref<TagRuleSet>()
  const draftRules = ref<TagRuleInput[]>([])
  const previewItems = ref<TagPreviewItem[]>([])
  const sampleText = ref('流浪地球2.2023.2160p.WEB-DL.H265.国语.mkv\n三体.S01E08.1080p.中英字幕.mp4')
  const importInput = ref<HTMLInputElement>()
  const categoryDialog = ref(false)
  const categoryForm = reactive<TagCategory>({
    id: '',
    code: '',
    name: '',
    color: '#409eff',
    sort_order: 0,
    enabled: true,
    builtin: false
  })
  const diffDialog = ref(false)
  const diffData = ref<{ base?: TagRuleSet; target: TagRuleSet }>()
  const failureDialog = ref(false)
  const failureJob = ref<TagRebuildJob>()
  const loadingFailures = ref(false)
  const ruleTypes: TagRuleInput['type'][] = ['word', 'stop_word', 'alias', 'regex']
  let jobTimer: ReturnType<typeof setInterval> | undefined

  const normalizeRule = (rule: TagRuleInput): TagRuleInput => ({
    id: rule.id,
    type: rule.type,
    target_field: rule.target_field || 'basename',
    pattern: rule.pattern || '',
    replacement: rule.replacement || '',
    category_id: rule.category_id || 'other',
    priority: rule.priority || 0,
    weight: rule.weight || 1,
    enabled: rule.enabled !== false
  })

  const loadRuleSets = async () => {
    const response = await getGlobalRuleSets()
    if (response.code !== 200) {
      throw new Error(response.message)
    }
    ruleSets.value = response.data || []
    const draftSummary = ruleSets.value.find(item => item.status === 'draft')
    if (!draftSummary) {
      draft.value = undefined
      draftRules.value = []
      return
    }
    const detailResponse = await getGlobalRuleSet(draftSummary.id)
    if (detailResponse.code !== 200) {
      throw new Error(detailResponse.message)
    }
    draft.value = detailResponse.data
    draftRules.value = (detailResponse.data.rules || []).map(normalizeRule)
  }

  const loadJobs = async () => {
    const response = await getTagRebuildJobs()
    if (response.code === 200) {
      jobs.value = response.data || []
    }
  }

  const loadAll = async () => {
    loading.value = true
    try {
      const [settingsResponse, categoryResponse] = await Promise.all([getAdminTagSettings(), getTagCategories()])
      if (settingsResponse.code !== 200) {
        throw new Error(settingsResponse.message)
      }
      if (categoryResponse.code !== 200) {
        throw new Error(categoryResponse.message)
      }
      settings.value = settingsResponse.data
      categories.value = categoryResponse.data || []
      await Promise.all([loadRuleSets(), loadJobs()])
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('admin.tags.loadFailed'))
    } finally {
      loading.value = false
    }
  }

  const saveSettings = async () => {
    if (!settings.value) {
      return
    }
    savingSettings.value = true
    try {
      const response = await updateAdminTagSettings(settings.value.enabled, settings.value.limit)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      settings.value = response.data
      await loadJobs()
      proxy?.$modal.msgSuccess(t('admin.tags.settingsSaved'))
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
    } finally {
      savingSettings.value = false
    }
  }

  const openCategory = (category?: TagCategory) => {
    Object.assign(
      categoryForm,
      category || { id: '', code: '', name: '', color: '#409eff', sort_order: 0, enabled: true, builtin: false }
    )
    categoryDialog.value = true
  }
  const saveCategory = async () => {
    savingCategory.value = true
    try {
      const response = await saveTagCategory({ ...categoryForm })
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      categoryDialog.value = false
      const categoriesResponse = await getTagCategories()
      categories.value = categoriesResponse.data || []
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
    } finally {
      savingCategory.value = false
    }
  }
  const removeCategory = async (category: TagCategory) => {
    try {
      await proxy?.$modal.confirm(t('admin.tags.deleteCategoryConfirm', { name: category.name }))
      const response = await deleteTagCategory(category.id)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      categories.value = categories.value.filter(item => item.id !== category.id)
    } catch (error) {
      if (error instanceof Error) {
        proxy?.$modal.msgError(error.message)
      }
    }
  }

  const createDraft = async () => {
    creatingDraft.value = true
    try {
      const response = await createGlobalDraft()
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      await loadRuleSets()
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
    } finally {
      creatingDraft.value = false
    }
  }
  const addRule = (type: TagRuleInput['type']) =>
    draftRules.value.push({
      type,
      target_field: 'basename',
      pattern: '',
      replacement: '',
      category_id: 'other',
      priority: 0,
      weight: 1,
      enabled: true
    })
  const validateRules = () => {
    if (draftRules.value.some(rule => !rule.pattern.trim())) {
      throw new Error(t('admin.tags.patternRequired'))
    }
    if (draftRules.value.some(rule => rule.type === 'alias' && !rule.replacement?.trim())) {
      throw new Error(t('admin.tags.aliasRequired'))
    }
  }
  const saveDraft = async () => {
    if (!draft.value) {
      return false
    }
    savingDraft.value = true
    try {
      validateRules()
      const response = await saveGlobalDraft(draft.value.id, draft.value.revision, draftRules.value)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      draft.value = response.data
      draftRules.value = (response.data.rules || []).map(normalizeRule)
      await loadRuleSets()
      proxy?.$modal.msgSuccess(t('admin.tags.draftSaved'))
      return true
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
      if (error instanceof Error && error.message.includes('冲突')) {
        await loadRuleSets()
      }
      return false
    } finally {
      savingDraft.value = false
    }
  }
  const previewDraft = async () => {
    if (!draft.value) {
      return
    }
    previewing.value = true
    try {
      validateRules()
      const samples = sampleText.value
        .split(/\r?\n/)
        .map(item => item.trim())
        .filter(Boolean)
        .slice(0, 100)
      if (!samples.length) {
        throw new Error(t('admin.tags.sampleRequired'))
      }
      const response = await previewGlobalDraft(draft.value.id, samples, draftRules.value)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      previewItems.value = response.data || []
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
    } finally {
      previewing.value = false
    }
  }
  const publishDraft = async () => {
    if (!draft.value) {
      return
    }
    publishing.value = true
    try {
      await proxy?.$modal.confirm(t('admin.tags.publishConfirm', { version: draft.value.version }))
      const saved = await saveDraft()
      if (!saved) {
        return
      }
      if (!draft.value) {
        return
      }
      const response = await publishGlobalDraft(draft.value.id)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      proxy?.$modal.msgSuccess(t('admin.tags.publishSuccess', { version: response.data.active_version }))
      await loadAll()
    } catch (error) {
      if (error instanceof Error) {
        proxy?.$modal.msgError(error.message)
      }
    } finally {
      publishing.value = false
    }
  }
  const handleImport = async (event: Event) => {
    if (!draft.value) {
      return
    }
    const input = event.target as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) {
      return
    }
    try {
      const format = file.name.toLowerCase().endsWith('.csv') ? 'csv' : 'json'
      const response = await importGlobalDraft(draft.value.id, draft.value.revision, format, file)
      draft.value = response.data
      draftRules.value = (response.data.rules || []).map(normalizeRule)
      await loadRuleSets()
      proxy?.$modal.msgSuccess(t('admin.tags.importSuccess'))
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('admin.tags.importFailed'))
    }
  }
  const exportRules = async (id: string, format: 'json' | 'csv') => {
    try {
      await exportGlobalRuleSet(id, format)
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('admin.tags.exportFailed'))
    }
  }
  const showDiff = async (ruleSet: TagRuleSet) => {
    const response = await getRuleSetDiff(ruleSet.id)
    if (response.code !== 200) {
      return proxy?.$modal.msgError(response.message)
    }
    diffData.value = response.data
    diffDialog.value = true
  }
  const rollback = async (ruleSet: TagRuleSet) => {
    try {
      await proxy?.$modal.confirm(t('admin.tags.rollbackConfirm', { version: ruleSet.version }))
      const response = await rollbackGlobalRuleSet(ruleSet.id)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      await loadAll()
    } catch (error) {
      if (error instanceof Error) {
        proxy?.$modal.msgError(error.message)
      }
    }
  }
  const cancelJob = async (job: TagRebuildJob) => {
    const response = await cancelTagRebuildJob(job.id)
    if (response.code === 200) {
      await loadJobs()
    }
  }
  const retryJob = async (job: TagRebuildJob) => {
    const response = await retryTagRebuildJob(job.id)
    if (response.code === 200) {
      await loadJobs()
    }
  }
  const loadFailures = async () => {
    if (!failureJob.value) {
      return
    }
    loadingFailures.value = true
    try {
      const response = await getTagRebuildFailures(failureJob.value.id, '', 100)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      failures.value = response.data || []
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('common.operationFailed'))
    } finally {
      loadingFailures.value = false
    }
  }
  const openJobFailures = async (job: TagRebuildJob) => {
    failureJob.value = job
    failureDialog.value = true
    await loadFailures()
  }
  const retryFailure = async (failure: TagRebuildFailure) => {
    const response = await retryTagRebuildFailure(failure.job_id, failure.uf_id)
    if (response.code !== 200) {
      return proxy?.$modal.msgError(response.message)
    }
    await loadFailures()
  }
  const jobProgress = (job: TagRebuildJob) =>
    job.total > 0 ? Math.min(100, Math.round((job.processed / job.total) * 100)) : 0
  const ruleStatusType = (status: string) => (status === 'active' ? 'success' : status === 'draft' ? 'warning' : 'info')
  const jobStatusType = (status: string) =>
    status === 'completed'
      ? 'success'
      : status === 'failed' || status === 'completed_with_errors'
        ? 'danger'
        : status === 'running'
          ? 'primary'
          : 'info'
  const failureStatusType = (status: string) =>
    status === 'resolved' ? 'success' : status === 'retrying' ? 'primary' : 'danger'
  const formatRules = (rules: TagRuleInput[]) =>
    JSON.stringify(
      rules.map(({ id: _id, ...rule }) => rule),
      null,
      2
    )

  onMounted(() => {
    loadAll()
    jobTimer = setInterval(loadJobs, 5000)
  })
  onBeforeUnmount(() => {
    if (jobTimer) {
      clearInterval(jobTimer)
    }
  })
</script>

<style scoped>
  .admin-tags {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .admin-tags__header,
  .section-heading {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .admin-tags__header h2,
  .admin-tags__header p,
  .section-heading h3,
  .section-heading p {
    margin: 0;
  }
  .admin-tags__header p,
  .section-heading p,
  .provider-status,
  .el-progress + small {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .tag-settings-card :deep(.el-card__body) {
    padding-bottom: 10px;
  }
  .provider-status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .admin-tags__tabs {
    min-height: 0;
    flex: 1;
  }
  .admin-tags__tabs :deep(.el-tabs__content),
  .admin-tags__tabs :deep(.el-tab-pane) {
    min-height: 0;
  }
  .tab-toolbar {
    margin-bottom: 12px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .tab-toolbar--wrap {
    flex-wrap: wrap;
  }
  .toolbar-spacer {
    flex: 1;
  }
  .color-dot {
    width: 12px;
    height: 12px;
    margin-right: 6px;
    display: inline-block;
    border-radius: 50%;
    vertical-align: -1px;
  }
  .empty-draft {
    display: grid;
    justify-items: center;
  }
  .rules-table :deep(.el-input-number) {
    width: 100%;
  }
  .draft-preview {
    margin-top: 18px;
    display: grid;
    gap: 12px;
  }
  .preview-results {
    display: grid;
    gap: 8px;
  }
  .preview-item {
    padding: 10px;
    display: grid;
    gap: 8px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    background: var(--el-fill-color-lighter);
  }
  .visually-hidden {
    position: fixed;
    width: 1px;
    height: 1px;
    opacity: 0;
    pointer-events: none;
  }
  .diff-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
  }
  .diff-grid section {
    min-width: 0;
  }
  .diff-grid pre {
    max-height: 60vh;
    padding: 12px;
    overflow: auto;
    border-radius: 8px;
    background: var(--el-fill-color-lighter);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 767px) {
    .admin-tags {
      height: auto;
      padding: 12px;
    }
    .admin-tags__header,
    .section-heading {
      align-items: stretch;
      flex-direction: column;
    }
    .diff-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
