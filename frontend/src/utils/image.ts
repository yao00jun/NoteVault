/**
 * 图片处理工具函数
 */

/**
 * 判断文件是否为图片类型
 */
export function isImageFile(file: File): boolean {
  return file.type.startsWith('image/')
}

/**
 * 从剪贴板数据中提取图片文件
 * 返回第一个图片文件，如果没有则返回 null
 */
export function extractImageFromClipboard(dataTransfer: DataTransfer | null): File | null {
  if (!dataTransfer || !dataTransfer.items) return null

  for (let i = 0; i < dataTransfer.items.length; i++) {
    const item = dataTransfer.items[i]
    if (item.type.startsWith('image/')) {
      const file = item.getAsFile()
      if (file) return file
    }
  }
  return null
}

/**
 * 将 ArrayBuffer 转换为 base64 字符串
 * 用于将图片二进制数据传递给 Wails 后端（Go 的 []byte 对应 base64 字符串）
 */
export function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  // 分块处理，避免大文件时栈溢出
  const chunkSize = 8192
  for (let i = 0; i < bytes.byteLength; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize)
    binary += String.fromCharCode(...chunk)
  }
  return btoa(binary)
}

/**
 * 生成 Markdown 图片链接
 */
export function generateMarkdownImage(imagePath: string, altText?: string): string {
  const alt = altText || 'image'
  return `\n![${alt}](${imagePath})\n`
}

/**
 * 从文件名提取扩展名（小写，含点）
 * 没有点的文件名返回空字符串
 */
export function getFileExtension(fileName: string): string {
  const lastDotIndex = fileName.lastIndexOf('.')
  if (lastDotIndex === -1) return ''
  return fileName.substring(lastDotIndex).toLowerCase()
}

/**
 * 判断是否为支持的图片格式
 */
export function isSupportedImageFormat(fileName: string): boolean {
  const ext = getFileExtension(fileName)
  return ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.svg', '.bmp'].includes(ext)
}
