<template>
  <div class="plugin-center">
    <el-alert :title="t('admin.plugins.warning')" type="warning" show-icon :closable="false" />
    <div class="toolbar">
      <input ref="fileInput" type="file" accept=".myobj-plugin" hidden @change="handleFile" />
      <el-button type="primary" icon="Upload" :loading="installing" @click="fileInput?.click()">{{
        t('admin.plugins.install')
      }}</el-button>
      <el-button icon="Refresh" @click="load">{{ t('common.refresh') }}</el-button>
    </div>
    <el-table v-if="!isHandheld" :data="plugins" v-loading="loading">
      <el-table-column prop="name" :label="t('admin.plugins.plugin')" min-width="180">
        <template #default="{ row }">
          <div>
            {{ row.name }} <el-tag size="small">v{{ row.version }}</el-tag>
          </div>
          <small>{{ row.id }}</small>
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.plugins.permissions')" min-width="260">
        <template #default="{ row }">
          <el-tag v-for="permission in row.permissions" :key="permission" size="small" class="permission">
            {{ permission }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.plugins.trust')" width="150">
        <template #default
          ><el-tag type="warning">{{ t('admin.plugins.unsignedTrusted') }}</el-tag></template
        >
      </el-table-column>
      <el-table-column :label="t('admin.plugins.status')" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.status === 'incompatible_api'" type="danger">{{ t('admin.plugins.incompatible') }}</el-tag>
          <el-switch v-else :model-value="row.enabled" @change="value => changeState(row, !!value)" />
        </template>
      </el-table-column>
      <el-table-column :label="t('admin.plugins.operation')" width="100">
        <template #default="{ row }"
          ><el-button link type="danger" @click="remove(row)">{{ t('admin.plugins.uninstall') }}</el-button></template
        >
      </el-table-column>
    </el-table>
    <div v-else v-loading="loading" class="mobile-admin-list">
      <article v-for="row in plugins" :key="row.id" class="mobile-admin-card">
        <div class="mobile-admin-card__header">
          <div class="mobile-admin-card__title">
            {{ row.name }} <el-tag size="small">v{{ row.version }}</el-tag>
          </div>
          <el-tag v-if="row.status === 'incompatible_api'" type="danger">{{ t('admin.plugins.incompatible') }}</el-tag>
          <el-switch v-else :model-value="row.enabled" @change="value => changeState(row, !!value)" />
        </div>
        <div class="mobile-admin-card__subtitle">{{ row.id }}</div>
        <div class="mobile-admin-card__meta">
          <el-tag v-for="permission in row.permissions" :key="permission" size="small">{{ permission }}</el-tag>
        </div>
        <div class="mobile-admin-card__footer">
          <el-button link type="danger" @click="remove(row)">{{ t('admin.plugins.uninstall') }}</el-button>
        </div>
      </article>
      <el-empty v-if="!loading && plugins.length === 0" :description="t('admin.plugins.empty')" />
    </div>
    <el-collapse class="audit">
      <el-collapse-item :title="t('admin.plugins.audit')" name="audit">
        <el-table v-if="!isHandheld" :data="auditRows" size="small">
          <el-table-column prop="created_at" :label="t('admin.plugins.time')" width="180" />
          <el-table-column prop="plugin_id" :label="t('admin.plugins.plugin')" min-width="160" />
          <el-table-column prop="action" :label="t('admin.plugins.operation')" width="120" />
          <el-table-column prop="status" :label="t('admin.plugins.result')" width="100" />
          <el-table-column prop="summary" :label="t('admin.plugins.summary')" min-width="240" />
        </el-table>
        <div v-else class="mobile-admin-list">
          <article
            v-for="row in auditRows"
            :key="`${row.created_at}-${row.plugin_id}-${row.action}`"
            class="mobile-admin-card"
          >
            <div class="mobile-admin-card__header">
              <div class="mobile-admin-card__title">{{ row.plugin_id }}</div>
              <el-tag size="small">{{ row.status }}</el-tag>
            </div>
            <div class="mobile-admin-card__subtitle">{{ row.summary }}</div>
            <div class="mobile-admin-card__meta">
              <span>{{ row.action }}</span
              ><span>{{ row.created_at }}</span>
            </div>
          </article>
        </div>
      </el-collapse-item>
    </el-collapse>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, ElMessageBox } from 'element-plus'
  import {
    inspectPlugin,
    installPlugin,
    listPluginAudit,
    listPlugins,
    togglePlugin,
    uninstallPlugin
  } from '@/api/plugin'
  import type { InstalledPlugin, PluginAudit } from '@/api/plugin'
  import { useI18n, useResponsive } from '@/composables'

  const { t } = useI18n()
  const { isHandheld } = useResponsive()

  const plugins = ref<InstalledPlugin[]>([])
  const auditRows = ref<PluginAudit[]>([])
  const loading = ref(false)
  const installing = ref(false)
  const fileInput = ref<HTMLInputElement>()

  const load = async () => {
    loading.value = true
    try {
      const [pluginResult, auditResult] = await Promise.all([listPlugins(), listPluginAudit()])
      plugins.value = pluginResult.data || []
      auditRows.value = auditResult.data?.items || []
    } catch (error) {
      ElMessage.error(error instanceof Error ? error.message : t('admin.plugins.loadFailed'))
    } finally {
      loading.value = false
    }
  }

  const handleFile = async (event: Event) => {
    const target = event.target as HTMLInputElement
    const file = target.files?.[0]
    target.value = ''
    if (!file) return
    installing.value = true
    let inspection
    try {
      inspection = await inspectPlugin(file)
    } catch (error) {
      ElMessage.error(error instanceof Error ? error.message : t('admin.plugins.inspectFailed'))
      return
    } finally {
      installing.value = false
    }
    if (inspection.code !== 200 || !inspection.data) return ElMessage.error(inspection.message)
    const manifest = inspection.data.manifest
    await ElMessageBox.confirm(
      t('admin.plugins.trustConfirm', {
        name: manifest.name,
        version: manifest.version,
        author: manifest.author || t('admin.plugins.unknown'),
        permissions: manifest.permissions.join(', ') || t('admin.plugins.none'),
        sha256: inspection.data.package_sha256
      }),
      t('admin.plugins.trustTitle'),
      { type: 'warning', confirmButtonText: t('admin.plugins.trustButton') }
    )
    installing.value = true
    try {
      const result = await installPlugin(file, manifest.permissions)
      if (result.code !== 200) throw new Error(result.message)
      ElMessage.success(t('admin.plugins.installSuccess'))
      await load()
    } finally {
      installing.value = false
    }
  }

  const changeState = async (plugin: InstalledPlugin, enabled: boolean) => {
    const result = await togglePlugin(plugin.id, enabled)
    if (result.code !== 200) return ElMessage.error(result.message)
    await load()
  }

  const remove = async (plugin: InstalledPlugin) => {
    await ElMessageBox.confirm(
      t('admin.plugins.uninstallConfirm', { name: plugin.name }),
      t('admin.plugins.uninstallTitle'),
      { type: 'warning' }
    )
    const result = await uninstallPlugin(plugin.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    ElMessage.success(t('admin.plugins.uninstallSuccess'))
    await load()
  }

  onMounted(load)
</script>

<style scoped>
  .plugin-center {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .toolbar {
    display: flex;
    gap: 10px;
  }
  .permission {
    margin: 2px 4px 2px 0;
  }
  small {
    color: var(--el-text-color-secondary);
  }
  .audit {
    margin-top: 8px;
  }
</style>
