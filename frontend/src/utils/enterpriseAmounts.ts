/**
 * Keeps enterprise dashboard and quota summaries readable. Usage and billing-detail
 * tables deliberately render the server precision unchanged for auditability.
 */
export function formatEnterpriseAmount(value: string | number | null | undefined): string {
  const amount = Number(value)
  const safeAmount = Number.isFinite(amount) ? amount : 0

  return new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(safeAmount)
}

export function formatEnterpriseCurrency(value: string | number | null | undefined): string {
  return `$${formatEnterpriseAmount(value)}`
}
