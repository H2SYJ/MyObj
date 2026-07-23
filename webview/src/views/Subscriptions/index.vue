<template>
  <div class="subscriptions-page desktop-content-page">
    <el-card shadow="never" class="subscriptions-header-card">
      <div class="header">
        <div>
          <h2>{{ t('subscriptions.title') }}</h2>
          <span>{{ t('subscriptions.description') }}</span>
        </div>
        <div>
          <el-button type="primary" icon="Plus" @click="openCreate">{{ t('subscriptions.create') }}</el-button
          ><el-button icon="Refresh" @click="load">{{ t('common.refresh') }}</el-button>
        </div>
      </div>
    </el-card>
    <el-card v-if="!isHandheld" shadow="never">
      <el-table :data="subscriptions" v-loading="loading">
        <el-table-column prop="name" :label="t('subscriptions.subscription')" min-width="180" />
        <el-table-column :label="t('subscriptions.plugin')" min-width="160"
          ><template #default="{ row }"
            >{{ pluginName(row.plugin_id) }} v{{ row.plugin_version }}</template
          ></el-table-column
        >
        <el-table-column prop="schedule_time" :label="t('subscriptions.dailyTime')" width="100" />
        <el-table-column prop="save_path" :label="t('subscriptions.savePath')" min-width="180" />
        <el-table-column :label="t('subscriptions.status')" width="150"
          ><template #default="{ row }"
            ><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template
          ></el-table-column
        >
        <el-table-column :label="t('subscriptions.enabled')" width="80"
          ><template #default="{ row }"
            ><el-switch
              :model-value="row.enabled"
              :disabled="row.status === 'needs_permission'"
              @change="value => toggle(row, !!value)" /></template
        ></el-table-column>
        <el-table-column :label="t('subscriptions.operation')" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="run(row)">{{ t('subscriptions.runNow') }}</el-button>
            <el-button link @click="showHistory(row)">{{ t('subscriptions.history') }}</el-button>
            <el-button link @click="openEdit(row)">{{ t('common.edit') }}</el-button>
            <el-button v-if="row.status === 'needs_permission'" link type="warning" @click="confirmPermissions(row)">{{
              t('subscriptions.confirmPermissions')
            }}</el-button>
            <el-button link type="danger" @click="remove(row)">{{ t('common.delete') }}</el-button>
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
          <span
            ><el-icon><Clock /></el-icon>{{ subscription.schedule_time }}</span
          >
          <span
            ><el-icon><Folder /></el-icon>{{ subscription.save_path }}</span
          >
        </div>
        <div class="subscription-card-foot">
          <el-tag :type="statusType(subscription.status)" size="small">{{ statusLabel(subscription.status) }}</el-tag>
          <div>
            <el-button type="primary" link @click="run(subscription)">{{ t('subscriptions.runNow') }}</el-button>
            <el-button link @click="openMobileActions(subscription)">{{ t('common.more') }}</el-button>
          </div>
        </div>
      </article>
      <el-empty v-if="!loading && subscriptions.length === 0" :description="t('subscriptions.empty')" />
    </div>

    <button
      v-if="isHandheld"
      type="button"
      class="subscription-fab"
      :aria-label="t('subscriptions.create')"
      @click="openCreate"
    >
      <el-icon><Plus /></el-icon>
    </button>

    <MobileActionSheet
      v-model="mobileActionsVisible"
      :title="t('subscriptions.actionsTitle')"
      :actions="mobileActions"
      history-key="subscription-actions"
      @select="handleMobileAction"
    />

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? t('subscriptions.editTitle') : t('subscriptions.createTitle')"
      width="640px"
      :fullscreen="isHandheld"
    >
      <el-form label-width="120px">
        <el-form-item :label="t('subscriptions.name')"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('subscriptions.plugin')">
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
                field.secret && configuredSecrets.includes(field.key)
                  ? t('subscriptions.secretConfigured')
                  : field.description
              "
              :show-password="field.type === 'password'"
            />
          </el-form-item>
        </template>
        <el-form-item :label="t('subscriptions.executionTime')"
          ><el-time-picker v-model="form.schedule_time" value-format="HH:mm" format="HH:mm"
        /></el-form-item>
        <el-form-item :label="t('subscriptions.savePath')" required
          ><el-input v-model="form.save_path" placeholder="/离线下载/订阅"
        /></el-form-item>
        <el-form-item :label="t('subscriptions.initialLimit')"
          ><el-input-number v-model="form.initial_limit" :min="1" :max="100"
        /></el-form-item>
        <el-form-item :label="t('subscriptions.runLimit')"
          ><el-input-number v-model="form.max_items_per_run" :min="1" :max="500"
        /></el-form-item>
        <el-form-item :label="t('subscriptions.pluginPermissions')">
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
            :title="t('subscriptions.customHeadersWarning')"
            type="warning"
            :closable="false"
          />
        </el-form-item>
      </el-form>
      <template #footer
        ><el-button @click="dialogVisible = false">{{ t('common.cancel') }}</el-button
        ><el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button></template
      >
    </el-dialog>

    <el-drawer
      v-model="historyVisible"
      :title="t('subscriptions.historyTitle', { name: historyTarget?.name || '' })"
      :size="isHandheld ? '100%' : '75%'"
    >
      <el-tabs v-model="historyTab" v-loading="historyLoading" @tab-change="loadHistory">
        <el-tab-pane :label="t('subscriptions.items')" name="items">
          <el-table v-if="!isHandheld" :data="items" size="small">
            <el-table-column prop="title" :label="t('subscriptions.itemTitle')" min-width="180" />
            <el-table-column prop="save_path" :label="t('subscriptions.savePath')" min-width="160" />
            <el-table-column prop="status" :label="t('subscriptions.submissionStatus')" width="120" />
            <el-table-column :label="t('subscriptions.requestHeaders')" min-width="180"
              ><template #default="{ row }"
                ><span v-if="row.has_request_headers">{{
                  t('subscriptions.headersConfigured', {
                    names: row.request_header_names.join(', ') || t('subscriptions.valueUnavailable')
                  })
                }}</span
                ><span v-else>-</span
                ><el-tag v-if="row.requires_headers" type="danger" size="small">{{
                  t('subscriptions.waitingCredentials')
                }}</el-tag></template
              ></el-table-column
            >
            <el-table-column :label="t('subscriptions.thumbnail')" min-width="150"
              ><template #default="{ row }"
                ><el-tag size="small">{{ row.thumbnail_status }}</el-tag
                ><el-button v-if="row.thumbnail_status === 'failed'" link @click="retryThumbnail(row)">{{
                  t('error.retry')
                }}</el-button></template
              ></el-table-column
            >
          </el-table>
          <div v-else class="mobile-history-list">
            <article v-for="item in items" :key="item.id" class="history-card">
              <strong>{{ item.title }}</strong>
              <span>{{ item.save_path }}</span>
              <div>
                <el-tag size="small">{{ item.status }}</el-tag
                ><el-tag size="small">{{ item.thumbnail_status }}</el-tag>
              </div>
              <small v-if="item.has_request_headers">{{
                t('subscriptions.requestHeadersValue', {
                  names: item.request_header_names.join(', ') || t('subscriptions.valueUnavailable')
                })
              }}</small>
            </article>
          </div>
        </el-tab-pane>
        <el-tab-pane :label="t('subscriptions.runs')" name="runs">
          <el-table v-if="!isHandheld" :data="runs" size="small"
            ><el-table-column prop="created_at" :label="t('subscriptions.time')" width="180" /><el-table-column
              prop="trigger"
              :label="t('subscriptions.trigger')"
              width="100" /><el-table-column
              prop="status"
              :label="t('subscriptions.status')"
              width="120" /><el-table-column
              prop="items_found"
              :label="t('subscriptions.found')"
              width="80" /><el-table-column
              prop="tasks_created"
              :label="t('subscriptions.submitted')"
              width="80" /><el-table-column prop="error_msg" :label="t('subscriptions.error')" min-width="220"
          /></el-table>
          <div v-else class="mobile-history-list">
            <article v-for="runItem in runs" :key="runItem.id" class="history-card">
              <div class="history-title">
                <strong>{{ runItem.created_at }}</strong
                ><el-tag size="small">{{ runItem.status }}</el-tag>
              </div>
              <span>{{
                t('subscriptions.runSummary', {
                  trigger: runItem.trigger,
                  found: runItem.items_found,
                  submitted: runItem.tasks_created
                })
              }}</span>
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
  import { useI18n, useMobileLayerHistory, useResponsive } from '@/composables'
  import { useLatestRequest } from '@/composables/core/useLatestRequest'
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
  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const subscriptions = ref<Subscription[]>([])
  const items = ref<SubscriptionItem[]>([])
  const runs = ref<SubscriptionRun[]>([])
  const loading = ref(false)
  const historyLoading = ref(false)
  const saving = ref(false)
  const dialogVisible = ref(false)
  const historyVisible = ref(false)
  const historyTab = ref('items')
  const historyTarget = ref<Subscription>()
  const editingId = ref('')
  const mobileActionsVisible = ref(false)
  const mobileActionTarget = ref<Subscription>()
  const configuredSecrets = ref<string[]>([])
  const listRequest = useLatestRequest()
  const historyRequest = useLatestRequest()
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
    { key: 'history', label: t('subscriptions.historyItems'), icon: 'Document' },
    { key: 'edit', label: t('subscriptions.editTitle'), icon: 'Edit' },
    ...(mobileActionTarget.value?.status === 'needs_permission'
      ? [
          {
            key: 'permissions',
            label: t('subscriptions.confirmPermissions'),
            icon: 'Lock',
            tone: 'primary' as const
          }
        ]
      : []),
    { key: 'delete', label: t('subscriptions.deleteTitle'), icon: 'Delete', tone: 'danger' }
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
    const requestTicket = listRequest.begin()
    loading.value = true
    try {
      const [pluginResult, subscriptionResult] = await Promise.all([
        availablePlugins({ signal: requestTicket.signal }),
        listSubscriptions(1, 100, { signal: requestTicket.signal })
      ])
      if (!requestTicket.isCurrent()) return
      plugins.value = pluginResult.data || []
      subscriptions.value = subscriptionResult.data?.subscriptions || []
    } catch (error: any) {
      if (requestTicket.isCurrent()) ElMessage.error(error.message || t('subscriptions.loadFailed'))
    } finally {
      if (requestTicket.isCurrent()) loading.value = false
    }
  }
  const pluginName = (id: string) => plugins.value.find(plugin => plugin.id === id)?.name || id
  const statusType = (status: string) =>
    status === 'ready' ? 'success' : status === 'needs_permission' ? 'warning' : 'danger'
  const statusLabel = (status: string) => {
    const key = `subscriptions.statuses.${status}`
    const translated = t(key)
    return translated === key ? status : translated
  }
  const permissionLabel = (value: string) =>
    ({
      'network.public_http': t('subscriptions.permissions.publicHttp'),
      'files.read_metadata': t('subscriptions.permissions.fileMetadata'),
      'downloads.custom_headers': t('subscriptions.permissions.customHeaders')
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
    if (!form.name || !form.plugin_id || !form.schedule_time) {
      return ElMessage.warning(t('subscriptions.requiredFields'))
    }
    if (!form.save_path.trim()) return ElMessage.warning(t('subscriptions.savePathRequired'))
    saving.value = true
    try {
      const result = editingId.value
        ? await updateSubscription({ ...form, id: editingId.value })
        : await createSubscription(form)
      if (result.code !== 200) return ElMessage.error(result.message)
      ElMessage.success(t('subscriptions.saveSuccess'))
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
    if (result.code === 200) ElMessage.success(t('subscriptions.runStarted'))
    else ElMessage.error(result.message)
  }
  const remove = async (row: Subscription) => {
    await ElMessageBox.confirm(t('subscriptions.deleteConfirm', { name: row.name }), t('subscriptions.deleteTitle'), {
      type: 'warning'
    })
    const result = await deleteSubscription(row.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    await load()
  }
  const confirmPermissions = async (row: Subscription) => {
    const plugin = plugins.value.find(value => value.id === row.plugin_id)
    if (!plugin) return
    await ElMessageBox.confirm(
      t('subscriptions.permissionsConfirm', { permissions: plugin.permissions.map(permissionLabel).join(', ') }),
      t('subscriptions.permissionsTitle'),
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
    const requestTicket = historyRequest.begin()
    const subscriptionId = historyTarget.value.id
    const tab = historyTab.value
    historyLoading.value = true
    try {
      if (tab === 'items') {
        const result = await listSubscriptionItems(subscriptionId, 1, 50, { signal: requestTicket.signal })
        if (requestTicket.isCurrent()) items.value = result.data?.items || []
      } else {
        const result = await listSubscriptionRuns(subscriptionId, 1, 50, { signal: requestTicket.signal })
        if (requestTicket.isCurrent()) runs.value = result.data?.items || []
      }
    } catch (error: any) {
      if (requestTicket.isCurrent()) ElMessage.error(error.message || t('subscriptions.historyLoadFailed'))
    } finally {
      if (requestTicket.isCurrent()) historyLoading.value = false
    }
  }
  const retryThumbnail = async (row: SubscriptionItem) => {
    const result = await retrySubscriptionThumbnail(row.id)
    if (result.code !== 200) return ElMessage.error(result.message)
    await loadHistory()
  }

  onMounted(load)
  watch(historyVisible, visible => {
    if (!visible) historyRequest.cancel()
  })
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
  .history-error {
    color: var(--danger-color) !important;
  }
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
    .subscriptions-page {
      min-height: 100%;
      padding: 12px 12px 86px;
      gap: 12px;
    }
    .subscriptions-header-card {
      display: none;
    }
    :deep(.el-drawer__header) {
      padding-top: calc(16px + env(safe-area-inset-top));
    }
    :deep(.el-drawer__body) {
      padding-bottom: calc(16px + env(safe-area-inset-bottom));
    }
  }
</style>
