<template>
  <el-dialog
    :model-value="modelValue"
    :title="`管理文件夹标签：${directoryName}`"
    width="min(620px, 94vw)"
    append-to-body
    @update:model-value="$emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="directory-tag-manager">
      <section>
        <h4>当前标签</h4>
        <div v-if="details?.tags.length" class="tag-list">
          <el-tag
            v-for="tag in details.tags"
            :key="tag.id"
            closable
            effect="plain"
            :style="tagStyle(tag.category.color)"
            @close="removeTag(tag.id)"
          >
            {{ tag.name }}
          </el-tag>
        </div>
        <el-empty v-else :image-size="64" description="尚未添加标签" />
      </section>

      <el-form label-position="top">
        <el-form-item label="标签名称">
          <el-select
            v-model="newTagNames"
            multiple
            filterable
            allow-create
            default-first-option
            remote
            :remote-method="loadSuggestions"
            :loading="suggestionsLoading"
            placeholder="输入或选择标签，影视文件夹请选择“影视模式”"
          >
            <el-option v-for="tag in suggestions" :key="tag.id" :label="tag.name" :value="tag.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="标签分类">
          <el-select v-model="categoryId">
            <el-option v-for="category in categories" :key="category.id" :label="category.name" :value="category.id" />
          </el-select>
        </el-form-item>
      </el-form>
    </div>
    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">关闭</el-button>
      <el-button type="primary" :loading="saving" :disabled="newTagNames.length === 0" @click="save"
        >添加标签</el-button
      >
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
  import { getCurrentInstance, ref, watch, type ComponentInternalInstance } from 'vue'
  import {
    getDirectoryTags,
    getEnabledTagCategories,
    getTagSuggestions,
    updateDirectoryTags,
    type DirectoryTagsData,
    type TagCategory
  } from '@/api/tag'
  import type { CompactTag } from '@/types'
  import { getTagStyle } from '@/utils/ui'

  const props = defineProps<{ modelValue: boolean; directoryId: number; directoryName: string }>()
  const emit = defineEmits<{ 'update:modelValue': [value: boolean]; saved: [] }>()
  const { proxy } = getCurrentInstance() as ComponentInternalInstance
  const tagStyle = (color?: string) => getTagStyle(color || '')
  const loading = ref(false)
  const saving = ref(false)
  const suggestionsLoading = ref(false)
  const details = ref<DirectoryTagsData>()
  const categories = ref<TagCategory[]>([])
  const suggestions = ref<CompactTag[]>([])
  const newTagNames = ref<string[]>([])
  const categoryId = ref('other')

  const reload = async () => {
    const response = await getDirectoryTags(props.directoryId)
    if (response.code !== 200 || !response.data) {
      throw new Error(response.message || '加载文件夹标签失败')
    }
    details.value = response.data
  }

  const loadSuggestions = async (keyword = '') => {
    suggestionsLoading.value = true
    try {
      const response = await getTagSuggestions({ keyword, target: 'directory', limit: 50 })
      if (response.code !== 200) {
        throw new Error(response.message || '加载标签建议失败')
      }
      suggestions.value = response.data || []
    } catch (error) {
      suggestions.value = []
      proxy?.$log.error('加载文件夹标签建议失败', error)
    } finally {
      suggestionsLoading.value = false
    }
  }

  const load = async () => {
    if (!props.directoryId) {
      return
    }
    loading.value = true
    try {
      const categoryResponse = await getEnabledTagCategories()
      categories.value = categoryResponse.code === 200 ? categoryResponse.data || [] : []
      categoryId.value =
        categories.value.find(category => category.code === 'other')?.id || categories.value[0]?.id || ''
      await Promise.all([reload(), loadSuggestions()])
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : '加载文件夹标签失败')
    } finally {
      loading.value = false
    }
  }

  const removeTag = async (tagId: string) => {
    try {
      const response = await updateDirectoryTags(props.directoryId, [], [tagId])
      if (response.code !== 200) {
        throw new Error(response.message || '删除标签失败')
      }
      await reload()
      emit('saved')
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : '删除标签失败')
    }
  }

  const save = async () => {
    saving.value = true
    try {
      const names = [...new Set(newTagNames.value.map(name => name.trim()).filter(Boolean))]
      if (names.length === 0) {
        return
      }
      const add = names.map(name => {
        const suggestion = suggestions.value.find(tag => tag.name === name)
        const suggestedCategory = categories.value.find(category => category.code === suggestion?.category_code)?.id
        return {
          name,
          category_id: suggestion?.system_code === 'cinema_mode' ? suggestedCategory || 'other' : categoryId.value
        }
      })
      const response = await updateDirectoryTags(props.directoryId, add, [])
      if (response.code !== 200) {
        throw new Error(response.message || '保存标签失败')
      }
      newTagNames.value = []
      await reload()
      emit('saved')
      proxy?.$modal.msgSuccess('文件夹标签已更新')
    } catch (error) {
      proxy?.$modal.msgError(error instanceof Error ? error.message : '保存标签失败')
    } finally {
      saving.value = false
    }
  }

  watch(
    () => [props.modelValue, props.directoryId] as const,
    ([visible]) => {
      if (visible) {
        details.value = undefined
        newTagNames.value = []
        void load()
      }
    },
    { immediate: true }
  )
</script>

<style scoped>
  .directory-tag-manager {
    min-height: 220px;
    display: grid;
    gap: 20px;
  }
  h4 {
    margin: 0 0 10px;
  }
  .tag-list {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  :deep(.el-select) {
    width: 100%;
  }
</style>
