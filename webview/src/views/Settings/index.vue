<template>
  <MobilePage v-if="isHandheld" class="mobile-settings-page">
    <div v-if="!section" class="mobile-settings-menu">
      <router-link v-for="item in settingItems" :key="item.path" :to="item.path" class="settings-menu-item">
        <span class="settings-icon"
          ><el-icon><component :is="item.icon" /></el-icon
        ></span>
        <span
          ><strong>{{ item.title }}</strong
          ><small>{{ item.description }}</small></span
        >
        <el-icon><ArrowRight /></el-icon>
      </router-link>
    </div>
    <component :is="sectionComponent" v-else />
  </MobilePage>

  <DesktopPage v-else :title="t('settings.title')" :description="t('settings.desktopDescription')" full-height>
    <div class="desktop-settings">
      <nav class="desktop-settings__nav" :aria-label="t('settings.title')">
        <router-link v-for="item in settingItems" :key="item.path" :to="item.path">
          <el-icon><component :is="item.icon" /></el-icon>
          <span
            ><strong>{{ item.title }}</strong
            ><small>{{ item.description }}</small></span
          >
          <el-icon class="desktop-settings__arrow"><ArrowRight /></el-icon>
        </router-link>
      </nav>
      <section class="desktop-settings__content"><component :is="sectionComponent || UserInfo" /></section>
    </div>
  </DesktopPage>
</template>

<script setup lang="ts">
  import UserInfo from './components/UserInfo.vue'
  import Password from './components/Password.vue'
  import Appearance from './components/Appearance.vue'
  import ApiKey from './components/ApiKey.vue'
  import { DesktopPage } from '@/components/desktop'
  import { MobilePage } from '@/components/mobile'
  import { useI18n, useResponsive } from '@/composables'

  const { t } = useI18n()
  const { isHandheld } = useResponsive()
  const route = useRoute()
  const section = computed(() => route.meta.settingSection)
  const components = { profile: UserInfo, password: Password, appearance: Appearance, 'api-key': ApiKey }
  const sectionComponent = computed(() => (section.value ? components[section.value] : undefined))
  const settingItems = computed(() => [
    {
      path: '/settings/profile',
      title: t('settings.userInfo.title'),
      description: t('settings.profileDescription'),
      icon: 'User'
    },
    {
      path: '/settings/password',
      title: t('settings.password.title'),
      description: t('settings.passwordDescription'),
      icon: 'Lock'
    },
    {
      path: '/settings/appearance',
      title: t('settings.appearance'),
      description: t('settings.appearanceDescription'),
      icon: 'Brush'
    },
    {
      path: '/settings/api-key',
      title: t('settings.apiKey.title'),
      description: t('settings.apiKeyDescription'),
      icon: 'Key'
    }
  ])
</script>

<style scoped>
  .desktop-settings {
    min-height: 620px;
    display: grid;
    grid-template-columns: 248px minmax(0, 1fr);
    gap: 18px;
  }
  .desktop-settings__nav,
  .desktop-settings__content {
    border: 1px solid var(--desktop-border);
    border-radius: var(--desktop-radius-lg);
    background: var(--desktop-surface);
    box-shadow: var(--desktop-shadow-sm);
  }
  .desktop-settings__nav {
    align-self: start;
    padding: 8px;
    display: grid;
    gap: 3px;
  }
  .desktop-settings__nav a {
    min-height: 64px;
    padding: 10px 11px;
    display: grid;
    grid-template-columns: 24px minmax(0, 1fr) 16px;
    align-items: center;
    gap: 10px;
    border-radius: 11px;
    color: var(--text-regular);
    text-decoration: none;
  }
  .desktop-settings__nav a:hover {
    background: var(--desktop-fill);
  }
  .desktop-settings__nav a.router-link-active {
    background: var(--desktop-primary-soft);
    color: var(--primary-color);
  }
  .desktop-settings__nav a > span {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .desktop-settings__nav strong {
    font-size: 13px;
  }
  .desktop-settings__nav small {
    overflow: hidden;
    color: var(--text-secondary);
    font-size: 10px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .desktop-settings__arrow {
    color: var(--text-placeholder);
    font-size: 12px;
  }
  .desktop-settings__content {
    min-width: 0;
    padding: 24px;
  }
  @media (max-width: 991px) {
    .desktop-settings {
      grid-template-columns: 200px minmax(0, 1fr);
    }
    .desktop-settings__content {
      padding: 18px;
    }
  }

  .mobile-settings-page {
    padding-top: 12px;
  }
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
  .settings-menu-item:first-child {
    border-top: 0;
  }
  .settings-menu-item:active {
    background: var(--border-light);
  }
  .settings-icon {
    width: 40px;
    height: 40px;
    display: grid;
    place-items: center;
    border-radius: 13px;
    color: var(--primary-color);
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
  }
  .settings-menu-item > span:nth-child(2) {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .settings-menu-item strong {
    font-size: 15px;
  }
  .settings-menu-item small {
    color: var(--text-secondary);
    font-size: 12px;
  }
</style>
