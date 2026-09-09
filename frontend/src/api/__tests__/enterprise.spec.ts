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
})
