import i18next from 'i18next'

import { consoleDetailText } from './referral-error-label'

type Translate = (key: string) => string

export function consoleFailureText(
  value: string | null | undefined,
  language: string,
  t: Translate,
  fallbackKey: string
): string {
  const text = value?.trim() ?? ''
  if (!text) return t(fallbackKey)
  return consoleDetailText(text, language, t, fallbackKey)
}

export function currentConsoleFailureText(
  value: string | null | undefined,
  fallbackKey: string
): string {
  return consoleFailureText(
    value,
    i18next.language,
    (key) => String(i18next.t(key)),
    fallbackKey
  )
}
