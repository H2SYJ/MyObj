<template>
  <div class="plugin-center">
    <el-alert
      title="插件在 WASM 沙箱中运行，但未签名插件仍需管理员确认来源和权限后信任安装。"
      type="warning"
      show-icon
      :closable="false"
    />
    <div class="toolbar">
      <input ref="fileInput" type="file" accept=".myobj-plugin" hidden @change="handleFile" />
      <el-button type="primary" icon="Upload" :loading="installing" @click="fileInput?.click()">安装插件</el-button>
      <el-button icon="Refresh" @click="load">刷新</el-button>
    </div>
    <el-table v-if="!isHandheld" :data="plugins" v-loading="loading">
      <el-table-column prop="name" label="插件" min-width="180">
        <template #default="{ row }">
          <div>
            {{ row.name }} <el-tag size="small">v{{ row.version }}</el-tag>
          </div>
          <small>{{ row.id }}</small>
        </template>
      </el-table-column>
      <el-table-column label="权限" min-width="260">
        <template #default="{ row }">
          <el-tag v-for="permission in row.permissions" :key="permission" size="small" class="permission">
            {{ permission }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="信任" width="150">
        <template #default><el-tag type="warning">未签名·管理员信任</el-tag></template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag v-if="row.status === 'incompatible_api'" type="danger">ABI 不兼容</el-tag>
          <el-switch
            v-else
            :model-value="row.enabled"
            @change="value => changeState(row, !!value)"
          />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }"><el-button link type="danger" @click="remove(row)">卸载</el-button></template>
      </el-table-column>
    </el-table>
    <div v-else v-loading="loading" class="mobile-admin-list">
      <article v-for="row in plugins" :key="row.id" class="mobile-admin-card">
        <div class="mobile-admin-card__header">
          <div class="mobile-admin-card__title">{{ row.name }} <el-tag size="small">v{{ row.version }}</el-tag></div>
          <el-tag v-if="row.status === 'incompatible_api'" type="danger">ABI 不兼容</el-tag>
          <el-switch v-else :model-value="row.enabled" @change="value => changeState(row, !!value)" />
        </div>
        <div class="mobile-admin-card__subtitle">{{ row.id }}</div>
        <div class="mobile-admin-card__meta">
          <el-tag v-for="permission in row.permissions" :key="permission" size="small">{{ permission }}</el-tag>
        </div>
        <div class="mobile-admin-card__footer"><el-button link type="danger" @click="remove(row)">卸载</el-button></div>
      </article>
      <el-empty v-if="!loading && plugins.length === 0" description="暂无插件" />
    </div>
    <el-collapse class="audit">
      <el-collapse-item title="插件审计记录" name="audit">
        <el-table v-if="!isHandheld" :data="auditRows" size="small">
          <el-table-column prop="created_at" label="时间" width="180" />
          <el-table-column prop="plugin_id" label="插件" min-width="160" />
          <el-table-column prop="action" label="操作" width="120" />
          <el-table-column prop="status" label="结果" width="100" />
          <el-table-column prop="summary" label="摘要" min-width="240" />
        </el-table>
        <div v-else class="mobile-admin-list">
          <article v-for="row in auditRows" :key="`${row.created_at}-${row.plugin_id}-${row.action}`" class="mobile-admin-card">
            <div class="mobile-admin-card__header"><div class="mobile-admin-card__title">{{ row.plugin_id }}</div><el-tag size="small">{{ row.status }}</el-tag></div>
            <div class="mobile-admin-card__subtitle">{{ row.summary }}</div>
            <div class="mobile-admin-card__meta"><span>{{ row.action }}</span><span>{{ row.created_at }}</span></div>
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
  import { useResponsive } from '@/composables'

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
      ElMessage.error(error instanceof Error ? error.message : '校验插件失败')
      return
    } finally {
      installing.value = false
    }
    if (inspection.code !== 200 || !inspection.data) return ElMessage.error(inspection.message)
    const manifest = inspection.data.manifest
    await ElMessageBox.confirm(
      `插件：${manifest.name} v${manifest.version}\n来源：${manifest.author || '未知'}\n权限：${manifest.permissions.join('、') || '无'}\n包 SHA-256：${inspection.data.package_sha256}\n\n该插件未签名，确认信任并安装吗？`,
      '信任安装未签名插件',
      { type: 'warning', confirmButtonText: '信任并安装' }
    )
    installing.value = true
    try {
      const result = await installPlugin(file, manifest.permissions)
      if (result.code !== 200) throw new Error(result.message)
      ElMessage.success('插件安装成功')
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
    await ElMessageBox.confirm(`确定卸载插件“${plugin.name}”吗？仍被订阅使用时后端会拒绝。`, '卸载插件', {
      type: 'warning'
    })
    const result = await uninstallPlugin(plugin.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    ElMessage.success('插件已卸载')
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
