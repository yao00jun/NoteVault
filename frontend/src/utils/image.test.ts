import { describe, it, expect } from 'vitest'
import {
  isImageFile,
  arrayBufferToBase64,
  generateMarkdownImage,
  getFileExtension,
  isSupportedImageFormat,
} from './image'

describe('image utils', () => {
  describe('isImageFile', () => {
    it('应识别 PNG 图片', () => {
      const file = new File(['content'], 'test.png', { type: 'image/png' })
      expect(isImageFile(file)).toBe(true)
    })

    it('应识别 JPEG 图片', () => {
      const file = new File(['content'], 'test.jpg', { type: 'image/jpeg' })
      expect(isImageFile(file)).toBe(true)
    })

    it('应识别 GIF 图片', () => {
      const file = new File(['content'], 'test.gif', { type: 'image/gif' })
      expect(isImageFile(file)).toBe(true)
    })

    it('不应识别文本文件', () => {
      const file = new File(['content'], 'test.txt', { type: 'text/plain' })
      expect(isImageFile(file)).toBe(false)
    })

    it('不应识别空类型文件', () => {
      const file = new File(['content'], 'test', { type: '' })
      expect(isImageFile(file)).toBe(false)
    })
  })

  describe('arrayBufferToBase64', () => {
    it('应将空 ArrayBuffer 转换为空 base64', () => {
      const buffer = new ArrayBuffer(0)
      expect(arrayBufferToBase64(buffer)).toBe('')
    })

    it('应将简单字节转换为正确的 base64', () => {
      // "Hello" 的 ASCII 码
      const buffer = new Uint8Array([72, 101, 108, 108, 111]).buffer
      expect(arrayBufferToBase64(buffer)).toBe('SGVsbG8=')
    })

    it('应将 PNG 魔数转换为正确的 base64', () => {
      // PNG 文件魔数：89 50 4E 47
      const buffer = new Uint8Array([0x89, 0x50, 0x4E, 0x47]).buffer
      expect(arrayBufferToBase64(buffer)).toBe('iVBORw==')
    })

    it('应正确处理大数据（超过分块大小）', () => {
      // 创建 10000 字节的数据
      const data = new Uint8Array(10000)
      for (let i = 0; i < 10000; i++) {
        data[i] = i % 256
      }
      const result = arrayBufferToBase64(data.buffer)
      // 验证结果是有效的 base64（长度应该是 ceil(10000/3)*4）
      const expectedLength = Math.ceil(10000 / 3) * 4
      expect(result.length).toBe(expectedLength)
      // 验证可以解码回来
      const decoded = atob(result)
      expect(decoded.length).toBe(10000)
    })

    it('转换结果应能被 atob 解码回原始数据', () => {
      const original = new Uint8Array([1, 2, 3, 4, 5, 250, 251, 252, 253, 254, 255])
      const base64 = arrayBufferToBase64(original.buffer)
      const decoded = atob(base64)
      const decodedBytes = new Uint8Array(decoded.length)
      for (let i = 0; i < decoded.length; i++) {
        decodedBytes[i] = decoded.charCodeAt(i)
      }
      expect(Array.from(decodedBytes)).toEqual(Array.from(original))
    })
  })

  describe('generateMarkdownImage', () => {
    it('应生成正确的 Markdown 图片链接', () => {
      const result = generateMarkdownImage('assets/image.png', '截图')
      expect(result).toBe('\n![截图](assets/image.png)\n')
    })

    it('未提供 alt 文本时应使用默认值', () => {
      const result = generateMarkdownImage('assets/image.png')
      expect(result).toBe('\n![image](assets/image.png)\n')
    })

    it('应正确处理带空格的路径', () => {
      const result = generateMarkdownImage('assets/my image.png', '图片')
      expect(result).toBe('\n![图片](assets/my image.png)\n')
    })

    it('应正确处理 URL 路径', () => {
      const result = generateMarkdownImage('https://example.com/image.jpg', '示例')
      expect(result).toBe('\n![示例](https://example.com/image.jpg)\n')
    })
  })

  describe('getFileExtension', () => {
    it('应提取 .png 扩展名', () => {
      expect(getFileExtension('image.png')).toBe('.png')
    })

    it('应提取 .jpeg 扩展名并转为小写', () => {
      expect(getFileExtension('photo.JPEG')).toBe('.jpeg')
    })

    it('应处理多重扩展名的文件', () => {
      expect(getFileExtension('archive.tar.gz')).toBe('.gz')
    })

    it('无扩展名时应返回空字符串', () => {
      expect(getFileExtension('README')).toBe('')
    })

    it('空文件名应返回空字符串', () => {
      expect(getFileExtension('')).toBe('')
    })

    it('以点开头的文件名应正确处理', () => {
      expect(getFileExtension('.gitignore')).toBe('.gitignore')
    })
  })

  describe('isSupportedImageFormat', () => {
    it('应支持 PNG', () => {
      expect(isSupportedImageFormat('image.png')).toBe(true)
    })

    it('应支持 JPG', () => {
      expect(isSupportedImageFormat('image.jpg')).toBe(true)
    })

    it('应支持 JPEG', () => {
      expect(isSupportedImageFormat('image.jpeg')).toBe(true)
    })

    it('应支持 GIF', () => {
      expect(isSupportedImageFormat('image.gif')).toBe(true)
    })

    it('应支持 WebP', () => {
      expect(isSupportedImageFormat('image.webp')).toBe(true)
    })

    it('应支持 SVG', () => {
      expect(isSupportedImageFormat('image.svg')).toBe(true)
    })

    it('应支持 BMP', () => {
      expect(isSupportedImageFormat('image.bmp')).toBe(true)
    })

    it('不应支持 TXT', () => {
      expect(isSupportedImageFormat('document.txt')).toBe(false)
    })

    it('不应支持 PDF', () => {
      expect(isSupportedImageFormat('document.pdf')).toBe(false)
    })

    it('应不区分大小写', () => {
      expect(isSupportedImageFormat('IMAGE.PNG')).toBe(true)
      expect(isSupportedImageFormat('Image.Jpg')).toBe(true)
    })
  })
})
