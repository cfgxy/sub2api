import { describe, expect, it } from 'vitest'
import { describeUserAgent, maskIpAddress } from '../maskSessionDevice'

describe('describeUserAgent', () => {
  it('extracts a whitelisted browser/OS label without echoing the raw string', () => {
    const ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36'
    const label = describeUserAgent(ua)
    expect(label).toBe('Chrome · Windows')
    expect(label).not.toContain('AppleWebKit')
    expect(label).not.toContain('537.36')
  })

  it('falls back to unknown labels for unrecognized or empty input', () => {
    expect(describeUserAgent('')).toBe('未知设备')
    expect(describeUserAgent(undefined)).toBe('未知设备')
    expect(describeUserAgent('some-custom-bot/1.0')).toBe('未知浏览器 · 未知系统')
  })
})

describe('maskIpAddress', () => {
  it('keeps only the first IPv4 octet', () => {
    expect(maskIpAddress('203.0.113.9')).toBe('203.***.***.**')
  })

  it('keeps only the first IPv6 group', () => {
    expect(maskIpAddress('2001:db8::1')).toBe('2001:****:****')
  })

  it('reports unavailable source instead of leaking an empty value', () => {
    expect(maskIpAddress('')).toBe('来源不可用')
    expect(maskIpAddress(undefined)).toBe('来源不可用')
  })
})
