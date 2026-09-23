const HAN = /\p{Script=Han}/u

type Translate = (
  key: string,
  options?: Record<string, string | number>
) => string

function chineseInterface(language: string): boolean {
  const normalized = language.trim().toLowerCase().replace(/_/g, '-')
  return (
    normalized === 'zh' ||
    normalized === 'zhcn' ||
    normalized === 'zhtw' ||
    normalized.startsWith('zh-')
  )
}

function withHanDetail(summary: string, detail: string): string {
  const rest = detail.trim()
  if (!rest || !HAN.test(rest)) return summary
  return `${summary}：${rest}`
}

function prefixRest(text: string, prefix: string): string | undefined {
  if (!text.toLowerCase().startsWith(prefix.toLowerCase())) return undefined
  return text.slice(prefix.length)
}

export function storedTaskErrorText(
  value: string,
  language: string,
  t: Translate,
  fallbackKey = ''
): string {
  const text = value.trim()
  if (!text) return ''
  if (!chineseInterface(language)) return text

  const checks = text.match(/^failed channel checks: (\d+)$/i)
  if (checks) {
    return t('Failed channel checks: {{count}}', { count: checks[1] })
  }
  const updates = text.match(/^failed channel updates: (\d+)$/i)
  if (updates) {
    return t('Failed channel updates: {{count}}', { count: updates[1] })
  }

  const cacheRefresh = prefixRest(text, 'runtime channel cache refresh failed:')
  if (cacheRefresh !== undefined) {
    return withHanDetail(t('Runtime channel cache refresh failed'), cacheRefresh)
  }
  const batchRefresh = prefixRest(
    text,
    'batch apply persisted but runtime cache refresh failed:'
  )
  if (batchRefresh !== undefined) {
    return withHanDetail(
      t('Batch apply was saved, but the runtime cache refresh failed'),
      batchRefresh
    )
  }
  const persist = prefixRest(text, 'failed to persist task terminal result:')
  if (persist !== undefined) {
    return withHanDetail(t('Failed to save the task result'), persist)
  }

  const lower = text.toLowerCase()
  if (
    lower === 'context canceled' ||
    lower === 'context cancelled' ||
    lower === 'task cancelled by user' ||
    lower === 'task canceled by user'
  ) {
    return t('Task was cancelled')
  }
  if (lower === 'context deadline exceeded') return t('Task timed out')
  if (HAN.test(text)) return text
  return fallbackKey ? t(fallbackKey) : ''
}

export function paymentOrphanReasonText(
  value: string,
  language: string,
  t: Translate
): string {
  const text = value.trim()
  if (!text) return ''
  if (!chineseInterface(language)) return text

  const localOrder = prefixRest(text, 'local order insert failed:')
  if (localOrder !== undefined) {
    return withHanDetail(t('Local order insert failed'), localOrder)
  }

  switch (text) {
    case 'BEpusdt subscription payment requires manual review after payment succeeded':
      return t(
        'BEpusdt subscription payment requires manual review after payment succeeded'
      )
    case 'Epay subscription payment requires manual review after payment succeeded':
      return t(
        'Epay subscription payment requires manual review after payment succeeded'
      )
    case 'Epay top-up payment requires manual review after payment succeeded':
      return t(
        'Epay top-up payment requires manual review after payment succeeded'
      )
    case 'BEpusdt top-up payment requires manual review after payment succeeded':
      return t(
        'BEpusdt top-up payment requires manual review after payment succeeded'
      )
    case 'Creem payment succeeded but local reference_id is missing':
      return t('Creem payment succeeded but local reference_id is missing')
    case 'Creem payment facts are invalid after payment succeeded':
      return t('Creem payment facts are invalid after payment succeeded')
    case 'Creem subscription payment requires manual review after payment succeeded':
      return t(
        'Creem subscription payment requires manual review after payment succeeded'
      )
    case 'Creem payment succeeded but no matching subscription order exists; requires manual review after payment succeeded':
      return t(
        'Creem payment succeeded but no matching subscription order exists; requires manual review after payment succeeded'
      )
    case 'Creem payment succeeded but the local top-up order is missing; requires manual review after payment succeeded':
      return t(
        'Creem payment succeeded but the local top-up order is missing; requires manual review after payment succeeded'
      )
    case 'Creem top-up payment requires manual review after payment succeeded':
      return t(
        'Creem top-up payment requires manual review after payment succeeded'
      )
    case 'Stripe payment facts are invalid after payment succeeded':
      return t('Stripe payment facts are invalid after payment succeeded')
    case 'Stripe subscription payment requires manual review after payment succeeded':
      return t(
        'Stripe subscription payment requires manual review after payment succeeded'
      )
    case 'subscription purchase limit reached after payment succeeded':
      return t('subscription purchase limit reached after payment succeeded')
    case 'Stripe subscription payment succeeded but payment facts do not match the order':
      return t(
        'Stripe subscription payment succeeded but payment facts do not match the order'
      )
    case 'local order not found after stripe payment succeeded':
      return t('local order not found after stripe payment succeeded')
    case 'Stripe top-up payment succeeded but payment facts do not match the order':
      return t(
        'Stripe top-up payment succeeded but payment facts do not match the order'
      )
    case 'Stripe top-up payment requires manual review after payment succeeded':
      return t(
        'Stripe top-up payment requires manual review after payment succeeded'
      )
    case 'Waffo top-up payment requires manual review after payment succeeded':
      return t(
        'Waffo top-up payment requires manual review after payment succeeded'
      )
    case 'Waffo Pancake payment succeeded but the local order could not be resolved; requires manual review after payment succeeded':
      return t(
        'Waffo Pancake payment succeeded but the local order could not be resolved; requires manual review after payment succeeded'
      )
    case 'Waffo Pancake subscription payment requires manual review after payment succeeded':
      return t(
        'Waffo Pancake subscription payment requires manual review after payment succeeded'
      )
    case 'Waffo Pancake top-up payment requires manual review after payment succeeded':
      return t(
        'Waffo Pancake top-up payment requires manual review after payment succeeded'
      )
    case 'Waffo Pancake webhook environment does not match the receiving endpoint':
      return t(
        'Waffo Pancake webhook environment does not match the receiving endpoint'
      )
    case 'Waffo Pancake webhook store does not match the configured merchant store':
      return t(
        'Waffo Pancake webhook store does not match the configured merchant store'
      )
    case 'Waffo Pancake top-up payment succeeded but the local order could not be resolved; requires manual review after payment succeeded':
      return t(
        'Waffo Pancake top-up payment succeeded but the local order could not be resolved; requires manual review after payment succeeded'
      )
    default:
      return HAN.test(text) ? text : t('Payment review required')
  }
}

export function paymentOrphanErrorText(
  value: string,
  language: string,
  t: Translate
): string {
  const text = value.trim()
  if (!text) return ''
  if (!chineseInterface(language)) return text
  return HAN.test(text) ? text : ''
}
