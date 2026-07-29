<template>
  <div ref="playerContainer" class="xg-player-wrapper"></div>
</template>

<script setup lang="ts">
  import { getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import Player, { Events } from 'xgplayer'
  import 'xgplayer/dist/index.min.css'
  import { useI18n } from '@/composables/core/useI18n'
  import { useResponsive } from '@/composables/ui/useResponsive'
  import { LanguageEnum } from '@/enums/LanguageEnum'

  interface Props {
    src: string
    autoplay?: boolean
    loop?: boolean
  }

  interface Emits {
    ready: []
    error: [message: string]
    play: []
    pause: []
    ended: []
  }

  const props = withDefaults(defineProps<Props>(), {
    autoplay: false,
    loop: false
  })

  const emit = defineEmits<Emits>()
  const { locale, t } = useI18n()
  const { isHandheld: isMobile } = useResponsive()
  const playerContainer = ref<HTMLElement | null>(null)
  const proxy = getCurrentInstance()?.proxy
  let playerInstance: Player | null = null

  const handlePlayerError = (error?: unknown) => {
    const errorMessage = t('preview.video.loadFailed')
    emit('error', errorMessage)
    proxy?.$log?.error('xgplayer 播放错误', error)
  }

  const bindPlayerEvents = (player: Player) => {
    player.on(Events.READY, () => emit('ready'))
    player.on(Events.ERROR, handlePlayerError)
    player.on(Events.PLAY, () => emit('play'))
    player.on(Events.PAUSE, () => emit('pause'))
    player.on(Events.ENDED, () => emit('ended'))
  }

  const destroyPlayer = () => {
    if (!playerInstance) {
      return
    }

    playerInstance.destroy()
    playerInstance = null
  }

  const initPlayer = async () => {
    if (!props.src) {
      return
    }

    await nextTick()
    if (!playerContainer.value) {
      return
    }

    destroyPlayer()
    playerInstance = new Player({
      el: playerContainer.value,
      url: props.src,
      autoplay: props.autoplay,
      loop: props.loop,
      playsinline: true,
      width: '100%',
      height: '100%',
      videoFillMode: 'contain',
      lang: locale.value === LanguageEnum.en_US ? 'en' : 'zh-cn',
      fullscreen: {
        // 只有手机开启旋转全屏，PC 关闭并使用浏览器原生全屏
        rotateFullscreen: isMobile.value
      }
    })
    bindPlayerEvents(playerInstance)
  }

  watch(
    () => props.src,
    newSrc => {
      if (!newSrc) {
        destroyPlayer()
        return
      }

      if (!playerInstance) {
        void initPlayer()
        return
      }

      const switchResult = playerInstance.switchURL(newSrc)
      if (switchResult) {
        void switchResult.catch(handlePlayerError)
      }
    }
  )

  defineExpose({
    play: () => playerInstance?.play(),
    pause: () => playerInstance?.pause(),
    restart: () => playerInstance?.replay(),
    getInstance: () => playerInstance
  })

  onMounted(() => {
    void initPlayer()
  })

  onBeforeUnmount(() => {
    destroyPlayer()
  })
</script>

<style scoped>
  .xg-player-wrapper {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    border-radius: 8px;
    background: var(--el-bg-color-page, #000);
  }

  .xg-player-wrapper :deep(video) {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
</style>
