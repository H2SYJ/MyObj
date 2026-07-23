<template>
  <div class="subscriptions-page">
    <el-card shadow="never" class="subscriptions-header-card">
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
    <el-card v-if="!isHandheld" shadow="never">
      <el-table :data="subscriptions" v-loading="loading">
        <el-table-column prop="name" label="订阅" min-width="180" />
        <el-table-column label="插件" min-width="160"
          ><template #default="{ row }"
            >{{ pluginName(row.plugin_id) }} v{{ row.plugin_version }}</template
          ></el-table-column
        >
        <el-table-column prop="schedule_time" label="每日时间" width="100" />
        <el-table-column prop="save_path" label="保存目录" min-width="180" />
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

    <div v-else class="mobile-subscription-list" v-loading="loading">
      <article v-for="subscription in subscriptions" :key="subscription.id" class="mobile-subscription-card">
        <div class="subscription-card-head">
          <div>
            <strong>{{ subscription.name }}</strong>
            <span>{{ pluginName(subscription.plugin_id) }} v{{ subscription.plugin_version }}</span>
          </div>
          <el-switch
            :model-value="subscription.enabled"
            :disabled="subscription.status === 'needs_permission'"
            @change="value => toggle(subscription, !!value)"
          />
        </div>
        <div class="subscription-meta">
          <span><el-icon><Clock /></el-icon>{{ subscription.schedule_time }}</span>
          <span><el-icon><Folder /></el-icon>{{ subscription.save_path }}</span>
        </div>
        <div class="subscription-card-foot">
          <el-tag :type="statusType(subscription.status)" size="small">{{ subscription.status }}</el-tag>
          <div>
            <el-button type="primary" link @click="run(subscription)">立即运行</el-button>
            <el-button link @click="openMobileActions(subscription)">更多</el-button>
          </div>
        </div>
      </article>
      <el-empty v-if="!loading && subscriptions.length === 0" description="暂无订阅" />
    </div>

    <button v-if="isHandheld" type="button" class="subscription-fab" aria-label="新建订阅" @click="openCreate">
      <el-icon><Plus /></el-icon>
    </button>

    <MobileActionSheet
      v-model="mobileActionsVisible"
      title="订阅操作"
      :actions="mobileActions"
      history-key="subscription-actions"
      @select="handleMobileAction"
    />

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑订阅' : '新建订阅'"
      width="640px"
      :fullscreen="isHandheld"
    >
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
          ><el-input v-model="form.save_path" placeholder="/离线下载/订阅"
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

    <el-drawer
      v-model="historyVisible"
      :title="`${historyTarget?.name || ''} · 执行与条目`"
      :size="isHandheld ? '100%' : '75%'"
    >
      <el-tabs v-model="historyTab" @tab-change="loadHistory">
        <el-tab-pane label="下载条目" name="items">
          <el-table v-if="!isHandheld" :data="items" size="small">
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
          <div v-else class="mobile-history-list">
            <article v-for="item in items" :key="item.id" class="history-card">
              <strong>{{ item.title }}</strong>
              <span>{{ item.save_path }}</span>
              <div><el-tag size="small">{{ item.status }}</el-tag><el-tag size="small">{{ item.thumbnail_status }}</el-tag></div>
              <small v-if="item.has_request_headers">请求头：{{ item.request_header_names.join(', ') || '值不可解密' }}</small>
            </article>
          </div>
        </el-tab-pane>
        <el-tab-pane label="执行记录" name="runs">
          <el-table v-if="!isHandheld" :data="runs" size="small"
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
          <div v-else class="mobile-history-list">
            <article v-for="runItem in runs" :key="runItem.id" class="history-card">
              <div class="history-title"><strong>{{ runItem.created_at }}</strong><el-tag size="small">{{ runItem.status }}</el-tag></div>
              <span>{{ runItem.trigger }} · 发现 {{ runItem.items_found }} · 提交 {{ runItem.tasks_created }}</span>
              <small v-if="runItem.error_msg" class="history-error">{{ runItem.error_msg }}</small>
            </article>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
  import { ElMessage, ElMessageBox } from 'element-plus'
  import type { TabsPaneContext } from 'element-plus'
  import { MobileActionSheet, type MobileSheetAction } from '@/components/mobile'
  import { useMobileLayerHistory, useResponsive } from '@/composables'
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
  const { isHandheld } = useResponsive()
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
  const mobileActionsVisible = ref(false)
  const mobileActionTarget = ref<Subscription>()
  const configuredSecrets = ref<string[]>([])
  const form = reactive<SubscriptionPayload>({
    name: '',
    plugin_id: '',
    config: {},
    granted_permissions: [],
    schedule_time: '08:00',
    save_path: '/离线下载/订阅',
    initial_limit: 10,
    max_items_per_run: 100,
    run_now: true
  })
  const selectedPlugin = computed(() => plugins.value.find(plugin => plugin.id === form.plugin_id))
  const mobileActions = computed<MobileSheetAction[]>(() => [
    { key: 'history', label: '执行与条目', icon: 'Document' },
    { key: 'edit', label: '编辑订阅', icon: 'Edit' },
    ...(mobileActionTarget.value?.status === 'needs_permission'
      ? [{ key: 'permissions', label: '确认权限', icon: 'Lock', tone: 'primary' as const }]
      : []),
    { key: 'delete', label: '删除订阅', icon: 'Delete', tone: 'danger' }
  ])
  useMobileLayerHistory(dialogVisible, 'subscription-edit', isHandheld)
  useMobileLayerHistory(historyVisible, 'subscription-history', isHandheld)

  const openMobileActions = (subscription: Subscription) => {
    mobileActionTarget.value = subscription
    mobileActionsVisible.value = true
  }

  const handleMobileAction = async (key: string) => {
    const target = mobileActionTarget.value
    if (!target) return
    if (key === 'history') showHistory(target)
    else if (key === 'edit') openEdit(target)
    else if (key === 'permissions') await confirmPermissions(target)
    else if (key === 'delete') await remove(target)
  }

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
      save_path: '/离线下载/订阅',
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
    if (!form.save_path.trim()) return ElMessage.warning('请填写保存目录')
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
    if (result.code === 200) ElMessage.success('已开始运行')
    else ElMessage.error(result.message)
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

  .mobile-subscription-list,
  .mobile-history-list {
    display: grid;
    gap: 12px;
  }
  .mobile-subscription-card,
  .history-card {
    padding: 16px;
    border: 1px solid var(--border-light);
    border-radius: 18px;
    background: var(--card-bg);
    box-shadow: 0 6px 22px rgba(15, 23, 42, 0.04);
  }
  .subscription-card-head,
  .subscription-card-foot,
  .history-title {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .subscription-card-head > div,
  .history-card {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .subscription-card-head strong,
  .history-card strong {
    color: var(--text-primary);
    font-size: 15px;
  }
  .subscription-card-head span,
  .history-card span,
  .history-card small {
    color: var(--text-secondary);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .subscription-meta {
    margin: 14px 0;
    display: grid;
    gap: 8px;
  }
  .subscription-meta span {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-secondary);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .history-error { color: var(--danger-color) !important; }
  .subscription-fab {
    position: fixed;
    right: 18px;
    bottom: calc(18px + env(safe-area-inset-bottom));
    z-index: 1000;
    width: 54px;
    height: 54px;
    border: 0;
    border-radius: 18px;
    display: grid;
    place-items: center;
    color: white;
    background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
    box-shadow: 0 12px 28px rgba(37, 99, 235, 0.32);
    font-size: 22px;
  }

  @media (max-width: 767px) {
    .subscriptions-page { min-height: 100%; padding: 12px 12px 86px; gap: 12px; }
    .subscriptions-header-card { display: none; }
    :deep(.el-drawer__header) { padding-top: calc(16px + env(safe-area-inset-top)); }
    :deep(.el-drawer__body) { padding-bottom: calc(16px + env(safe-area-inset-bottom)); }
  }
</style>
