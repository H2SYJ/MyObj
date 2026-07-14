const VIDEO_FILE_EXTENSION = /\.(mp4|webm|ogg|mov|m4v|avi|mkv)$/i
const THUMBNAIL_MAX_DIMENSION = 300
const THUMBNAIL_JPEG_QUALITY = 0.85
const VIDEO_LOAD_TIMEOUT = 15000

export const isVideoFile = (file: File): boolean => {
  return file.type.startsWith('video/') || VIDEO_FILE_EXTENSION.test(file.name)
}

const waitForVideoEvent = (
  video: HTMLVideoElement,
  eventName: 'loadedmetadata' | 'loadeddata' | 'seeked',
  errorMessage: string
): Promise<void> => {
  return new Promise((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer)
      video.removeEventListener(eventName, handleSuccess)
      video.removeEventListener('error', handleError)
    }

    const handleSuccess = () => {
      cleanup()
      resolve()
    }

    const handleError = () => {
      cleanup()
      reject(new Error(errorMessage))
    }

    const timer = window.setTimeout(() => {
      cleanup()
      reject(new Error(`${errorMessage}：等待超时`))
    }, VIDEO_LOAD_TIMEOUT)

    video.addEventListener(eventName, handleSuccess, { once: true })
    video.addEventListener('error', handleError, { once: true })
  })
}

const canvasToJpeg = (canvas: HTMLCanvasElement): Promise<Blob> => {
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      blob => {
        if (blob) {
          resolve(blob)
          return
        }
        reject(new Error('生成视频缩略图失败'))
      },
      'image/jpeg',
      THUMBNAIL_JPEG_QUALITY
    )
  })
}

/**
 * 从本地视频截取一帧并生成 JPEG 缩略图。
 * 长视频截取 10% 处且不超过 5 秒，短视频截取中点。
 */
export const generateVideoThumbnail = async (file: File): Promise<File> => {
  const objectUrl = URL.createObjectURL(file)
  const video = document.createElement('video')

  video.preload = 'metadata'
  video.muted = true
  video.playsInline = true

  try {
    const metadataLoaded = waitForVideoEvent(video, 'loadedmetadata', '无法读取视频元数据')
    video.src = objectUrl
    video.load()
    await metadataLoaded

    if (!Number.isFinite(video.duration) || video.duration <= 0) {
      throw new Error('视频时长无效')
    }

    const captureTime = video.duration <= 2 ? video.duration / 2 : Math.min(video.duration * 0.1, 5)
    if (captureTime > 0) {
      const seeked = waitForVideoEvent(video, 'seeked', '无法定位视频缩略图帧')
      video.currentTime = captureTime
      await seeked
    } else {
      await waitForVideoEvent(video, 'loadeddata', '无法读取视频画面')
    }

    if (video.videoWidth <= 0 || video.videoHeight <= 0) {
      throw new Error('视频画面尺寸无效')
    }

    const scale = Math.min(1, THUMBNAIL_MAX_DIMENSION / Math.max(video.videoWidth, video.videoHeight))
    const width = Math.max(1, Math.round(video.videoWidth * scale))
    const height = Math.max(1, Math.round(video.videoHeight * scale))
    const canvas = document.createElement('canvas')
    canvas.width = width
    canvas.height = height

    const context = canvas.getContext('2d')
    if (!context) {
      throw new Error('浏览器不支持生成视频缩略图')
    }

    context.drawImage(video, 0, 0, width, height)
    const thumbnailBlob = await canvasToJpeg(canvas)
    const baseName = file.name.replace(/\.[^.]+$/, '') || 'video'
    return new File([thumbnailBlob], `${baseName}.thumbnail.jpg`, { type: 'image/jpeg' })
  } finally {
    video.pause()
    video.removeAttribute('src')
    video.load()
    URL.revokeObjectURL(objectUrl)
  }
}
