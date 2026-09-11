import type { EnterpriseBrand } from '@/types/enterprise'

const ALLOWED_IMAGE_TYPES = new Set(['image/jpeg', 'image/png', 'image/webp'])
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
  const enterpriseNameLength = Array.from(input.enterprise_name.trim()).length
  if (enterpriseNameLength > 255) return '企业名称不能超过 255 个字符'
  if (Array.from(input.title).length > 40) return '品牌标题不能超过 40 个字符'
  if (Array.from(input.body).length > 120) return '品牌正文不能超过 120 个字符'
  if (Array.from(input.slogan).length > 60) return '品牌标语不能超过 60 个字符'
  if ([input.enterprise_name, input.title, input.body, input.slogan].some((value) => /[<>]|script|svg/i.test(value))) {
    return '品牌文案包含不允许的内容'
  }
  if (input.background_url && input.background_url !== '/logo.svg' && input.background_url !== '/api/v1/enterprise/brand/background') {
    return '背景图片必须使用平台托管资源'
  }
  return ''
}

export function validateImageFile(file: File): string {
  if (!ALLOWED_IMAGE_TYPES.has(file.type.toLowerCase())) return '仅支持 JPG、PNG 或 WebP 文件'
  return file.size > 0 && file.size <= MAX_BACKGROUND_BYTES ? '' : '图片大小必须在 5MB 以内'
}
