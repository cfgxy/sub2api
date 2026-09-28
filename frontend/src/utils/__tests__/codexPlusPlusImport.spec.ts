import { describe, expect, it } from 'vitest'
import type { ApiKey } from '@/types'
import { buildCodexPlusPlusImportUri, resolveCodexPlusPlusCandidate } from '../codexPlusPlusImport'

const openaiKey = {
  id: 42,
  name: 'test',
  key: 'sk-test-FAKE-397',
  status: 'active',
  group: { platform: 'openai', status: 'active' }
} as ApiKey

describe('Codex++ v1 供应商导入', () => {
  it('按 OpenAI Responses 口径传递当前站点的 /v1 地址，保留路径前缀且不重复追加', () => {
    const candidate = resolveCodexPlusPlusCandidate(openaiKey, 'https://api.example.test/proxy/')
    expect(candidate).toEqual({
      name: 'Sub2API / OpenAI / key #42',
      baseUrl: 'https://api.example.test/proxy/v1',
      wireApi: 'responses',
      relayMode: 'pureApi'
    })
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.example.test/proxy/v1')?.baseUrl)
      .toBe('https://api.example.test/proxy/v1')
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'http://127.0.0.1:18080')?.baseUrl)
      .toBe('http://127.0.0.1:18080/v1')
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'http://localhost:18080/v1')?.baseUrl)
      .toBe('http://localhost:18080/v1')
    expect(candidate).not.toHaveProperty('apiKey')
  })

  it('仅在用户点击时构造含密钥的 v1 URI，模型仅在已核实时传递', () => {
    const key = { ...openaiKey, name: '测试 密钥' }
    const candidate = resolveCodexPlusPlusCandidate(key, 'https://api.example.test', ['gpt-test'], 'gpt-test')!
    const uri = new URL(buildCodexPlusPlusImportUri(candidate, key.key))
    expect(uri.protocol).toBe('codexplusplus:')
    expect(uri.host).toBe('v1')
    expect(uri.pathname).toBe('/import/provider')
    expect(Object.fromEntries(uri.searchParams)).toEqual({
      resource: 'provider',
      name: 'Sub2API / OpenAI / key #42',
      baseUrl: 'https://api.example.test/v1',
      apiKey: key.key,
      wireApi: 'responses',
      relayMode: 'pureApi',
      model: 'gpt-test'
    })
    expect(uri.href).not.toContain('https://api.example.test/v1')
    expect(new URL(buildCodexPlusPlusImportUri(resolveCodexPlusPlusCandidate(key, 'https://api.example.test')!, key.key))
      .searchParams.has('model')).toBe(false)
  })

  it('仅对可读取的有效 OpenAI Key 开放，不把 Claude 地址误配置为 Responses', () => {
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, key: 'sk-****' }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, status: 'inactive' }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, quota: 10, quota_used: 10 }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, expires_at: '2020-01-01T00:00:00Z' }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, group: undefined }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, group: { ...openaiKey.group!, platform: 'anthropic' } }, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'http://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'http://192.168.1.20:18080')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://name:pass@api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.test/v1?key=bad')).toBeNull()
    expect(() => buildCodexPlusPlusImportUri(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.test')!, 'sk-****')).toThrow()
  })
})
