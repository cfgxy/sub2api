export const PAINUOYUN_ENTERPRISE_NAME = '派诺云'
export const PAINUOYUN_ENTERPRISE_FAVICON = '/brand/painuoyun-icon.svg'

export function isPainuoyunEnterprisePortal(pathname: string): boolean {
  return pathname === '/enterprise' || pathname.startsWith('/enterprise/')
}
