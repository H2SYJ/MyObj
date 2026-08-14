const THUMBNAIL_MAX_SIZE = 1024 * 1024
const THUMBNAIL_MAX_DIMENSION = 1000
const JPEG_FILE_EXTENSION = /\.jpe?g$/i

const readImageDimensions = (file: File): Promise<{ width: number; height: number }> =>
  new Promise((resolve, reject) => {
    const objectUrl = URL.createObjectURL(file)
    const image = new Image()

    const cleanup = () => {
      image.onload = null
      image.onerror = null
      URL.revokeObjectURL(objectUrl)
    }

    image.onload = () => {
      const dimensions = { width: image.naturalWidth, height: image.naturalHeight }
      cleanup()
      resolve(dimensions)
    }
    image.onerror = () => {
      cleanup()
      reject(new Error('无法读取缩略图，请确认文件未损坏'))
    }
    image.src = objectUrl
  })

/** 校验手动上传的缩略图是否符合服务端接收约束。 */
export const validateThumbnailFile = async (file: File): Promise<void> => {
  if (file.type !== 'image/jpeg' && !JPEG_FILE_EXTENSION.test(file.name)) {
    throw new Error('缩略图仅支持 JPEG 格式')
  }
  if (file.size > THUMBNAIL_MAX_SIZE) {
    throw new Error('缩略图不能超过 1 MiB')
  }

  const { width, height } = await readImageDimensions(file)
  if (width <= 0 || height <= 0) {
    throw new Error('缩略图尺寸无效')
  }
  if (width > THUMBNAIL_MAX_DIMENSION || height > THUMBNAIL_MAX_DIMENSION) {
    throw new Error('缩略图宽高不能超过 1000 像素')
  }
}
