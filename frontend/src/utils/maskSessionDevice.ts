// 会话设备展示脱敏：仅输出白名单字段（浏览器/操作系统标签），不透出原始 UA 全文或完整 IP。

const BROWSER_PATTERNS: Array<[RegExp, string]> = [
  [/edg\//i, 'Edge'],
  [/chrome\//i, 'Chrome'],
  [/firefox\//i, 'Firefox'],
  [/safari\//i, 'Safari'],
]

const OS_PATTERNS: Array<[RegExp, string]> = [
  [/windows/i, 'Windows'],
  [/mac ?os|macintosh/i, 'macOS'],
  [/android/i, 'Android'],
  [/iphone|ipad|ios/i, 'iOS'],
  [/linux/i, 'Linux'],
]

function matchFirst(source: string, patterns: Array<[RegExp, string]>, fallback: string): string {
  for (const [pattern, label] of patterns) {
    if (pattern.test(source)) return label
  }
  return fallback
}

/** 从原始 User-Agent 中提取白名单浏览器/系统标签，不回显原始字符串。 */
export function describeUserAgent(userAgent: string | undefined | null): string {
  const source = (userAgent ?? '').trim()
  if (!source) return '未知设备'
  const browser = matchFirst(source, BROWSER_PATTERNS, '未知浏览器')
  const os = matchFirst(source, OS_PATTERNS, '未知系统')
  return `${browser} · ${os}`
}

/** 掩码 IP 地址：IPv4 仅保留首段，IPv6 仅保留首个分组，其余替换为 ***。 */
export function maskIpAddress(ip: string | undefined | null): string {
  const source = (ip ?? '').trim()
  if (!source) return '来源不可用'
  if (source.includes('.')) {
    const parts = source.split('.')
    if (parts.length === 4) return `${parts[0]}.***.***.**`
  }
  if (source.includes(':')) {
    const parts = source.split(':').filter(Boolean)
    if (parts.length > 0) return `${parts[0]}:****:****`
  }
  return '***'
}
