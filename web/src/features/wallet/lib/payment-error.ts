import { currentConsoleFailureText } from '@/lib/console-failure-text'

export function paymentFailureText(response: unknown): string {
  const payload =
    response && typeof response === 'object'
      ? (response as { message?: unknown; data?: unknown })
      : undefined
  const rawMessage =
    typeof payload?.message === 'string' ? payload.message.trim() : ''
  const message =
    rawMessage === 'error' || rawMessage === 'success' ? '' : rawMessage
  const dataMessage = typeof payload?.data === 'string' ? payload.data.trim() : ''
  return currentConsoleFailureText(
    message || dataMessage,
    'Payment request failed'
  )
}
