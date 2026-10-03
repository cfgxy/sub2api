import { apiClient } from './client'

export async function getModelPricingContent(signal?: AbortSignal): Promise<string> {
  const { data } = await apiClient.get<string>('/model-pricing/content', { signal })
  return data
}
