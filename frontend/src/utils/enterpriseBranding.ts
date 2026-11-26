export const PAINUOYUN_ENTERPRISE_NAME = '派诺云'

const PAINUOYUN_HOST_SUFFIX = '.ainoriahub.com'

export function isPainuoyunEnterpriseHost(hostname: string): boolean {
  const host = hostname.trim().toLowerCase().replace(/\.$/, '')
  return host.length > PAINUOYUN_HOST_SUFFIX.length && host.endsWith(PAINUOYUN_HOST_SUFFIX)
}

export function isPainuoyunEnterprisePortal(hostname: string, pathname: string): boolean {
  return isPainuoyunEnterpriseHost(hostname)
    && (pathname === '/enterprise' || pathname.startsWith('/enterprise/'))
}

export function replaceLegacyEnterpriseBrand(value: string, enabled: boolean): string {
  return enabled ? value.replace(/sub2api/gi, PAINUOYUN_ENTERPRISE_NAME) : value
}
