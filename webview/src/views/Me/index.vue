<template>
  <MobilePage v-if="isHandheld">
    <section class="mobile-profile-card">
      <div class="avatar">{{ avatarText }}</div>
      <div class="profile-copy">
        <strong>{{ displayName }}</strong
        ><span>{{ accountDetail }}</span>
      </div>
      <router-link to="/settings/profile" :aria-label="t('me.editProfile')"
        ><el-icon><ArrowRight /></el-icon
      ></router-link>
    </section>

    <section class="mobile-storage-card">
      <div class="storage-row">
        <div>
          <span>{{ t('me.storageTitle') }}</span
          ><strong>{{ storageDescription }}</strong>
        </div>
        <span>{{ storagePercent }}</span>
      </div>
      <el-progress :percentage="progressValue" :show-text="false" :stroke-width="8" :color="storageColor" />
    </section>

    <section v-for="group in menuGroups" :key="group.title" class="mobile-menu-group">
      <h2>{{ group.title }}</h2>
      <router-link v-for="item in group.items" :key="item.path" :to="item.path" class="mobile-menu-item">
        <span class="menu-icon"
          ><el-icon><component :is="item.icon" /></el-icon></span
        ><span>{{ item.label }}</span
        ><el-icon><ArrowRight /></el-icon>
      </router-link>
    </section>
    <button type="button" class="mobile-logout" @click="logout">
      <el-icon><SwitchButton /></el-icon>{{ t('me.logout') }}
    </button>
  </MobilePage>

  <DesktopPage v-else :title="t('me.title')" :description="t('me.description')">
    <div class="account-overview">
      <section class="account-hero desktop-card">
        <div class="account-avatar">{{ avatarText }}</div>
        <div class="account-copy">
          <span>{{ t('me.accountLabel') }}</span>
          <h2>{{ displayName }}</h2>
          <p>{{ accountDetail }}</p>
        </div>
        <div class="account-actions">
          <el-button icon="User" @click="router.push('/settings/profile')">{{ t('me.editProfile') }}</el-button>
          <el-button icon="Lock" @click="router.push('/settings/password')">{{ t('me.changePassword') }}</el-button>
        </div>
      </section>

      <section class="storage-overview desktop-card">
        <div class="section-heading">
          <div>
            <span>{{ t('me.storageTitle') }}</span
            ><strong>{{ storageDescription }}</strong>
          </div>
          <b>{{ storagePercent }}</b>
        </div>
        <el-progress :percentage="progressValue" :show-text="false" :stroke-width="10" :color="storageColor" />
        <p>{{ t('me.usedStorage', { used: formatStorage(storageInfo.used) }) }}</p>
      </section>

      <section class="quick-access">
        <h2>{{ t('me.quickAccess') }}</h2>
        <div class="quick-access__grid">
          <router-link v-for="item in desktopLinks" :key="item.path" :to="item.path" class="desktop-card">
            <span
              ><el-icon><component :is="item.icon" /></el-icon
            ></span>
            <div>
              <strong>{{ item.label }}</strong
              ><small>{{ item.description }}</small>
            </div>
            <el-icon><ArrowRight /></el-icon>
          </router-link>
        </div>
      </section>
    </div>
  </DesktopPage>
</template>

<script setup lang="ts">
  import { DesktopPage } from '@/components/desktop'
  import { MobilePage } from '@/components/mobile'
  import { useAdmin, useI18n, useResponsive } from '@/composables'
  import { useAuthStore, useUserStore } from '@/stores'

  const router = useRouter()
  const authStore = useAuthStore()
  const userStore = useUserStore()
  const { isAdmin } = useAdmin()
  const { isHandheld } = useResponsive()
  const { t } = useI18n()
  const storageInfo = computed(() => userStore.storageInfo)
  const displayName = computed(() => userStore.nickname || userStore.username || 'MyObj')
  const avatarText = computed(() => displayName.value.charAt(0).toUpperCase())
  const accountDetail = computed(() => userStore.email || userStore.phone || t('me.accountLabel'))
  const progressValue = computed(() => (storageInfo.value.isUnlimited ? 100 : storageInfo.value.percentage))
  const storagePercent = computed(() => (storageInfo.value.isUnlimited ? '∞' : `${storageInfo.value.percentage}%`))

  const formatStorage = (bytes: number) => {
    if (!bytes) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    return `${(bytes / 1024 ** index).toFixed(index > 1 ? 1 : 0)} ${units[index]}`
  }
  const storageDescription = computed(() =>
    storageInfo.value.isUnlimited
      ? `${formatStorage(storageInfo.value.used)} / ${t('me.unlimitedStorage')}`
      : `${formatStorage(storageInfo.value.used)} / ${formatStorage(storageInfo.value.total)}`
  )
  const storageColor = computed(() =>
    storageInfo.value.percentage >= 90
      ? 'var(--danger-color)'
      : storageInfo.value.percentage >= 70
        ? 'var(--warning-color)'
        : 'var(--primary-color)'
  )

  const baseLinks = computed(() => [
    { path: '/tags', label: t('menu.tags'), description: t('tagCloud.description'), icon: 'CollectionTag' },
    { path: '/shares', label: t('menu.shares'), description: t('me.contentAutomation'), icon: 'Share' },
    { path: '/subscriptions', label: t('menu.subscriptions'), description: t('me.contentAutomation'), icon: 'Clock' },
    { path: '/trash', label: t('menu.trash'), description: t('me.contentAutomation'), icon: 'Delete' },
    { path: '/settings', label: t('menu.settings'), description: t('me.accountSystem'), icon: 'Setting' }
  ])
  const desktopLinks = computed(() =>
    isAdmin.value
      ? [
          ...baseLinks.value,
          { path: '/admin', label: t('me.adminCenter'), description: t('me.accountSystem'), icon: 'Tools' }
        ]
      : baseLinks.value
  )
  const menuGroups = computed(() => [
    { title: t('me.contentAutomation'), items: baseLinks.value.slice(0, 4) },
    { title: t('me.accountSystem'), items: desktopLinks.value.slice(4) }
  ])

  const logout = async () => {
    authStore.logout()
    await router.replace('/login')
  }
