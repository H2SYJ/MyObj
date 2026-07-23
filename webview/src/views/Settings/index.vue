<template>
  <MobilePage v-if="isHandheld" class="mobile-settings-page">
    <div v-if="!section" class="mobile-settings-menu">
      <router-link v-for="item in settingItems" :key="item.path" :to="item.path" class="settings-menu-item">
        <span class="settings-icon"><el-icon><component :is="item.icon" /></el-icon></span>
        <span><strong>{{ item.title }}</strong><small>{{ item.description }}</small></span>
        <el-icon><ArrowRight /></el-icon>
      </router-link>
    </div>
    <component :is="sectionComponent" v-else />
  </MobilePage>

  <div v-else class="settings-page">
    <el-card shadow="never" class="settings-card">
      <template #header>
        <div class="card-header">
          <h2>{{ t('settings.title') }}</h2>
        </div>
      </template>

      <el-tabs v-model="activeTab" class="settings-tabs">
        <el-tab-pane :label="t('settings.userInfo.title')" name="userInfo">
          <UserInfo />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.password.title')" name="password">
          <Password />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.appearance')" name="appearance">
          <Appearance />
        </el-tab-pane>

        <el-tab-pane :label="t('settings.apiKey.title')" name="apiKey">
          <ApiKey />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup lang="ts">
  import UserInfo from './components/UserInfo.vue'
  import Password from './components/Password.vue'
  import Appearance from './components/Appearance.vue'
  import ApiKey from './components/ApiKey.vue'
  import { MobilePage } from '@/components/mobile'
  import { useI18n, useResponsive } from '@/composables'

  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const route = useRoute()
  const activeTab = ref('userInfo')
  const section = computed(() => route.meta.settingSection)
  const sectionComponent = computed(() => {
    const components = { profile: UserInfo, password: Password, appearance: Appearance, 'api-key': ApiKey }
    return section.value ? components[section.value] : undefined
  })
  const settingItems = [
    { path: '/settings/profile', title: '个人信息', description: '头像、昵称与联系方式', icon: 'User' },
    { path: '/settings/password', title: '修改密码', description: '更新账户登录密码', icon: 'Lock' },
    { path: '/settings/appearance', title: '外观设置', description: '主题、颜色与显示偏好', icon: 'Brush' },
    { path: '/settings/api-key', title: 'API Key', description: '管理接口访问凭据', icon: 'Key' }
  ]

  watch(
    section,
    value => {
      const tabMap = { profile: 'userInfo', password: 'password', appearance: 'appearance', 'api-key': 'apiKey' }
      if (value) activeTab.value = tabMap[value]
    },
    { immediate: true }
  )
</script>

<style scoped>
  .settings-page {
    height: 100%;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 4px;
  }

  .settings-card {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .settings-card :deep(.el-card__body) {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    padding: 0;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .card-header h2 {
    margin: 0;
    font-size: 20px;
    font-weight: 600;
    color: var(--el-text-color-primary);
  }

  .settings-tabs {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    padding: 16px;
  }

  .settings-tabs :deep(.el-tabs__content) {
    flex: 1;
    overflow: auto;
  }

  .settings-tabs :deep(.el-tab-pane) {
    height: 100%;
  }

  @media (max-width: 1024px) {
    .settings-tabs {
      padding: 12px;
    }
  }

  .mobile-settings-page { padding-top: 12px; }
  .mobile-settings-menu {
    overflow: hidden;
    border: 1px solid var(--border-light);
    border-radius: 20px;
    background: var(--card-bg);
  }
  .settings-menu-item {
    min-height: 72px;
    padding: 10px 14px;
    display: grid;
    grid-template-columns: 40px 1fr 24px;
    align-items: center;
    gap: 12px;
    color: var(--text-primary);
    text-decoration: none;
    border-top: 1px solid var(--border-light);
  }
  .settings-menu-item:first-child { border-top: 0; }
  .settings-menu-item:active { background: var(--border-light); }
  .settings-icon {
    width: 40px;
    height: 40px;
    display: grid;
    place-items: center;
    border-radius: 13px;
    color: var(--primary-color);
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
  }
  .settings-menu-item > span:nth-child(2) { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .settings-menu-item strong { font-size: 15px; }
  .settings-menu-item small { color: var(--text-secondary); font-size: 12px; }

  /* 深色模式样式 */
  html.dark .settings-card {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .settings-card :deep(.el-card__header) {
    background: var(--card-bg);
    border-bottom-color: var(--el-border-color);
  }

  html.dark .settings-card :deep(.el-card__body) {
    background: var(--card-bg);
  }

  html.dark .card-header h2 {
    color: var(--el-text-color-primary);
  }

  html.dark .settings-tabs :deep(.el-tabs__header) {
    background: var(--card-bg);
    border-color: var(--el-border-color);
  }

  html.dark .settings-tabs :deep(.el-tabs__item) {
    color: var(--el-text-color-primary);
    border-color: var(--el-border-color);
  }

  html.dark .settings-tabs :deep(.el-tabs__item.is-active) {
    color: var(--primary-color);
    border-bottom-color: var(--primary-color);
  }

  html.dark .settings-tabs :deep(.el-tabs__nav-wrap::after) {
    background-color: var(--el-border-color);
  }
</style>
