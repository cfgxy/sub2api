import { ref } from 'vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

export const enterpriseTestLocale = ref<'zh' | 'en'>('zh')

export function useEnterpriseTestI18n() {
  return {
    locale: enterpriseTestLocale,
    t(key: string, named?: Record<string, string | number>): string {
      const messages = enterpriseTestLocale.value === 'zh' ? zh : en
      const message = key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], messages)
      if (typeof message !== 'string') throw new Error(`缺少词条：${key}`)
      return message.replace(/\{(\w+)\}/g, (_, name: string) => String(named?.[name] ?? `{${name}}`))
    },
  }
}
