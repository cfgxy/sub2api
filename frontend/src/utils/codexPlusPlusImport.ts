import type { ApiKey } from '@/types'

const LOOPBACK_ORIGIN = 'http://127.0.0.1:49371'
const SID_PATTERN = /^[0-9a-f]{32}$/

export interface CodexPlusPlusCandidate {
  name: string
  baseUrl: string
  platform: 'openai'
  model: string
}

function sessionUrl(sid: string): string {
  if (!SID_PATTERN.test(sid)) throw new Error('无效的导入会话')
  return `${LOOPBACK_ORIGIN}/v2/import/sessions/${sid}`
}

export function createCodexPlusPlusSessionId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

export function buildCodexPlusPlusImportUri(sid: string, origin: string): string {
  sessionUrl(sid)
  const source = new URL(origin)
  const isLocal = source.hostname === 'localhost' || source.hostname === '127.0.0.1'
  if (source.origin !== origin || source.username || source.password ||
      (source.protocol !== 'https:' && !(isLocal && source.protocol === 'http:'))) {
    throw new Error('无效的页面来源')
  }
  return `codexplusplus://v2/import/provider?${new URLSearchParams({ sid, origin })}`
}

export function resolveCodexPlusPlusCandidate(
  key: ApiKey,
  baseUrl: string,
  verifiedModels: readonly string[] = [],
  selectedModel = ''
): CodexPlusPlusCandidate | null {
  const group = key.group
  const model = verifiedModels.includes(selectedModel) ? selectedModel.trim() : ''
  if (key.status !== 'active' || !key.key.trim() || /\*|•|…|\.\.\./.test(key.key) ||
      (key.quota > 0 && key.quota_used >= key.quota) ||
      (key.expires_at && !(Date.parse(key.expires_at) > Date.now())) ||
      group?.status !== 'active' || group.platform !== 'openai' || !model) return null

  try {
    const url = new URL(baseUrl)
    if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) return null
    const path = url.pathname.replace(/\/+$/, '')
    url.pathname = path.endsWith('/v1') ? path : `${path}/v1`
    return {
      name: `Sub2API / OpenAI / key #${key.id}`,
      baseUrl: url.toString().replace(/\/$/, ''),
      platform: 'openai',
      model
    }
  } catch {
    return null
  }
}

export async function readCodexPlusPlusChallenge(
  sid: string,
  fetcher: typeof fetch = fetch
): Promise<string> {
  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 10_000)
  try {
    const response = await fetcher(sessionUrl(sid), {
      method: 'GET',
      credentials: 'omit',
      cache: 'no-store',
      redirect: 'error',
      referrerPolicy: 'no-referrer',
      signal: controller.signal
    })
    if (!response.ok) throw new Error('本机连接未建立')
    const payload: unknown = await response.json()
    if (typeof payload !== 'object' || payload === null ||
        (payload as { state?: unknown }).state !== 'ready' ||
        typeof (payload as { challenge?: unknown }).challenge !== 'string' ||
        !/^[A-Za-z0-9_-]{16,128}$/.test((payload as { challenge: string }).challenge)) {
      throw new Error('本机连接未建立')
    }
    return (payload as { challenge: string }).challenge
  } finally {
    clearTimeout(timeout)
  }
}

export async function cancelCodexPlusPlusSession(sid: string, fetcher: typeof fetch = fetch): Promise<void> {
  await fetcher(`${sessionUrl(sid)}/cancel`, {
    method: 'POST',
    credentials: 'omit',
    cache: 'no-store',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
    body: undefined
  })
}
