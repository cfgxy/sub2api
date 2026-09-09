import type { EnterpriseBrand } from '@/types/enterprise'

const ALLOWED_IMAGE_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp'])
const ALLOWED_IMAGE_EXTENSIONS = ['.jpg', '.jpeg', '.png', '.webp']
const MAX_BACKGROUND_BYTES = 5 * 1024 * 1024

export function validatePassword(value: string): string {
  return value.length >= 12 ? '' : '密码至少需要 12 个字符'
}

export function validateDepartmentName(value: string): string {
  const length = Array.from(value.trim()).length
  if (!length) return '请输入部门名称'
  return length <= 100 ? '' : '部门名称不能超过 100 个字符'
}

export function validateBrand(input: EnterpriseBrand): string {
  if (Array.from(input.title).length > 40) return '品牌标题不能超过 40 个字符'
  if (Array.from(input.body).length > 120) return '品牌正文不能超过 120 个字符'
  if (Array.from(input.slogan).length > 60) return '品牌标语不能超过 60 个字符'
  if ([input.title, input.body, input.slogan].some((value) => /[<>]|script|svg/i.test(value))) {
    return '品牌文案包含不允许的内容'
  }
  if (!input.background_url) {
    return input.background_content_type || input.background_sha256 || input.background_size_bytes
      ? '未设置背景地址时不能提交图片元数据'
      : ''
  }
  if (!/^(https|asset):\/\//.test(input.background_url)) return '背景地址必须使用 HTTPS 或 asset 协议'
  const path = input.background_url.toLowerCase().split(/[?#]/)[0]
  if (!ALLOWED_IMAGE_EXTENSIONS.some((extension) => path.endsWith(extension))) return '背景仅支持 JPG、PNG 或 WebP'
  if (!ALLOWED_IMAGE_TYPES.has(input.background_content_type.toLowerCase())) return '背景图片类型无效'
  if (!/^[a-f\d]{64}$/i.test(input.background_sha256)) return '背景图片 SHA256 必须为 64 位十六进制'
  if (input.background_size_bytes <= 0 || input.background_size_bytes > MAX_BACKGROUND_BYTES) return '背景图片大小必须在 5MB 以内'
  return ''
}

export function validateImageFile(file: File): string {
  if (!ALLOWED_IMAGE_TYPES.has(file.type.toLowerCase())) return '仅支持 JPG、PNG 或 WebP 文件'
  return file.size > 0 && file.size <= MAX_BACKGROUND_BYTES ? '' : '图片大小必须在 5MB 以内'
}
