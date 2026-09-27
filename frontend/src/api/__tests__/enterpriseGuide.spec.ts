import { beforeEach, describe, expect, it, vi } from 'vitest'
import { enterpriseClient, fetchEnterpriseGuideModels } from '../enterprise'

describe('enterprise session guide catalogue', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    vi.restoreAllMocks()
  })

  it('uses the employee session without a one-time API key on every visit', async () => {
    const request = vi.spyOn(enterpriseClient, 'get').mockResolvedValue({
      data: { object: 'list', data: [
        { id: 'enterprise-model-1', platform: 'openai', display_name: 'Model 1' },
        { id: 'enterprise-model-2', platform: 'anthropic', description: 'Model 2' },
      ] },
    })

    for (let visit = 0; visit < 2; visit += 1) {
      const models = await fetchEnterpriseGuideModels()
      expect(models.map((model) => model.id)).toEqual(['enterprise-model-1', 'enterprise-model-2'])
      expect(models.map((model) => model.platform)).toEqual(['openai', 'anthropic'])
    }
    expect(request).toHaveBeenCalledTimes(2)
    expect(request).toHaveBeenCalledWith('/enterprise/guide/models', { signal: undefined, enterpriseSuppressUnavailableRedirect: true })
  })

  it('fails closed when the session catalogue is malformed', async () => {
    vi.spyOn(enterpriseClient, 'get').mockResolvedValue({ data: { data: null } })
    await expect(fetchEnterpriseGuideModels()).rejects.toMatchObject({ status: 502 })
  })

  it('propagates session access failures without inventing a model list', async () => {
    vi.spyOn(enterpriseClient, 'get').mockRejectedValue({ response: { status: 403 } })
    await expect(fetchEnterpriseGuideModels()).rejects.toMatchObject({ response: { status: 403 } })
  })
})
