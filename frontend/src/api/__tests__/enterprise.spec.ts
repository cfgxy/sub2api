import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearEnterpriseKeyMutationRetryState, enterpriseAPI, enterpriseClient, syncEnterpriseAuthSession } from '@/api/enterprise'

const employeePrincipal = {
  enterprise_id: 12,
  principal_type: 'employee' as const,
  principal_id: 33,
  email: 'employee@example.com',
  role: 'enterprise_employee' as const,
  force_password_change: false,
}

function setEmployeePrincipal(principal = employeePrincipal) {
  localStorage.setItem('enterprise_principal', JSON.stringify(principal))
}

describe('enterprise API password handling', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    clearEnterpriseKeyMutationRetryState()
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends an initial password without persisting or returning it', async () => {
    const post = vi.spyOn(enterpriseClient, 'post').mockResolvedValue({
      data: { id: 9, email: 'new@example.com', status: 'active', must_change_password: true },
    })

    const employee = await enterpriseAPI.createEmployee({
      email: 'new@example.com',
      initial_password: 'one-time-password',
      department_id: null,
    })

    expect(post).toHaveBeenCalledWith('/enterprise/admin/employees', {
      email: 'new@example.com',
      initial_password: 'one-time-password',
      department_id: null,
    })
    expect(employee).not.toHaveProperty('initial_password')
    expect(JSON.stringify(localStorage)).not.toContain('one-time-password')
  })

  it('uploads brand background bytes with multipart form data', async () => {
    const post = vi.spyOn(enterpriseClient, 'post').mockResolvedValue({
      data: {
        background_url: '/api/v1/enterprise/brand/background',
        background_content_type: 'image/png',
        background_sha256: 'a'.repeat(64),
        background_size_bytes: 4,
      },
    })
    const file = new File([new Uint8Array([1, 2, 3, 4])], 'background.png', { type: 'image/png' })

    await enterpriseAPI.uploadBrandBackground(file)

    expect(post).toHaveBeenCalledOnce()
    expect(post.mock.calls[0]?.[0]).toBe('/enterprise/admin/brand/background')
    expect(post.mock.calls[0]?.[1]).toBeInstanceOf(FormData)
    expect((post.mock.calls[0]?.[1] as FormData).get('file')).toBe(file)
    expect((post.mock.calls[0]?.[1] as FormData).get('sha256')).toBe('9f64a747e1b97f131fabb6b447296c9b6f0201e79fb3c5356e6c77e89b6a806a')
  })

  it('updates editable brand fields without submitting server-owned background metadata', async () => {
    const put = vi.spyOn(enterpriseClient, 'put').mockResolvedValue({ data: {} })

    await enterpriseAPI.updateBrand({
      enterprise_name: 'Example Enterprise',
      title: 'Workspace',
      slogan: 'Build together',
      body: 'Secure access for the team.',
      background_url: '/api/v1/enterprise/brand/background',
    })

    expect(put).toHaveBeenCalledWith('/enterprise/admin/brand', {
      enterprise_name: 'Example Enterprise',
      title: 'Workspace',
      slogan: 'Build together',
      body: 'Secure access for the team.',
      background_url: '/api/v1/enterprise/brand/background',
    })
  })

  it('uses server-derived employee identity and distinct operation keys for successful key mutations', async () => {
    setEmployeePrincipal()
    const randomUUID = vi.spyOn(crypto, 'randomUUID')
      .mockReturnValueOnce('11111111-1111-4111-8111-111111111111')
      .mockReturnValueOnce('22222222-2222-4222-8222-222222222222')
      .mockReturnValueOnce('33333333-3333-4333-8333-333333333333')
    const post = vi.spyOn(enterpriseClient, 'post').mockResolvedValue({ data: { key: { id: 9 }, replayed: false } })

    await enterpriseAPI.createKey()
    await enterpriseAPI.rotateKey(9)
    await enterpriseAPI.disableKey(9)

    expect(post).toHaveBeenNthCalledWith(1, '/enterprise/keys', undefined, {
      headers: { 'Idempotency-Key': '11111111-1111-4111-8111-111111111111' },
    })
    expect(post).toHaveBeenNthCalledWith(2, '/enterprise/keys/rotate', { expected_api_key_id: 9 }, {
      headers: { 'Idempotency-Key': '22222222-2222-4222-8222-222222222222' },
    })
    expect(post).toHaveBeenNthCalledWith(3, '/enterprise/keys/disable', { expected_api_key_id: 9 }, {
      headers: { 'Idempotency-Key': '33333333-3333-4333-8333-333333333333' },
    })
    expect(post.mock.calls.flat().join(' ')).not.toContain('enterprise_id')
    expect(post.mock.calls.flat().join(' ')).not.toContain('employee_id')
    expect(randomUUID).toHaveBeenCalledTimes(3)
    expect(sessionStorage.length).toBe(0)
  })

  it('falls back to a generated idempotency key when Web Crypto UUID is unavailable', async () => {
    setEmployeePrincipal()
    vi.stubGlobal('crypto', {})
    const post = vi.spyOn(enterpriseClient, 'post').mockResolvedValue({ data: { key: { id: 9 }, replayed: false } })

    await enterpriseAPI.createKey()

    expect(post).toHaveBeenCalledWith('/enterprise/keys', undefined, {
      headers: { 'Idempotency-Key': expect.stringMatching(/^\d+-[a-z0-9]+$/) },
    })
  })

  it('reuses the same create key after an ambiguous failure and clears it after a response', async () => {
    setEmployeePrincipal()
    const post = vi.spyOn(enterpriseClient, 'post')
      .mockRejectedValueOnce(new Error('network timeout'))
      .mockResolvedValueOnce({ data: { key: { id: 9 }, replayed: true } })

    await expect(enterpriseAPI.createKey()).rejects.toThrow('network timeout')
    const firstHeaders = post.mock.calls[0]?.[2]?.headers
    expect(sessionStorage.getItem('sub2api:enterprise:key-mutation:12:33:create:0')).toBe(firstHeaders?.['Idempotency-Key'])

    await enterpriseAPI.createKey()

    expect(post.mock.calls[1]?.[2]?.headers).toEqual(firstHeaders)
    expect(sessionStorage.length).toBe(0)
  })

  it('does not reuse a pending key across enterprise employees or target keys', async () => {
    setEmployeePrincipal()
    const post = vi.spyOn(enterpriseClient, 'post')
      .mockRejectedValueOnce(new Error('network timeout'))
      .mockResolvedValueOnce({ data: { key: { id: 10 }, replayed: false } })
      .mockResolvedValueOnce({ data: { key: { id: 11 }, replayed: false } })

    await expect(enterpriseAPI.rotateKey(9)).rejects.toThrow('network timeout')
    const firstHeaders = post.mock.calls[0]?.[2]?.headers
    setEmployeePrincipal({ ...employeePrincipal, enterprise_id: 13, principal_id: 34 })
    await enterpriseAPI.rotateKey(9)
    await enterpriseAPI.rotateKey(10)

    expect(post.mock.calls[1]?.[2]?.headers).not.toEqual(firstHeaders)
    expect(post.mock.calls[2]?.[2]?.headers).not.toEqual(post.mock.calls[1]?.[2]?.headers)
    expect(sessionStorage.getItem('sub2api:enterprise:key-mutation:12:33:rotate:9')).toBe(firstHeaders?.['Idempotency-Key'])
  })

  it('clears pending key retries when the authenticated principal changes', async () => {
    setEmployeePrincipal()
    vi.spyOn(enterpriseClient, 'post').mockRejectedValueOnce(new Error('network timeout'))
    await expect(enterpriseAPI.disableKey(9)).rejects.toThrow('network timeout')
    expect(sessionStorage.length).toBe(1)

    syncEnterpriseAuthSession({
      access_token: 'access', refresh_token: 'refresh', token_type: 'Bearer', expires_in: 900,
      principal: { ...employeePrincipal, enterprise_id: 13, principal_id: 34 },
    })

    expect(sessionStorage.length).toBe(0)
  })

  it('clears a stale operation key after a definite lifecycle conflict', async () => {
    setEmployeePrincipal()
    const post = vi.spyOn(enterpriseClient, 'post')
      .mockRejectedValueOnce({ status: 409, code: 409, reason: 'ENTERPRISE_KEY_VERSION_CONFLICT', message: 'changed' })
      .mockResolvedValueOnce({ data: { key: { id: 10 }, replayed: false } })

    await expect(enterpriseAPI.rotateKey(9)).rejects.toMatchObject({ reason: 'ENTERPRISE_KEY_VERSION_CONFLICT' })
    const staleHeaders = post.mock.calls[0]?.[2]?.headers
    expect(sessionStorage.length).toBe(0)

    await enterpriseAPI.rotateKey(9)

    expect(post.mock.calls[1]?.[2]?.headers).not.toEqual(staleHeaders)
    expect(sessionStorage.length).toBe(0)
  })

  it('does not persist or send a mutation key without a validated employee principal', async () => {
    const post = vi.spyOn(enterpriseClient, 'post')

    await expect(enterpriseAPI.createKey()).rejects.toThrow('authenticated enterprise employee')

    expect(post).not.toHaveBeenCalled()
    expect(sessionStorage.length).toBe(0)
  })
})
