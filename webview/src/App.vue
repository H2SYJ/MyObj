<script setup lang="ts">
  // App只作为路由容器，逻辑由router守卫和各页面处理
  import { useTheme } from '@/composables'
  import { useAppStore, useAuthStore } from '@/stores'
  import { taskEventClient } from '@/utils/taskEvents'

  // 初始化主题系统
  useTheme()

  // 获取 Element Plus 语言包
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const realtimeState = taskEventClient.connectionState
  const realtimeDisconnected = computed(() => Boolean(authStore.token) && realtimeState.value === 'disconnected')

  watch(
    () => authStore.token,
    token => {
      if (token) taskEventClient.start()
      else taskEventClient.stop()
    },
    { immediate: true }
  )

  onBeforeUnmount(() => taskEventClient.stop())
</script>

<template>
  <ElConfigProvider :locale="appStore.elementPlusLocale">
    <div v-if="realtimeDisconnected" class="realtime-disconnected" role="alert">
      <span>任务实时状态连接已断开</span>
      <el-button link type="primary" @click="taskEventClient.reconnect()">立即重连并刷新</el-button>
    </div>
    <router-view />
  </ElConfigProvider>
</template>

<style scoped>
  /* 全局样式在style.css中定义 */

  .realtime-disconnected {
    position: fixed;
    z-index: 3000;
    top: 12px;
    left: 50%;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    border: 1px solid var(--el-color-warning-light-5);
    border-radius: 8px;
    color: var(--el-color-warning-dark-2);
    background: var(--el-color-warning-light-9);
    box-shadow: var(--el-box-shadow-light);
    transform: translateX(-50%);
  }

  /* 页面过渡动画 - 优化为更快速的淡入淡出 */
  .fade-enter-active,
  .fade-leave-active {
    transition: opacity 0.15s ease;
  }

  .fade-enter-from,
  .fade-leave-to {
    opacity: 0;
  }

  /* 滑动过渡 */
  .slide-enter-active,
  .slide-leave-active {
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  }

  .slide-enter-from {
    opacity: 0;
    transform: translateX(30px);
  }

  .slide-leave-to {
    opacity: 0;
    transform: translateX(-30px);
  }

  /* 缩放过渡 */
  .scale-enter-active,
  .scale-leave-active {
    transition: all 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  }

  .scale-enter-from {
    opacity: 0;
    transform: scale(0.95);
  }

  .scale-leave-to {
    opacity: 0;
    transform: scale(1.05);
  }
</style>
