<template>
  <MobilePage>
    <section class="profile-card">
      <div class="avatar">{{ avatarText }}</div>
      <div class="profile-copy">
        <strong>{{ userStore.nickname || userStore.username }}</strong>
        <span>{{ userStore.email || userStore.phone || 'MyObj 用户' }}</span>
      </div>
      <router-link to="/settings/profile" aria-label="编辑个人信息"><el-icon><ArrowRight /></el-icon></router-link>
    </section>

    <section class="storage-card">
      <div class="storage-row">
        <div>
          <span>存储空间</span>
          <strong>{{ storageDescription }}</strong>
        </div>
        <span class="storage-percent">{{ storageInfo.isUnlimited ? '∞' : `${storageInfo.percentage}%` }}</span>
      </div>
      <el-progress
        :percentage="storageInfo.isUnlimited ? 100 : storageInfo.percentage"
        :show-text="false"
        :stroke-width="8"
        :color="storageColor"
      />
    </section>

    <section v-for="group in menuGroups" :key="group.title" class="menu-group">
      <h2>{{ group.title }}</h2>
      <router-link v-for="item in group.items" :key="item.path" :to="item.path" class="menu-item">
        <span class="menu-icon" :class="item.tone"><el-icon><component :is="item.icon" /></el-icon></span>
        <span>{{ item.label }}</span>
        <el-icon class="chevron"><ArrowRight /></el-icon>
      </router-link>
    </section>

    <button type="button" class="logout-button" @click="logout">
      <el-icon><SwitchButton /></el-icon>退出登录
    </button>
  </MobilePage>
</template>

<script setup lang="ts">
  import { MobilePage } from '@/components/mobile'
  import { useAdmin } from '@/composables'
  import { useAuthStore, useUserStore } from '@/stores'

  const router = useRouter()
  const authStore = useAuthStore()
  const userStore = useUserStore()
  const { isAdmin } = useAdmin()
  const storageInfo = computed(() => userStore.storageInfo)
  const avatarText = computed(() => (userStore.nickname || userStore.username || 'U').charAt(0).toUpperCase())

  const formatStorage = (bytes: number) => {
    if (!bytes) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
    return `${(bytes / 1024 ** index).toFixed(index > 1 ? 1 : 0)} ${units[index]}`
  }
  const storageDescription = computed(() =>
    storageInfo.value.isUnlimited
      ? `${formatStorage(storageInfo.value.used)} / 无限容量`
      : `${formatStorage(storageInfo.value.used)} / ${formatStorage(storageInfo.value.total)}`
  )
  const storageColor = computed(() => {
    if (storageInfo.value.isUnlimited || storageInfo.value.percentage < 70) return 'var(--primary-color)'
    if (storageInfo.value.percentage < 90) return 'var(--warning-color)'
    return 'var(--danger-color)'
  })

  const menuGroups = computed(() => [
    {
      title: '内容与自动化',
      items: [
        { path: '/shares', label: '我的分享', icon: 'Share', tone: 'blue' },
        { path: '/subscriptions', label: '订阅管理', icon: 'Clock', tone: 'purple' },
        { path: '/trash', label: '回收站', icon: 'Delete', tone: 'orange' }
      ]
    },
    {
      title: '账户与系统',
      items: [
        { path: '/settings', label: '设置', icon: 'Setting', tone: 'gray' },
        ...(isAdmin.value ? [{ path: '/admin', label: '管理中心', icon: 'Tools', tone: 'red' }] : [])
      ]
    }
  ])

  const logout = async () => {
    authStore.logout()
    await router.replace('/login')
  }
</script>

<style scoped>
  .profile-card,
  .storage-card,
  .menu-group {
    border: 1px solid var(--border-light);
    background: var(--card-bg);
    border-radius: 20px;
    box-shadow: 0 8px 28px rgba(15, 23, 42, 0.04);
  }

  .profile-card {
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
    display: grid;
    place-items: center;
    color: white;
    font-size: 22px;
    font-weight: 800;
    background: linear-gradient(135deg, var(--primary-color), var(--secondary-color));
  }

  .profile-copy { min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .profile-copy strong { font-size: 17px; overflow: hidden; text-overflow: ellipsis; }
  .profile-copy span { color: var(--text-secondary); font-size: 13px; overflow: hidden; text-overflow: ellipsis; }
  .profile-card > a { width: 44px; height: 44px; display: grid; place-items: center; color: var(--text-secondary); }

  .storage-card { margin-top: 12px; padding: 16px; }
  .storage-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .storage-row > div { display: flex; flex-direction: column; gap: 5px; }
  .storage-row span { color: var(--text-secondary); font-size: 12px; }
  .storage-row strong { font-size: 14px; }
  .storage-percent { color: var(--primary-color) !important; font-size: 18px !important; font-weight: 800; }

  .menu-group { margin-top: 16px; overflow: hidden; }
  .menu-group h2 { margin: 0; padding: 15px 16px 8px; color: var(--text-secondary); font-size: 12px; font-weight: 700; }
  .menu-item {
    min-height: 58px;
    padding: 0 12px 0 16px;
    display: grid;
    grid-template-columns: 36px 1fr 28px;
    align-items: center;
    gap: 12px;
    color: var(--text-primary);
    text-decoration: none;
    border-top: 1px solid var(--border-light);
  }
  .menu-item:active { background: var(--border-light); }
  .menu-icon { width: 34px; height: 34px; display: grid; place-items: center; border-radius: 11px; }
  .blue { color: #2563eb; background: rgba(37, 99, 235, 0.1); }
  .purple { color: #7c3aed; background: rgba(124, 58, 237, 0.1); }
  .orange { color: #ea580c; background: rgba(234, 88, 12, 0.1); }
  .gray { color: #475569; background: rgba(71, 85, 105, 0.1); }
  .red { color: #dc2626; background: rgba(220, 38, 38, 0.1); }
  .chevron { color: var(--text-placeholder); }

  .logout-button {
    width: 100%;
    min-height: 50px;
    margin: 18px 0 8px;
    border: 0;
    border-radius: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--danger-color);
    background: color-mix(in srgb, var(--danger-color) 8%, var(--card-bg));
    font-size: 15px;
    font-weight: 650;
  }
</style>