</script>

<style scoped>
  .account-overview {
    display: grid;
    grid-template-columns: minmax(0, 1.35fr) minmax(300px, 0.65fr);
    gap: 18px;
  }
  .account-hero {
    min-height: 190px;
    padding: 28px;
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr);
    align-items: center;
    gap: 20px;
  }
  .account-avatar,
  .avatar {
    display: grid;
    place-items: center;
    color: #fff;
    font-weight: 800;
    background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
  }
  .account-avatar {
    width: 76px;
    height: 76px;
    border-radius: 24px;
    font-size: 30px;
  }
  .account-copy {
    min-width: 0;
  }
  .account-copy span {
    color: var(--primary-color);
    font-size: 11px;
    font-weight: 750;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .account-copy h2 {
    margin: 5px 0 4px;
    font-size: 24px;
  }
  .account-copy p {
    margin: 0;
    color: var(--text-secondary);
    font-size: 13px;
  }
  .account-actions {
    grid-column: 1 / -1;
    display: flex;
    gap: 8px;
  }
  .storage-overview {
    padding: 24px;
  }
  .section-heading {
    margin-bottom: 22px;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .section-heading div {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .section-heading span {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .section-heading strong {
    font-size: 15px;
  }
  .section-heading b {
    color: var(--primary-color);
    font-size: 24px;
  }
  .storage-overview p {
    margin: 14px 0 0;
    color: var(--text-secondary);
    font-size: 11px;
  }
  .quick-access {
    grid-column: 1 / -1;
  }
  .quick-access > h2 {
    margin: 12px 0;
    font-size: 16px;
  }
  .quick-access__grid {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 12px;
  }
  .quick-access__grid a {
    min-height: 92px;
    padding: 16px;
    display: grid;
    grid-template-columns: 42px minmax(0, 1fr) 16px;
    align-items: center;
    gap: 11px;
    color: var(--text-primary);
    text-decoration: none;
  }
  .quick-access__grid a > span {
    width: 42px;
    height: 42px;
    display: grid;
    place-items: center;
    border-radius: 13px;
    color: var(--primary-color);
    background: var(--desktop-primary-soft);
  }
  .quick-access__grid a > div {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .quick-access__grid strong {
    font-size: 13px;
  }
  .quick-access__grid small {
    color: var(--text-secondary);
    font-size: 10px;
  }
  @media (max-width: 1100px) {
    .quick-access__grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  .mobile-profile-card,
  .mobile-storage-card,
  .mobile-menu-group {
    border: 1px solid var(--border-light);
    background: var(--card-bg);
    border-radius: 20px;
    box-shadow: 0 8px 28px rgba(15, 23, 42, 0.04);
  }
  .mobile-profile-card {
    min-height: 92px;
    padding: 16px;
    display: grid;
    grid-template-columns: 56px 1fr 44px;
    align-items: center;
    gap: 12px;
  }
  .avatar {
    width: 56px;
    height: 56px;
    border-radius: 18px;
    font-size: 22px;
  }
  .profile-copy {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .profile-copy span {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .mobile-storage-card {
    margin-top: 12px;
    padding: 16px;
  }
  .storage-row {
    margin-bottom: 12px;
    display: flex;
    justify-content: space-between;
    gap: 12px;
  }
  .storage-row > div {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .storage-row span {
    color: var(--text-secondary);
    font-size: 12px;
  }
  .mobile-menu-group {
    margin-top: 12px;
    overflow: hidden;
  }
  .mobile-menu-group h2 {
    margin: 0;
    padding: 13px 14px 7px;
    color: var(--text-secondary);
    font-size: 11px;
  }
  .mobile-menu-item {
    min-height: 58px;
    padding: 8px 14px;
    display: grid;
    grid-template-columns: 38px 1fr 20px;
    align-items: center;
    gap: 10px;
    border-top: 1px solid var(--border-light);
    color: var(--text-primary);
    text-decoration: none;
  }
  .menu-icon {
    width: 36px;
    height: 36px;
    display: grid;
    place-items: center;
    border-radius: 12px;
    color: var(--primary-color);
    background: color-mix(in srgb, var(--primary-color) 10%, transparent);
  }
  .mobile-logout {
    width: 100%;
    min-height: 48px;
    margin-top: 16px;
    border: 0;
    border-radius: 16px;
    background: color-mix(in srgb, var(--danger-color) 10%, transparent);
    color: var(--danger-color);
  }
</style>
