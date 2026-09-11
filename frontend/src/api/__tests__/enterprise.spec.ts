import { beforeEach, describe, expect, it, vi } from 'vitest'
import { enterpriseAPI, enterpriseClient } from '@/api/enterprise'

describe('enterprise API password handling', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
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
})
