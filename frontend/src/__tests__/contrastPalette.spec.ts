import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import colors from 'tailwindcss/colors'
import tailwindConfig from '../../tailwind.config.js'

const projectColors = (tailwindConfig as { theme: { extend: { colors: Record<string, Record<string, string>> } } }).theme.extend.colors
const palette: Record<string, Record<string, string>> = {
  gray: colors.gray as unknown as Record<string, string>,
  green: colors.green as unknown as Record<string, string>,
  red: colors.red as unknown as Record<string, string>,
  orange: colors.orange as unknown as Record<string, string>,
  primary: projectColors.primary,
  dark: projectColors.dark
}

function resolveToken(token: string): string {
  if (token === 'white') return '#ffffff'
  const [name, shade] = token.split('-')
  const hex = palette[name]?.[shade]
  if (!hex) throw new Error(`未知色板词条：${token}`)
  return hex
}

function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
  const [r, g, b] = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(fg: string, bg: string): number {
  const [hi, lo] = [luminance(resolveToken(fg)), luminance(resolveToken(bg))].sort((a, b) => b - a)
  return (hi + 0.05) / (lo + 0.05)
}

interface Site {
  file: string
  fixed: string
  legacy: string
  fg: string
  bg: string
}

const read = (file: string) => readFileSync(resolve(__dirname, '..', file), 'utf8')

const lightSites: Site[] = [
  { file: 'style.css', fixed: '@apply text-gray-500 dark:text-dark-400;\n  }\n\n  /* ============ 页面头部', legacy: '@apply text-gray-400 dark:text-dark-500;', fg: 'gray-500', bg: 'white' },
  { file: 'components/admin/PlatformEnterpriseShell.vue', fixed: 'text-[10px] font-medium text-gray-500 dark:text-dark-400', legacy: 'text-[10px] font-medium text-gray-400 dark:text-dark-500', fg: 'gray-500', bg: 'white' },
  { file: 'components/enterprise/EnterpriseLayout.vue', fixed: 'text-[10px] font-medium text-gray-500 dark:text-dark-400', legacy: 'text-[10px] font-medium text-gray-400 dark:text-dark-500', fg: 'gray-500', bg: 'white' },
  { file: 'views/admin/EnterprisesView.vue', fixed: 'class="text-gray-500 dark:text-dark-400" data-testid="enterprise-no-subscriptions"', legacy: 'class="text-gray-400 dark:text-dark-500" data-testid="enterprise-no-subscriptions"', fg: 'gray-500', bg: 'white' },
  { file: 'components/common/Input.vue', fixed: '<span v-if="required" class="text-red-600 dark:text-red-400">*</span>', legacy: '<span v-if="required" class="text-red-500">*</span>', fg: 'red-600', bg: 'white' },
  { file: 'views/admin/DashboardView.vue', fixed: 'class="text-green-700 dark:text-green-400"', legacy: 'class="text-green-600 dark:text-green-400"', fg: 'green-700', bg: 'white' },
  { file: 'views/admin/DashboardView.vue', fixed: 'class="text-orange-700 dark:text-orange-400"', legacy: 'class="text-orange-500 dark:text-orange-400"', fg: 'orange-700', bg: 'white' },
  { file: 'views/admin/DashboardView.vue', fixed: '<span class="text-gray-500 dark:text-gray-400"> / </span>', legacy: '<span class="text-gray-400 dark:text-gray-500"> / </span>', fg: 'gray-500', bg: 'white' },
  { file: 'views/admin/EnterpriseCreateView.vue', fixed: 'bg-primary-50 text-xs font-bold text-primary-700 dark:bg-primary-900/30', legacy: 'bg-primary-50 text-xs font-bold text-primary-600 dark:bg-primary-900/30', fg: 'primary-700', bg: 'primary-50' },
  { file: 'style.css', fixed: '@apply bg-primary-50 dark:bg-primary-900/20;\n    @apply text-primary-700 dark:text-primary-400;', legacy: '@apply bg-primary-50 dark:bg-primary-900/20;\n    @apply text-primary-600 dark:text-primary-400;', fg: 'primary-700', bg: 'primary-50' }
]

const darkSites = [
  { fg: 'dark-400', bg: 'dark-900', label: '侧栏分组标题 / 品牌副标题 / 无订阅' },
  { fg: 'gray-400', bg: 'dark-900', label: '仪表盘分隔符 / 零值' }
]

describe('B-3 对比度：颜色词条落地且满足 WCAG AA 4.5:1', () => {
  it.each(lightSites)('浅色 $file：$fixed', (site) => {
    const source = read(site.file)
    expect(source).toContain(site.fixed)
    expect(source).not.toContain(site.legacy)
    expect(contrast(site.fg, site.bg)).toBeGreaterThanOrEqual(4.5)
  })

  it.each(darkSites)('深色 $label', ({ fg, bg }) => {
    expect(contrast(fg, bg)).toBeGreaterThanOrEqual(4.5)
  })

  it('旧色值在同一位置低于 4.5:1（防止用例失去区分度）', () => {
    expect(contrast('gray-400', 'white')).toBeLessThan(4.5)
    expect(contrast('primary-600', 'primary-50')).toBeLessThan(4.5)
    expect(contrast('green-600', 'white')).toBeLessThan(4.5)
    expect(contrast('red-500', 'white')).toBeLessThan(4.5)
    expect(contrast('orange-500', 'white')).toBeLessThan(4.5)
    expect(contrast('dark-500', 'dark-900')).toBeLessThan(4.5)
  })
})
