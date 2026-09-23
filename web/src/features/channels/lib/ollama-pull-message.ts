const HAN = /\p{Script=Han}/u

type Translate = (key: string) => string

function chineseInterface(language: string): boolean {
  const normalized = language.trim().toLowerCase().replace(/_/g, '-')
  return (
    normalized === 'zh' ||
    normalized === 'zhcn' ||
    normalized === 'zhtw' ||
    normalized.startsWith('zh-')
  )
}

export function ollamaPullFailureText(
  raw: string,
  language: string,
  t: Translate
): string {
  const text = raw.trim()
  if (!text || (chineseInterface(language) && !HAN.test(text))) {
    return t('Request failed')
  }
  return text
}

export function ollamaActionFailureText(
  raw: string,
  language: string,
  t: Translate,
  fallbackKey: string
): string {
  const text = raw.trim()
  if (!text || (chineseInterface(language) && !HAN.test(text))) {
    return t(fallbackKey)
  }
  return text
}

export function ollamaPullStatusText(
  raw: string,
  language: string,
  t: Translate
): string {
  const text = raw.trim()
  if (!text || text === '-') return '-'
  if (text.toLowerCase() === 'success') return t('Success')
  if (chineseInterface(language) && !HAN.test(text)) return t('Pulling...')
  return text
}
