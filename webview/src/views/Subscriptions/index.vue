<template>
  <div class="subscriptions-page">
    <el-card shadow="never">
      <div class="header">
        <div>
          <h2>订阅管理</h2>
          <span>每天定时调用已安装插件，并自动提交到离线下载。</span>
        </div>
        <div>
          <el-button type="primary" icon="Plus" @click="openCreate">新建订阅</el-button
          ><el-button icon="Refresh" @click="load">刷新</el-button>
        </div>
      </div>
    </el-card>
    <el-card shadow="never">
      <el-table :data="subscriptions" v-loading="loading">
        <el-table-column prop="name" label="订阅" min-width="180" />
        <el-table-column label="插件" min-width="160"
          ><template #default="{ row }"
            >{{ pluginName(row.plugin_id) }} v{{ row.plugin_version }}</template
          ></el-table-column
        >
        <el-table-column prop="schedule_time" label="每日时间" width="100" />
        <el-table-column prop="default_path" label="保存目录" min-width="180" />
        <el-table-column label="状态" width="150"
          ><template #default="{ row }"
            ><el-tag :type="statusType(row.status)">{{ row.status }}</el-tag></template
          ></el-table-column
        >
        <el-table-column label="启用" width="80"
          ><template #default="{ row }"
            ><el-switch
              :model-value="row.enabled"
              :disabled="row.status === 'needs_permission'"
              @change="value => toggle(row, !!value)" /></template
        ></el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="run(row)">立即运行</el-button>
            <el-button link @click="showHistory(row)">记录</el-button>
            <el-button link @click="openEdit(row)">编辑</el-button>
            <el-button v-if="row.status === 'needs_permission'" link type="warning" @click="confirmPermissions(row)"
              >确认权限</el-button
            >
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑订阅' : '新建订阅'" width="640px">
      <el-form label-width="120px">
        <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="插件">
          <el-select v-model="form.plugin_id" :disabled="!!editingId" style="width: 100%" @change="pluginChanged">
            <el-option
              v-for="plugin in plugins"
              :key="plugin.id"
              :label="`${plugin.name} v${plugin.version}`"
              :value="plugin.id"
            />
          </el-select>
        </el-form-item>
        <template v-for="field in selectedPlugin?.config_fields || []" :key="field.key">
          <el-form-item :label="field.label" :required="field.required">
            <el-switch v-if="field.type === 'boolean'" v-model="form.config[field.key]" />
            <el-input-number v-else-if="field.type === 'number'" v-model="form.config[field.key]" style="width: 100%" />
            <el-select v-else-if="field.type === 'select'" v-model="form.config[field.key]" style="width: 100%">
              <el-option
                v-for="option in field.options"
                :key="String(option.value)"
                :label="option.label"
                :value="option.value"
              />
            </el-select>
            <el-input
              v-else
              v-model="form.config[field.key]"
              :type="field.type"
              :placeholder="
                field.secret && configuredSecrets.includes(field.key) ? '已配置，留空保持不变' : field.description
              "
              :show-password="field.type === 'password'"
            />
          </el-form-item>
        </template>
        <el-form-item label="执行时间"
          ><el-time-picker v-model="form.schedule_time" value-format="HH:mm" format="HH:mm"
        /></el-form-item>
        <el-form-item label="保存目录" required
          ><el-input v-model="form.default_path" placeholder="/离线下载/订阅"
        /></el-form-item>
        <el-form-item label="首次下载"
          ><el-input-number v-model="form.initial_limit" :min="1" :max="100"
        /></el-form-item>
        <el-form-item label="单次上限"
          ><el-input-number v-model="form.max_items_per_run" :min="1" :max="500"
        /></el-form-item>
        <el-form-item label="插件权限">
          <el-checkbox-group v-model="form.granted_permissions">
            <el-checkbox
              v-for="permission in selectedPlugin?.permissions || []"
              :key="permission"
              :value="permission"
              :disabled="permission === 'network.public_http'"
            >
              {{ permissionLabel(permission) }}
            </el-checkbox>
          </el-checkbox-group>
          <el-alert
            v-if="selectedPlugin?.permissions.includes('downloads.custom_headers')"
            title="自定义头会加密保存，只向精确白名单主机发送；界面永不回显头值。"
            type="warning"
            :closable="false"
          />
        </el-form-item>
      </el-form>
      <template #footer
        ><el-button @click="dialogVisible = false">取消</el-button
        ><el-button type="primary" :loading="saving" @click="save">保存</el-button></template
      >
    </el-dialog>

    <el-drawer v-model="historyVisible" :title="`${historyTarget?.name || ''} · 执行与条目`" size="75%">
      <el-tabs v-model="historyTab" @tab-change="loadHistory">
        <el-tab-pane label="下载条目" name="items">
          <el-table :data="items" size="small">
            <el-table-column prop="title" label="标题" min-width="180" />
            <el-table-column prop="save_path" label="保存目录" min-width="160" />
            <el-table-column prop="status" label="提交状态" width="120" />
            <el-table-column label="请求头" min-width="180"
              ><template #default="{ row }"
                ><span v-if="row.has_request_headers"
                  >已配置：{{ row.request_header_names.join(', ') || '值不可解密' }}</span
                ><span v-else>-</span
                ><el-tag v-if="row.requires_headers" type="danger" size="small">等待凭据</el-tag></template
              ></el-table-column
            >
            <el-table-column label="缩略图" min-width="150"
              ><template #default="{ row }"
                ><el-tag size="small">{{ row.thumbnail_status }}</el-tag
                ><el-button v-if="row.thumbnail_status === 'failed'" link @click="retryThumbnail(row)"
                  >重试</el-button
                ></template
              ></el-table-column
            >
          </el-table>
        </el-tab-pane>
        <el-tab-pane label="执行记录" name="runs">
          <el-table :data="runs" size="small"
            ><el-table-column prop="created_at" label="时间" width="180" /><el-table-column
              prop="trigger"
              label="触发"
              width="100" /><el-table-column prop="status" label="状态" width="120" /><el-table-column
              prop="items_found"
              label="发现"
              width="80" /><el-table-column prop="tasks_created" label="提交" width="80" /><el-table-column
              prop="error_msg"
              label="错误"
              min-width="220"
          /></el-table>
        </el-tab-pane>
      </el-tabs>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, ElMessageBox } from 'element-plus'
  import type { TabsPaneContext } from 'element-plus'
  import type { InstalledPlugin } from '@/api/plugin'
  import {
    availablePlugins,
    createSubscription,
    deleteSubscription,
    listSubscriptionItems,
    listSubscriptionRuns,
    listSubscriptions,
    retrySubscriptionThumbnail,
    runSubscription,
    toggleSubscription,
    updateSubscription,
    updateSubscriptionPermissions
  } from '@/api/subscription'
  import type { Subscription, SubscriptionItem, SubscriptionPayload, SubscriptionRun } from '@/api/subscription'

  const plugins = ref<InstalledPlugin[]>([])
  const subscriptions = ref<Subscription[]>([])
  const items = ref<SubscriptionItem[]>([])
  const runs = ref<SubscriptionRun[]>([])
  const loading = ref(false)
  const saving = ref(false)
  const dialogVisible = ref(false)
  const historyVisible = ref(false)
  const historyTab = ref('items')
  const historyTarget = ref<Subscription>()
  const editingId = ref('')
  const configuredSecrets = ref<string[]>([])
  const form = reactive<SubscriptionPayload>({
    name: '',
    plugin_id: '',
    config: {},
    granted_permissions: [],
    schedule_time: '08:00',
    default_path: '/离线下载/订阅',
    initial_limit: 10,
    max_items_per_run: 100,
    run_now: true
  })
  const selectedPlugin = computed(() => plugins.value.find(plugin => plugin.id === form.plugin_id))

  const load = async () => {
    loading.value = true
    try {
      const [pluginResult, subscriptionResult] = await Promise.all([availablePlugins(), listSubscriptions()])
      plugins.value = pluginResult.data || []
      subscriptions.value = subscriptionResult.data?.subscriptions || []
    } finally {
      loading.value = false
    }
  }
  const pluginName = (id: string) => plugins.value.find(plugin => plugin.id === id)?.name || id
  const statusType = (status: string) =>
    status === 'ready' ? 'success' : status === 'needs_permission' ? 'warning' : 'danger'
  const permissionLabel = (value: string) =>
    ({
      'network.public_http': '访问公网 HTTP',
      'files.read_metadata': '查询我的文件元数据',
      'downloads.custom_headers': '提供离线下载自定义头'
    })[value] || value
  const pluginChanged = () => {
    form.config = {}
    form.granted_permissions = selectedPlugin.value?.permissions.filter(value => value === 'network.public_http') || []
    for (const field of selectedPlugin.value?.config_fields || [])
      if (field.default !== undefined) form.config[field.key] = field.default
  }
  const resetForm = () =>
    Object.assign(form, {
      name: '',
      plugin_id: '',
      config: {},
      granted_permissions: [],
      schedule_time: '08:00',
      default_path: '/离线下载/订阅',
      initial_limit: 10,
      max_items_per_run: 100,
      run_now: true
    })
  const openCreate = () => {
    editingId.value = ''
    configuredSecrets.value = []
    resetForm()
    dialogVisible.value = true
  }
  const openEdit = (row: Subscription) => {
    editingId.value = row.id
    configuredSecrets.value = [...(row.secret_fields_configured || [])]
    Object.assign(form, { ...row, config: { ...(row.config || {}) } })
    dialogVisible.value = true
  }
  const save = async () => {
    if (!form.name || !form.plugin_id || !form.schedule_time) return ElMessage.warning('请填写名称、插件和执行时间')
    if (!form.default_path.trim()) return ElMessage.warning('请填写保存目录')
    saving.value = true
    try {
      const result = editingId.value
        ? await updateSubscription({ ...form, id: editingId.value })
        : await createSubscription(form)
      if (result.code !== 200) return ElMessage.error(result.message)
      ElMessage.success('订阅已保存')
      dialogVisible.value = false
      await load()
    } finally {
      saving.value = false
    }
  }
  const toggle = async (row: Subscription, enabled: boolean) => {
    const result = await toggleSubscription(row.id, enabled)
    if (result.code !== 200) ElMessage.error(result.message)
    await load()
  }
  const run = async (row: Subscription) => {
    const result = await runSubscription(row.id)
    result.code === 200 ? ElMessage.success('已开始运行') : ElMessage.error(result.message)
  }
  const remove = async (row: Subscription) => {
    await ElMessageBox.confirm(`确定删除“${row.name}”吗？`, '删除订阅', { type: 'warning' })
    const result = await deleteSubscription(row.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    await load()
  }
  const confirmPermissions = async (row: Subscription) => {
    const plugin = plugins.value.find(value => value.id === row.plugin_id)
    if (!plugin) return
    await ElMessageBox.confirm(
      `插件需要权限：${plugin.permissions.map(permissionLabel).join('、')}`,
      '重新确认插件权限',
      { type: 'warning' }
    )
    const result = await updateSubscriptionPermissions(row.id, plugin.permissions)
    if (result.code !== 200) return ElMessage.error(result.message)
    await load()
  }
  const showHistory = async (row: Subscription) => {
    historyTarget.value = row
    historyTab.value = 'items'
    historyVisible.value = true
    await loadHistory()
  }
  const loadHistory = async (_pane?: TabsPaneContext | string) => {
    if (!historyTarget.value) return
    if (historyTab.value === 'items')
      items.value = (await listSubscriptionItems(historyTarget.value.id)).data?.items || []
    else runs.value = (await listSubscriptionRuns(historyTarget.value.id)).data?.items || []
  }
  const retryThumbnail = async (row: SubscriptionItem) => {
    const result = await retrySubscriptionThumbnail(row.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    await loadHistory()
  }

  onMounted(load)
</script>

<style scoped>
  .subscriptions-page {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 16px;
  }
  .header h2 {
    margin: 0 0 6px;
  }
  .header span {
    color: var(--el-text-color-secondary);
  }
</style>
