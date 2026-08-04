<template>
  <div class="personal-dictionary">
    <div class="settings-section-header">
      <div>
        <h3>{{ t('settings.tagDictionary.title') }}</h3>
        <p>{{ t('settings.tagDictionary.description') }}</p>
      </div>
      <el-button type="primary" :loading="saving" icon="Check" @click="save">{{ t('common.save') }}</el-button>
    </div>

    <el-alert v-if="lastJob" type="success" :closable="false" show-icon>
      {{ t('settings.tagDictionary.rebuildQueued', { total: lastJob.total }) }}
    </el-alert>

    <div class="dictionary-toolbar">
      <el-button icon="Plus" @click="addRule('word')">{{ t('tags.ruleType.word') }}</el-button>
      <el-button icon="Plus" @click="addRule('stop_word')">{{ t('tags.ruleType.stop_word') }}</el-button>
      <el-button icon="Plus" @click="addRule('alias')">{{ t('tags.ruleType.alias') }}</el-button>
      <span>{{ t('settings.tagDictionary.ruleCount', { count: rules.length }) }}</span>
    </div>

    <el-table v-loading="loading" :data="rules" row-key="id" empty-text="暂无个人词典规则">
      <el-table-column :label="t('tags.ruleTypeLabel')" width="130">
        <template #default="{ row }">
          <el-select v-model="row.type">
            <el-option :label="t('tags.ruleType.word')" value="word" />
            <el-option :label="t('tags.ruleType.stop_word')" value="stop_word" />
            <el-option :label="t('tags.ruleType.alias')" value="alias" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('tags.pattern')" min-width="180">
        <template #default="{ row }">
          <el-input v-model="row.pattern" maxlength="64" show-word-limit />
        </template>
      </el-table-column>
      <el-table-column :label="t('tags.replacement')" min-width="160">
        <template #default="{ row }">
          <el-input v-if="row.type === 'alias'" v-model="row.replacement" maxlength="64" show-word-limit />
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column :label="t('tags.category')" min-width="140">
        <template #default="{ row }">
          <el-select v-model="row.category_id" :disabled="row.type === 'stop_word'">
            <el-option v-for="category in categories" :key="category.id" :value="category.id" :label="category.name" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('tags.priority')" width="105">
        <template #default="{ row }"
          ><el-input-number v-model="row.priority" :min="-1000" :max="1000" controls-position="right"
        /></template>
      </el-table-column>
      <el-table-column :label="t('common.status')" width="90">
        <template #default="{ row }"><el-switch v-model="row.enabled" /></template>
      </el-table-column>
      <el-table-column :label="t('common.operation')" width="76" fixed="right">
        <template #default="{ $index }">
          <el-button text type="danger" icon="Delete" @click="rules.splice($index, 1)" />
        </template>
      </el-table-column>
    </el-table>

    <section class="dictionary-preview">
      <div class="settings-section-header">
        <div>
          <h4>{{ t('settings.tagDictionary.previewTitle') }}</h4>
          <p>{{ t('settings.tagDictionary.previewDescription') }}</p>
        </div>
        <el-button :loading="previewing" icon="View" @click="preview">{{ t('tags.preview') }}</el-button>
      </div>
      <el-input
        v-model="sampleText"
        type="textarea"
        :rows="5"
        :placeholder="t('settings.tagDictionary.samplePlaceholder')"
      />
      <div v-if="previewItems.length" class="preview-results">
        <div v-for="item in previewItems" :key="item.input" class="preview-item">
          <strong>{{ item.input }}</strong>
          <FileTags :tags="item.tags" :limit="20" />
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
  import { getCurrentInstance, onMounted, ref, type ComponentInternalInstance } from 'vue'
  import {
    getEnabledTagCategories,
    getPersonalTagDictionary,
    previewPersonalTagDictionary,
    updatePersonalTagDictionary,
    type TagCategory,
    type TagPreviewItem,
    type TagRebuildJob,
    type TagRuleInput
  } from '@/api/tag'
  import FileTags from '@/components/FileTags/index.vue'
  import { useI18n } from '@/composables'

  const { t } = useI18n()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const loading = ref(false)
  const saving = ref(false)
  const previewing = ref(false)
  const rules = ref<TagRuleInput[]>([])
  const categories = ref<TagCategory[]>([])
  const lastJob = ref<TagRebuildJob>()
  const sampleText = ref('流浪地球2.2023.2160p.WEB-DL.H265.国语.mkv\n三体.S01E08.1080p.中英字幕.mp4')
  const previewItems = ref<TagPreviewItem[]>([])

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

  const load = async () => {
    loading.value = true
    try {
      const [dictionaryResponse, categoryResponse] = await Promise.all([
        getPersonalTagDictionary(),
        getEnabledTagCategories()
      ])
      if (dictionaryResponse.code === 200) {
        rules.value = (dictionaryResponse.data?.rules || []).map(normalizeRule)
      }
      if (categoryResponse.code === 200) {
        categories.value = categoryResponse.data || []
      }
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('settings.tagDictionary.loadFailed'))
    } finally {
      loading.value = false
    }
  }

  const addRule = (type: TagRuleInput['type']) => {
    rules.value.push({
      type,
      target_field: 'basename',
      pattern: '',
      replacement: '',
      category_id: type === 'stop_word' ? 'other' : categories.value.find(item => item.code === 'title')?.id || 'other',
      priority: 0,
      weight: 1,
      enabled: true
    })
  }

  const validateRules = () => {
    if (rules.value.some(rule => !rule.pattern.trim())) {
      throw new Error(t('settings.tagDictionary.patternRequired'))
    }
    if (rules.value.some(rule => rule.type === 'alias' && !rule.replacement?.trim())) {
      throw new Error(t('settings.tagDictionary.aliasRequired'))
    }
  }

  const save = async () => {
    saving.value = true
    try {
      validateRules()
      const response = await updatePersonalTagDictionary(rules.value)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      rules.value = (response.data.rule_set.rules || []).map(normalizeRule)
      lastJob.value = response.data.rebuild_job
      proxy?.$modal.msgSuccess(t('settings.tagDictionary.saveSuccess'))
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('settings.tagDictionary.saveFailed'))
    } finally {
      saving.value = false
    }
  }

  const preview = async () => {
    previewing.value = true
    try {
      validateRules()
      const samples = sampleText.value
        .split(/\r?\n/)
        .map(item => item.trim())
        .filter(Boolean)
        .slice(0, 100)
      if (!samples.length) {
        throw new Error(t('settings.tagDictionary.sampleRequired'))
      }
      const response = await previewPersonalTagDictionary(samples, rules.value)
      if (response.code !== 200) {
        throw new Error(response.message)
      }
      previewItems.value = response.data || []
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : t('settings.tagDictionary.previewFailed'))
    } finally {
      previewing.value = false
    }
  }

  onMounted(load)
</script>

<style scoped>
  .personal-dictionary {
    display: grid;
    gap: 18px;
  }
  .settings-section-header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .settings-section-header h3,
  .settings-section-header h4,
  .settings-section-header p {
    margin: 0;
  }
  .settings-section-header p,
  .dictionary-toolbar span,
  .muted {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .dictionary-toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .dictionary-toolbar span {
    margin-left: auto;
  }
  .dictionary-preview {
    display: grid;
    gap: 12px;
    padding-top: 18px;
    border-top: 1px solid var(--el-border-color-lighter);
  }
  .preview-results {
    display: grid;
    gap: 10px;
  }
  .preview-item {
    min-width: 0;
    padding: 12px;
    display: grid;
    gap: 8px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 10px;
    background: var(--el-fill-color-lighter);
  }
  .preview-item strong {
    overflow-wrap: anywhere;
  }
  @media (max-width: 767px) {
    .settings-section-header {
      align-items: stretch;
      flex-direction: column;
    }
    .personal-dictionary :deep(.el-table) {
      font-size: 12px;
    }
  }
</style>
