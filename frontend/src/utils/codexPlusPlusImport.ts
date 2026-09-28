import type { ApiKey } from '@/types'

export interface CodexPlusPlusCandidate {
  name: string
  baseUrl: string
  wireApi: 'responses'
  relayMode: 'pureApi'
  model?: string
}

export function resolveCodexPlusPlusCandidate(
  key: ApiKey,
  baseUrl: string,
  verifiedModels: readonly string[] = [],
  selectedModel = ''
): CodexPlusPlusCandidate | null {
  const group = key.group
  if (key.status !== 'active' || !key.key.trim() || /\*|•|…|\.\.\./.test(key.key) ||
      (key.quota > 0 && key.quota_used >= key.quota) ||
      (key.expires_at && !(Date.parse(key.expires_at) > Date.now())) ||
      group?.status !== 'active' || group.platform !== 'openai') return null

  try {
    const url = new URL(baseUrl)
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) return null
    const path = url.pathname.replace(/\/+$/, '')
    url.pathname = path.endsWith('/v1') ? path : `${path}/v1`
    const model = verifiedModels.includes(selectedModel) ? selectedModel.trim() : ''
    return {
      name: `Sub2API / OpenAI / key #${key.id}`,
      baseUrl: url.toString().replace(/\/$/, ''),
      wireApi: 'responses',
      relayMode: 'pureApi',
      ...(model ? { model } : {})
    }
  } catch {
    return null
  }
}

export function buildCodexPlusPlusImportUri(candidate: CodexPlusPlusCandidate, apiKey: string): string {
  if (!apiKey.trim() || /\*|•|…|\.\.\./.test(apiKey)) throw new Error('无效的密钥')
  const params = new URLSearchParams({
    resource: 'provider',
    name: candidate.name,
    baseUrl: candidate.baseUrl,
    apiKey,
    wireApi: candidate.wireApi,
    relayMode: candidate.relayMode
  })
  if (candidate.model) params.set('model', candidate.model)
  return `codexplusplus://v1/import/provider?${params}`
}
