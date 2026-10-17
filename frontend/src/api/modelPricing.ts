import { apiClient } from './client'

export async function getModelPricing(signal?: AbortSignal): Promise<string> {
  const { data } = await apiClient.get<{ html: string }>('/model-pricing', { signal })
  return data.html
}
