import { buildCcSwitchImportDeeplink } from '@/utils/ccswitchImport'

export type EnterpriseImportPlatform = 'openai' | 'anthropic'

export interface EnterpriseImportAddress {
  root: string
  openai: string
}

interface EnterpriseImportInput {
  address: EnterpriseImportAddress
  key: string
  platform: EnterpriseImportPlatform
  model?: string
  name?: string
}

export function enterpriseImportBaseUrls(configured: string): EnterpriseImportAddress {
  const url = new URL(configured.trim())
  const loopback = ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)
  if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback)) ||
      url.username || url.password || url.search || url.hash) {
    throw new Error('无效的部署接入地址')
  }
  const path = url.pathname.replace(/\/+$/, '').replace(/\/v1$/, '')
  url.pathname = path || '/'
  const root = url.toString().replace(/\/+$/, '')
  return { root, openai: `${root}/v1` }
}

function requireFullKey(key: string): string {
  const value = key.trim()
  if (!value || /\*|•|…|\.\.\./.test(value)) throw new Error('密钥不可用于导入')
  return value
}

export function buildEnterpriseCodexImportUri(input: EnterpriseImportInput): string {
  const fields: Record<string, string> = {
    resource: 'provider',
    name: input.name || 'Sub2API enterprise',
    baseUrl: input.platform === 'openai' ? input.address.openai : input.address.root,
    apiKey: requireFullKey(input.key),
    wireApi: input.platform === 'openai' ? 'responses' : 'messages',
    relayMode: 'pureApi',
  }
  if (input.model?.trim()) fields.model = input.model.trim()
  return `codexplusplus://v1/import/provider?${new URLSearchParams(fields)}`
}

export function buildEnterpriseCcsImportUri(input: EnterpriseImportInput): string {
  const usageScript = `({
    request: {
      url: "{{baseUrl}}/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return { isValid: response?.is_active ?? response?.isValid ?? true, remaining, unit };
    }
  })`
  const uri = new URL(buildCcSwitchImportDeeplink({
    baseUrl: input.address.root,
    platform: input.platform,
    clientType: 'claude',
    providerName: input.name || 'Sub2API enterprise',
    apiKey: requireFullKey(input.key),
    usageScript,
  }))
  if (input.model?.trim()) uri.searchParams.set('model', input.model.trim())
  return uri.toString()
}
