import { describe, expect, it } from 'vitest'
import { buildEnterpriseCodexImportUri, buildEnterpriseCcsImportUri, enterpriseImportBaseUrls } from './enterpriseImport'

describe('enterprise developer tool import', () => {
  it('transmits the configured address with the correct wire protocol', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test/custom/v1')
    expect(address).toEqual({ root: 'https://tenant.example.test/custom', openai: 'https://tenant.example.test/custom/v1' })
    const openai = new URL(buildEnterpriseCodexImportUri({ address, key: 'sk-test-FAKE-389', platform: 'openai', model: 'model-a' }))
    expect(openai.protocol).toBe('codexplusplus:')
    expect(openai.host).toBe('v1')
    expect(openai.pathname).toBe('/import/provider')
    expect(openai.searchParams.get('resource')).toBe('provider')
    expect(openai.searchParams.get('baseUrl')).toBe(address.openai)
    expect(openai.searchParams.get('wireApi')).toBe('responses')
    expect(openai.searchParams.get('model')).toBe('model-a')
    expect(openai.searchParams.get('apiKey')).toBe('sk-test-FAKE-389')

    const anthropic = new URL(buildEnterpriseCodexImportUri({ address, key: 'sk-test-FAKE-389', platform: 'anthropic', model: 'claude-example' }))
    expect(anthropic.searchParams.get('baseUrl')).toBe(address.root)
    expect(anthropic.searchParams.get('wireApi')).toBe('messages')
    expect(anthropic.searchParams.get('model')).toBe('claude-example')
  })

  it('keeps CCSwitch provider transport compatible with the regular Key page', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test/v1')
    const openai = new URL(buildEnterpriseCcsImportUri({ address, key: 'sk-test-FAKE-389', platform: 'openai', model: 'gpt-5' }))
    expect(openai.searchParams.get('app')).toBe('codex')
    expect(openai.searchParams.get('endpoint')).toBe(address.openai)
    expect(openai.searchParams.get('model')).toBe('gpt-5')
    const anthropic = new URL(buildEnterpriseCcsImportUri({ address, key: 'sk-test-FAKE-389', platform: 'anthropic', model: 'claude-sonnet-4' }))
    expect(anthropic.searchParams.get('app')).toBe('claude')
    expect(anthropic.searchParams.get('endpoint')).toBe(address.root)
    expect(anthropic.searchParams.get('model')).toBe('claude-sonnet-4')
  })

  it('refuses invalid deployment endpoints and masked or missing keys', () => {
    expect(() => enterpriseImportBaseUrls('')).toThrow()
    expect(() => enterpriseImportBaseUrls('https://user:password@tenant.example.test/v1')).toThrow()
    expect(() => enterpriseImportBaseUrls('http://tenant.example.test/v1')).toThrow()
    const address = enterpriseImportBaseUrls('https://tenant.example.test/v1')
    expect(() => buildEnterpriseCodexImportUri({ address, key: 'sk-abc...1234', platform: 'openai' })).toThrow()
    expect(() => buildEnterpriseCcsImportUri({ address, key: '', platform: 'anthropic' })).toThrow()
  })
})
