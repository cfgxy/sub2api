import { describe, expect, it } from 'vitest'
import {
  buildEnterpriseCcsImportUri,
  buildEnterpriseCodexImportUri,
  buildEnterpriseGuideExample,
  enterpriseImportBaseUrls,
  resolveEnterpriseCodexCandidate,
} from '../enterpriseImport'

const fakeKey = 'sk-test-FAKE-411'

describe('enterprise Base URL and import URIs', () => {
  it('derives an OpenAI address with /v1 and an Anthropic address without /v1 from any configured form', () => {
    const expected = { root: 'https://tenant.example.test/custom', openai: 'https://tenant.example.test/custom/v1' }
    expect(enterpriseImportBaseUrls('https://tenant.example.test/custom/v1')).toEqual(expected)
    expect(enterpriseImportBaseUrls('https://tenant.example.test/custom/')).toEqual(expected)
    expect(enterpriseImportBaseUrls('https://tenant.example.test')).toEqual({
      root: 'https://tenant.example.test',
      openai: 'https://tenant.example.test/v1',
    })
  })

  it('refuses unsafe or unparsable deployment addresses', () => {
    for (const value of ['', 'javascript:invalid', 'http://tenant.example.test/v1', 'https://u:p@tenant.example.test', 'https://tenant.example.test/?q=1']) {
      expect(() => enterpriseImportBaseUrls(value)).toThrow()
    }
    expect(enterpriseImportBaseUrls('http://localhost:8080').openai).toBe('http://localhost:8080/v1')
  })

  it('builds the Codex++ import URI with the OpenAI address and the selected model', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test')
    const candidate = resolveEnterpriseCodexCandidate(address, { id: 'gpt-5', platform: 'openai' })
    expect(candidate).toMatchObject({ baseUrl: 'https://tenant.example.test/v1', wireApi: 'responses', model: 'gpt-5' })
    const uri = new URL(buildEnterpriseCodexImportUri(candidate!, fakeKey))
    expect(uri.protocol).toBe('codexplusplus:')
    expect(uri.searchParams.get('baseUrl')).toBe('https://tenant.example.test/v1')
    expect(uri.searchParams.get('model')).toBe('gpt-5')
    expect(uri.searchParams.get('apiKey')).toBe(fakeKey)
  })

  it('does not offer Codex++ import for non-OpenAI models', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test')
    expect(resolveEnterpriseCodexCandidate(address, { id: 'claude-x', platform: 'anthropic' })).toBeNull()
    expect(resolveEnterpriseCodexCandidate(address, { id: 'gemini-x', platform: 'gemini' })).toBeNull()
  })

  it('builds CCSwitch links per platform with the selected model', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test/v1')
    const openai = new URL(buildEnterpriseCcsImportUri(address, { id: 'gpt-5', platform: 'openai' }, fakeKey))
    expect(openai.protocol).toBe('ccswitch:')
    expect(openai.searchParams.get('app')).toBe('codex')
    expect(openai.searchParams.get('endpoint')).toBe('https://tenant.example.test/v1')
    expect(openai.searchParams.get('model')).toBe('gpt-5')
    const anthropic = new URL(buildEnterpriseCcsImportUri(address, { id: 'claude-x', platform: 'anthropic' }, fakeKey))
    expect(anthropic.searchParams.get('app')).toBe('claude')
    expect(anthropic.searchParams.get('endpoint')).toBe('https://tenant.example.test')
    expect(anthropic.searchParams.get('model')).toBe('claude-x')
    expect(() => buildEnterpriseCcsImportUri(address, { id: 'gemini-x', platform: 'gemini' }, fakeKey)).toThrow()
  })

  it('refuses masked or empty keys for both import targets', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test')
    const candidate = resolveEnterpriseCodexCandidate(address, { id: 'gpt-5', platform: 'openai' })!
    expect(() => buildEnterpriseCodexImportUri(candidate, 'sk-abc...1234')).toThrow()
    expect(() => buildEnterpriseCcsImportUri(address, { id: 'gpt-5', platform: 'openai' }, '  ')).toThrow()
  })

  it('renders examples against the protocol address and never embeds a real key', () => {
    const address = enterpriseImportBaseUrls('https://tenant.example.test')
    const openaiCurl = buildEnterpriseGuideExample('curl', address, { id: 'gpt-5', platform: 'openai' })
    expect(openaiCurl).toContain('https://tenant.example.test/v1/chat/completions')
    expect(openaiCurl).toContain('Authorization: Bearer {{API_KEY}}')
    expect(openaiCurl).toContain('"gpt-5"')
    const anthropicCurl = buildEnterpriseGuideExample('curl', address, { id: 'claude-x', platform: 'anthropic' })
    expect(anthropicCurl).toContain('https://tenant.example.test/v1/messages')
    expect(anthropicCurl).toContain('x-api-key: {{API_KEY}}')
    expect(buildEnterpriseGuideExample('python', address, { id: 'gpt-5', platform: 'openai' })).toContain('urlopen')
    expect(buildEnterpriseGuideExample('javascript', address, { id: 'claude-x', platform: 'anthropic' })).toContain('/v1/messages')
    for (const kind of ['curl', 'python', 'javascript'] as const) {
      expect(buildEnterpriseGuideExample(kind, address, { id: 'gpt-5', platform: 'openai' })).not.toContain(fakeKey)
    }
  })
})
