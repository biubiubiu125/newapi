const HAN = /\p{Script=Han}/u

type Translate = (key: string) => string

function chineseInterface(language: string): boolean {
  const normalized = language.trim().toLowerCase().replaceAll('_', '-')
  return (
    normalized === 'zh' ||
    normalized === 'zhcn' ||
    normalized === 'zhtw' ||
    normalized.startsWith('zh-')
  )
}

const FOREIGN_ERROR_WORD = new Set([
  'timeout',
  'refused',
  'connection',
  'failed',
  'failure',
  'error',
  'errors',
  'dial',
  'connect',
  'invalid',
  'unexpected',
  'exception',
  'denied',
  'unreachable',
  'reset',
  'closed',
  'eof',
  'broken',
  'pipe',
  'deadline',
  'exceeded',
  'forbidden',
  'unauthorized',
  'internal',
  'upstream',
  'gateway',
])

function isLatinLetter(char: string): boolean {
  return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

function latinWords(text: string): string[] {
  const words: string[] = []
  let run = ''
  const flush = () => {
    if (!run) return
    words.push(run.toLowerCase())
    run = ''
  }
  for (const char of text) {
    if (isLatinLetter(char)) {
      run += char
      continue
    }
    flush()
  }
  flush()
  return words
}

function collapseSpaces(text: string): string {
  return text
    .split(/\s+/)
    .filter((part) => part !== '')
    .join(' ')
}

function dropAsciiNoise(text: string): string {
  let out = ''
  for (const char of text) {
    const code = char.codePointAt(0) ?? 0
    if (isLatinLetter(char) || (char >= '0' && char <= '9')) {
      out += ' '
      continue
    }
    if (code < 128 && char !== ' ' && char !== '\t') continue
    out += char
  }
  return collapseSpaces(out)
}

function dropForeignErrorWords(text: string): string {
  let out = ''
  let run = ''
  const flush = () => {
    if (!run) return
    out += FOREIGN_ERROR_WORD.has(run.toLowerCase()) ? ' ' : run
    run = ''
  }
  for (const char of text) {
    if (isLatinLetter(char)) {
      run += char
      continue
    }
    flush()
    out += char
  }
  flush()
  return collapseSpaces(out)
}

function cleanChineseClause(text: string): string {
  const clause = text.trim()
  if (!clause || !HAN.test(clause)) return ''
  const words = latinWords(clause)
  const longWord = words.some((word) => word.length >= 4)
  if (words.length >= 2 && longWord) return dropAsciiNoise(clause)
  if (words.some((word) => FOREIGN_ERROR_WORD.has(word))) {
    return dropForeignErrorWords(clause)
  }
  return clause
}

export function sanitizeChineseConsoleText(text: string): string {
  const trimmed = text.trim()
  if (!trimmed || !HAN.test(trimmed)) return ''
  return trimmed
    .split(/[:：;；\n\r]+/)
    .map((part) => cleanChineseClause(part))
    .filter((part) => part && HAN.test(part))
    .join('：')
}

export function consoleDetailText(
  value: string,
  language: string,
  t: Translate,
  fallbackKey = 'Unknown error'
): string {
  const text = value.trim()
  if (!text) return '-'
  if (!chineseInterface(language)) return text
  const cleaned = sanitizeChineseConsoleText(text)
  if (!cleaned) return t(fallbackKey)
  return cleaned
}

export function referralErrorLabel(
  value: string,
  language: string,
  t: Translate
): string {
  const normalized = (value || '').trim()
  if (!normalized) return '-'
  switch (normalized) {
    case 'fx_rate_missing':
      return t('Referral exchange rate is missing')
    case 'missing_referral_snapshot':
      return t('Order has no referral snapshot, commission generation skipped')
    case 'zero_commission_amount':
      return t('Commission amount is 0, generation skipped')
    case 'affiliate_not_eligible':
      return t('Affiliate is not approved or settlement is closed')
    case 'unsupported source_type':
      return t('Unsupported order source type')
    case 'trade_no is required':
      return t('Order number is missing')
    case 'failed to update referral pending amount':
      return t('Failed to update affiliate pending balance')
    case 'record not found':
      return t('Related record not found')
    case 'subscription order not found':
      return t('Subscription order not found')
    case 'topup order not found':
      return t('Top-up order not found')
    case 'duplicate_job_superseded_by_subscription':
      return t('Commission was regenerated from the subscription order')
    case 'paid_amount must be a positive finite number':
      return t('Paid amount must be greater than 0')
    case 'affiliate_not_found':
      return t('Affiliate not found')
    case 'affiliate_not_approved':
      return t('Affiliate is not approved')
    case 'affiliate_acquisition_disabled':
      return t('Affiliate acquisition is frozen')
    case 'affiliate_settlement_disabled':
      return t('Affiliate settlement is frozen')
    case 'no_binding':
      return t('Order user has no valid invite binding')
    case 'invalid_rate':
      return t('Referral rate is invalid')
    case 'redemption_commission_chain_incomplete':
      return t('Referral commission chain is incomplete')
    default:
      if (normalized.includes('UNIQUE constraint failed')) {
        return t(
          'Commission record already exists or unique constraint conflict'
        )
      }
      if (normalized.includes('duplicate key value')) {
        return t(
          'Commission record already exists or unique constraint conflict'
        )
      }
      if (normalized.includes('subscription order not found')) {
        return t('Subscription order not found')
      }
      if (normalized.includes('topup order not found')) {
        return t('Top-up order not found')
      }
      if (normalized.includes('record not found')) {
        return t('Related record not found')
      }
      return consoleDetailText(normalized, language, t)
  }
}
