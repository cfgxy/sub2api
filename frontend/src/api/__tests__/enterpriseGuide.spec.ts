import { beforeEach, describe, expect, it, vi } from 'vitest'
import { enterpriseClient, fetchEnterpriseGuideModels } from '../enterprise'

describe('enterprise guide model catalogue', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('reads the catalogue from the employee session on every visit', async () => {
    const request = vi.spyOn(enterpriseClient, 'get').mockResolvedValue({
      data: { object: 'list', data: [
        { id: 'model-1', platform: 'openai' },
        { id: ' model-2 ', platform: 'anthropic' },
        { id: '', platform: 'openai' },
        { platform: 'openai' },
      ] },
    })
    for (let visit = 0; visit < 2; visit += 1) {
      const models = await fetchEnterpriseGuideModels()
      expect(models).toEqual([{ id: 'model-1', platform: 'openai' }, { id: 'model-2', platform: 'anthropic' }])
    }
    expect(request).toHaveBeenCalledWith('/enterprise/guide/models', { signal: undefined, enterpriseSuppressUnavailableRedirect: true })
  })

  it('returns an empty list, never a default catalogue, when the server has no usable model', async () => {
    vi.spyOn(enterpriseClient, 'get').mockResolvedValue({ data: { object: 'list', data: [] } })
    await expect(fetchEnterpriseGuideModels()).resolves.toEqual([])
  })

  it('fails closed on malformed payloads and propagates session failures', async () => {
    vi.spyOn(enterpriseClient, 'get').mockResolvedValueOnce({ data: { data: null } })
    await expect(fetchEnterpriseGuideModels()).rejects.toMatchObject({ status: 502 })
    vi.spyOn(enterpriseClient, 'get').mockRejectedValueOnce({ response: { status: 403 } })
    await expect(fetchEnterpriseGuideModels()).rejects.toMatchObject({ response: { status: 403 } })
  })
})
