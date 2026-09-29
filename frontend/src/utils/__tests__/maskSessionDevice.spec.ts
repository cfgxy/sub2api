import { describe, expect, it } from 'vitest'
import { describeUserAgent, maskIpAddress } from '../maskSessionDevice'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const CJK = /[\u3400-\u9fff\uff00-\uffef]/
const translator = (messages: unknown) => (key: string) => {
  const value = key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], messages)
  if (typeof value !== 'string') throw new Error(`缺少词条：${key}`)
  return value
}
const tZh = translator(zh)
const tEn = translator(en)

describe('describeUserAgent', () => {
  it('extracts a whitelisted browser/OS label without echoing the raw string', () => {
    const ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36'
    const label = describeUserAgent(ua, tZh)
    expect(label).toBe('Chrome · Windows')
    expect(label).not.toContain('AppleWebKit')
    expect(label).not.toContain('537.36')
  })

  it('falls back to unknown labels for unrecognized or empty input', () => {
    expect(describeUserAgent('', tZh)).toBe('未知设备')
    expect(describeUserAgent(undefined, tZh)).toBe('未知设备')
    expect(describeUserAgent('some-custom-bot/1.0', tZh)).toBe('未知浏览器 · 未知系统')
  })
})

describe('en 语言下的兜底文案', () => {
  it('未知设备/浏览器/系统/来源均为英文，不含中文', () => {
    const labels = [
      describeUserAgent('', tEn),
      describeUserAgent('some-custom-bot/1.0', tEn),
      maskIpAddress('', tEn),
    ]
    expect(labels).toEqual(['Unknown device', 'Unknown browser · Unknown system', 'Source unavailable'])
    for (const label of labels) expect(label).not.toMatch(CJK)
  })
})

describe('maskIpAddress', () => {
  it('keeps only the first IPv4 octet', () => {
    expect(maskIpAddress('203.0.113.9', tZh)).toBe('203.***.***.**')
  })

  it('keeps only the first IPv6 group', () => {
    expect(maskIpAddress('2001:db8::1', tZh)).toBe('2001:****:****')
  })

  it('reports unavailable source instead of leaking an empty value', () => {
    expect(maskIpAddress('', tZh)).toBe('来源不可用')
    expect(maskIpAddress(undefined, tZh)).toBe('来源不可用')
  })
})
