export function parseTopupAmountInput(value: string): number | null {
  const trimmed = value.trim()
  if (trimmed === '') return 0
  if (!/^\d+$/.test(trimmed)) return null
  const amount = Number(trimmed)
  if (!Number.isSafeInteger(amount) || amount < 0) return null
  return amount
}
