import { describe, expect, it, vi } from 'vitest'
import type { ApiKey } from '@/types'
import {
  buildCodexPlusPlusImportUri,
  cancelCodexPlusPlusSession,
  createCodexPlusPlusSessionId,
  readCodexPlusPlusChallenge,
  resolveCodexPlusPlusCandidate
} from '../codexPlusPlusImport'

const sid = '0123456789abcdef0123456789abcdef'

const openaiKey = {
  id: 42,
  name: 'test',
  key: 'sk-test-FAKE-397',
  status: 'active',
  group: {
    platform: 'openai',
    status: 'active'
  }
} as ApiKey
const verifiedModels = ['gpt-test-available']
const selectedModel = 'gpt-test-available'

describe('Codex++ v2 无凭据接缝', () => {
  it('每次生成 128 位随机会话号', () => {
    const first = createCodexPlusPlusSessionId()
    expect(first).toMatch(/^[0-9a-f]{32}$/)
    expect(createCodexPlusPlusSessionId()).not.toBe(first)
  })

  it('仅把随机会话和网页来源写入 v2 URI', () => {
    const uri = new URL(buildCodexPlusPlusImportUri(sid, 'https://console.example.test'))
    expect(uri.protocol).toBe('codexplusplus:')
    expect(uri.host).toBe('v2')
    expect(uri.pathname).toBe('/import/provider')
    expect([...uri.searchParams.keys()]).toEqual(['sid', 'origin'])
    expect(uri.searchParams.get('sid')).toBe(sid)
    expect(uri.searchParams.get('origin')).toBe('https://console.example.test')
    expect(uri.href).not.toContain(openaiKey.key)
  })

  it('拒绝不合法的会话号或来源', () => {
    expect(() => buildCodexPlusPlusImportUri('short', 'https://console.example.test')).toThrow()
    expect(() => buildCodexPlusPlusImportUri(sid, 'https://console.example.test/path')).toThrow()
    expect(() => buildCodexPlusPlusImportUri(sid, 'http://other.example.test')).toThrow()
  })

  it('仅对可读取的有效 OpenAI Key 和已确认模型生成候选配置', () => {
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.example.test/proxy/', verifiedModels, selectedModel)).toEqual({
      name: 'Sub2API / OpenAI / key #42',
      baseUrl: 'https://api.example.test/proxy/v1',
      platform: 'openai',
      model: 'gpt-test-available'
    })
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.example.test/v1', verifiedModels, selectedModel)?.baseUrl)
      .toBe('https://api.example.test/v1')
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.example.test', verifiedModels, selectedModel)).not.toHaveProperty('apiKey')
  })

  it('在权限、平台、模型或 HTTPS 地址无法证实时关闭候选', () => {
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, key: 'sk-****' }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, status: 'inactive' }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, quota: 10, quota_used: 10 }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, expires_at: '2020-01-01T00:00:00Z' }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, group: undefined }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate({ ...openaiKey, group: { ...openaiKey.group!, platform: 'composite' } }, 'https://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.test')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.test', verifiedModels, 'unknown')).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'http://api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://name:pass@api.test', verifiedModels, selectedModel)).toBeNull()
    expect(resolveCodexPlusPlusCandidate(openaiKey, 'https://api.test/v1?key=bad', verifiedModels, selectedModel)).toBeNull()
  })

  it('就绪探针不携带密钥或浏览器凭据且校验响应', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ state: 'ready', challenge: 'a'.repeat(32) })
    })
    await expect(readCodexPlusPlusChallenge(sid, fetcher)).resolves.toBe('a'.repeat(32))
    expect(fetcher).toHaveBeenCalledWith(
      `http://127.0.0.1:49371/v2/import/sessions/${sid}`,
      expect.objectContaining({ credentials: 'omit', cache: 'no-store', redirect: 'error', referrerPolicy: 'no-referrer' })
    )
    expect(JSON.stringify(fetcher.mock.calls)).not.toContain(openaiKey.key)
  })

  it('未就绪或旧版响应不自动回退', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ state: 'ready' }) })
    await expect(readCodexPlusPlusChallenge(sid, fetcher)).rejects.toThrow()
    fetcher.mockResolvedValue({ ok: false, status: 404 })
    await expect(readCodexPlusPlusChallenge(sid, fetcher)).rejects.toThrow()
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(fetcher.mock.calls.every(([url]) => url.includes('/v2/'))).toBe(true)
  })

  it('探针十秒无响应即取消请求', async () => {
    vi.useFakeTimers()
    try {
      const fetcher = vi.fn((_url: string, options: RequestInit) => new Promise((_resolve, reject) => {
        options.signal?.addEventListener('abort', () => reject(new Error('aborted')))
      })) as unknown as typeof fetch
      const result = readCodexPlusPlusChallenge(sid, fetcher)
      const rejected = expect(result).rejects.toThrow('aborted')
      await vi.advanceTimersByTimeAsync(10_000)
      await rejected
    } finally {
      vi.useRealTimers()
    }
  })

  it('取消只发送空的 POST 且不带凭据', async () => {
    const fetcher = vi.fn().mockResolvedValue({ ok: true })
    await cancelCodexPlusPlusSession(sid, fetcher)
    expect(fetcher).toHaveBeenCalledWith(
      `http://127.0.0.1:49371/v2/import/sessions/${sid}/cancel`,
      expect.objectContaining({ method: 'POST', credentials: 'omit', body: undefined })
    )
    expect(JSON.stringify(fetcher.mock.calls)).not.toContain(openaiKey.key)
  })
})
