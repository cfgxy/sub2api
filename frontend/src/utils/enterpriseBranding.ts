export const PAINUOYUN_ENTERPRISE_NAME = '派诺云'

export function isPainuoyunEnterprisePortal(pathname: string): boolean {
  return pathname === '/enterprise' || pathname.startsWith('/enterprise/')
}
