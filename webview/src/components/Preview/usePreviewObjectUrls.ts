import { ref } from 'vue'

export type PreviewUrlKind = 'image' | 'video' | 'audio' | 'pdf'

export function isPreviewObjectUrl(url: string) {
  return url.startsWith('blob:')
}

export function usePreviewObjectUrls(revoke: (url: string) => void = url => window.URL.revokeObjectURL(url)) {
  const imageUrl = ref('')
  const videoUrl = ref('')
  const audioUrl = ref('')
  const pdfUrl = ref('')
  const urls = { image: imageUrl, video: videoUrl, audio: audioUrl, pdf: pdfUrl }

  const release = (url: string) => {
    if (isPreviewObjectUrl(url)) revoke(url)
  }

  const setPreviewUrl = (kind: PreviewUrlKind, nextUrl: string) => {
    const currentUrl = urls[kind].value
    if (currentUrl && currentUrl !== nextUrl) release(currentUrl)
    urls[kind].value = nextUrl
  }

  const releaseUncommittedUrl = (url: string) => release(url)

  const clearPreviewUrls = () => {
    for (const url of Object.values(urls)) {
      release(url.value)
      url.value = ''
    }
  }

  return {
    imageUrl,
    videoUrl,
    audioUrl,
    pdfUrl,
    setPreviewUrl,
    releaseUncommittedUrl,
    clearPreviewUrls
  }
}
