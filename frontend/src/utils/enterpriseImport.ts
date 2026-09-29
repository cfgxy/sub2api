import { buildCcSwitchImportDeeplink } from '@/utils/ccswitchImport'
import { buildCodexPlusPlusImportUri, type CodexPlusPlusCandidate } from '@/utils/codexPlusPlusImport'

export type EnterpriseGuideProtocol = 'openai' | 'anthropic'
export type EnterpriseGuideExampleKind = 'curl' | 'python' | 'javascript'

export interface EnterpriseImportAddress {
  root: string
  openai: string
}

export interface EnterpriseImportModel {
  id: string
  platform: string
}

export const ENTERPRISE_KEY_PLACEHOLDER = '{{API_KEY}}'

const MASKED_KEY = /\*|•|…|\.\.\./

export function enterpriseImportBaseUrls(configured: string): EnterpriseImportAddress {
  const url = new URL(configured.trim())
  const loopback = ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)
  if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback)) ||
      url.username || url.password || url.search || url.hash) {
    throw new Error('invalid deployment address')
  }
  const path = url.pathname.replace(/\/+$/, '').replace(/\/v1$/, '')
  url.pathname = path || '/'
  const root = url.toString().replace(/\/+$/, '')
  return { root, openai: `${root}/v1` }
}

export const enterpriseGuideProtocol = (platform: string): EnterpriseGuideProtocol =>
  platform === 'anthropic' ? 'anthropic' : 'openai'

function requireFullKey(key: string): string {
  const value = key.trim()
  if (!value || MASKED_KEY.test(value)) throw new Error('key unavailable for import')
  return value
}

export function resolveEnterpriseCodexCandidate(
  address: EnterpriseImportAddress,
  model: EnterpriseImportModel | null,
  name = 'Sub2API / OpenAI / enterprise',
): CodexPlusPlusCandidate | null {
  if (!model?.id.trim() || model.platform !== 'openai') return null
  return { name, baseUrl: address.openai, wireApi: 'responses', relayMode: 'pureApi', model: model.id.trim() }
}

export function buildEnterpriseCodexImportUri(candidate: CodexPlusPlusCandidate, key: string): string {
  return buildCodexPlusPlusImportUri(candidate, requireFullKey(key))
}

export const canImportEnterpriseCcs = (model: EnterpriseImportModel | null): boolean =>
  !!model?.id.trim() && (model.platform === 'openai' || model.platform === 'anthropic')

const CCS_USAGE_SCRIPT = `({
    request: {
      url: "{{baseUrl}}/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`

export function buildEnterpriseCcsImportUri(
  address: EnterpriseImportAddress,
  model: EnterpriseImportModel,
  key: string,
  providerName = 'Sub2API',
): string {
  if (!canImportEnterpriseCcs(model)) throw new Error('model not supported for import')
  const uri = new URL(buildCcSwitchImportDeeplink({
    baseUrl: address.root,
    platform: model.platform === 'openai' ? 'openai' : 'anthropic',
    clientType: 'claude',
    providerName,
    apiKey: requireFullKey(key),
    usageScript: CCS_USAGE_SCRIPT,
  }))
  uri.searchParams.set('model', model.id.trim())
  return uri.toString()
}

const shellQuote = (value: string) => `'${value.replace(/'/g, "'\\''")}'`

export function buildEnterpriseGuideExample(
  kind: EnterpriseGuideExampleKind,
  address: EnterpriseImportAddress,
  model: EnterpriseImportModel,
): string {
  const anthropic = enterpriseGuideProtocol(model.platform) === 'anthropic'
  const endpoint = anthropic ? `${address.root}/v1/messages` : `${address.openai}/chat/completions`
  const modelId = JSON.stringify(model.id)
  const messages = `[{"role":"user","content":"Hello"}]`
  const body = anthropic
    ? `{"model":${modelId},"max_tokens":1024,"messages":${messages}}`
    : `{"model":${modelId},"messages":${messages}}`
  const headers: Array<[string, string]> = anthropic
    ? [['x-api-key', ENTERPRISE_KEY_PLACEHOLDER], ['anthropic-version', '2023-06-01']]
    : [['Authorization', `Bearer ${ENTERPRISE_KEY_PLACEHOLDER}`]]
  headers.unshift(['Content-Type', 'application/json'])

  if (kind === 'python') {
    const headerLines = headers.map(([name, value]) => `        ${JSON.stringify(name)}: ${JSON.stringify(value)},`).join('\n')
    return `import json\nfrom urllib.request import Request, urlopen\n\nrequest = Request(\n    ${JSON.stringify(endpoint)},\n    data=json.dumps(${body.replace(/"/g, "'")}).encode('utf-8'),\n    headers={\n${headerLines}\n    },\n    method='POST',\n)\nwith urlopen(request, timeout=30) as response:\n    print(json.load(response))`
  }
  if (kind === 'javascript') {
    const headerLines = headers.map(([name, value]) => `    ${JSON.stringify(name)}: ${JSON.stringify(value)},`).join('\n')
    return `const response = await fetch(${JSON.stringify(endpoint)}, {\n  method: 'POST',\n  headers: {\n${headerLines}\n  },\n  body: JSON.stringify(${body}),\n});\nif (!response.ok) throw new Error(\`HTTP \${response.status}\`);\nconsole.log(await response.json());`
  }
  const headerLines = headers.map(([name, value]) => `  -H ${shellQuote(`${name}: ${value}`)} \\`).join('\n')
  return `curl ${shellQuote(endpoint)} \\\n${headerLines}\n  -d ${shellQuote(body)}`
}
