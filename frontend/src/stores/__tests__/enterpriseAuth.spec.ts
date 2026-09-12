import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { AxiosError, type InternalAxiosRequestConfig } from 'axios'

vi.mock('@/api/enterprise', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/enterprise')>()
  return {
    ...actual,
    enterpriseAPI: {
      ...actual.enterpriseAPI,
      login: vi.fn(),
      refresh: vi.fn(),
      logout: vi.fn(),
    },
  }
})

import { enterpriseAPI, enterpriseClient } from '@/api/enterprise'
import { useEnterpriseAuthStore } from '@/stores/enterpriseAuth'

describe('enterprise auth store isolation', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('persists only enterprise-prefixed identity data', async () => {
    localStorage.setItem('auth_token', 'platform-token')
    localStorage.setItem('auth_user', JSON.stringify({ id: 7, role: 'admin' }))
    vi.mocked(enterpriseAPI.login).mockResolvedValue({
      access_token: 'enterprise-access',
      refresh_token: 'enterprise-refresh',
      token_type: 'Bearer',
      expires_in: 900,
      principal: {
        enterprise_id: 12,
        principal_type: 'employee',
        principal_id: 33,
        email: 'employee@example.com',
        role: 'enterprise_employee',
        force_password_change: true,
      },
    })

    const store = useEnterpriseAuthStore()
    await store.login({ email: 'employee@example.com', password: 'temporary-secret' })

    expect(localStorage.getItem('auth_token')).toBe('platform-token')
    expect(localStorage.getItem('enterprise_access_token')).toBe('enterprise-access')
    expect(localStorage.getItem('enterprise_principal')).not.toContain('temporary-secret')
    expect(store.mustChangePassword).toBe(true)
  })

  it('uses the rotated refresh token when logging out after automatic refresh', async () => {
    const principal = {
      enterprise_id: 12,
      principal_type: 'employee' as const,
      principal_id: 33,
      email: 'employee@example.com',
      role: 'enterprise_employee' as const,
      force_password_change: false,
    }
    localStorage.setItem('enterprise_access_token', 'access-r0')
    localStorage.setItem('enterprise_refresh_token', 'refresh-r0')
    localStorage.setItem('enterprise_principal', JSON.stringify(principal))
    const store = useEnterpriseAuthStore()
    store.restore()

    const refreshed = {
      access_token: 'access-r1',
      refresh_token: 'refresh-r1',
      token_type: 'Bearer',
      expires_in: 900,
      principal,
    }
    const post = vi.spyOn(enterpriseClient, 'post').mockResolvedValue({ data: refreshed })
    let attempts = 0
    await enterpriseClient.get('/enterprise/sessions', {
      adapter: async (config) => {
        attempts += 1
        if (attempts === 1) {
          throw new AxiosError('unauthorized', 'ERR_BAD_REQUEST', config as InternalAxiosRequestConfig, undefined, {
            data: { code: 'TOKEN_EXPIRED', message: 'expired' },
            status: 401,
            statusText: 'Unauthorized',
            headers: {},
            config: config as InternalAxiosRequestConfig,
          })
        }
        return { data: { code: 0, data: [] }, status: 200, statusText: 'OK', headers: {}, config }
      },
    })

    expect(store.accessToken).toBe('access-r1')
    expect(localStorage.getItem('enterprise_refresh_token')).toBe('refresh-r1')
    expect(post).toHaveBeenCalledWith('/enterprise/auth/refresh', { refresh_token: 'refresh-r0' })
    await store.logout()
    expect(enterpriseAPI.logout).toHaveBeenCalledWith('refresh-r1')
  })
})
