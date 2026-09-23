/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'

export const INTERFACE_LANGUAGE_OPTIONS = [
  { code: 'zhCN', label: '简体中文' },
  { code: 'en', label: 'English' },
  { code: 'fr', label: 'Français' },
  { code: 'ru', label: 'Русский' },
  { code: 'ja', label: '日本語' },
  { code: 'vi', label: 'Tiếng Việt' },
  { code: 'zhTW', label: '繁體中文' },
] as const

export type InterfaceLanguageCode =
  (typeof INTERFACE_LANGUAGE_OPTIONS)[number]['code']

export const DEFAULT_INTERFACE_LANGUAGE: InterfaceLanguageCode = 'zhCN'
export const DEFAULT_INTL_LOCALE = 'zh-CN'
// Guests: an explicit localStorage choice, then the browser language.
// Anything still unresolved uses the i18next fallback, simplified Chinese.
// A signed-in account language replaces this after login.
export const GUEST_LANGUAGE_DETECTION_ORDER = [
  'localStorage',
  'navigator',
] as const

export function normalizeInterfaceLanguage(
  value?: string | null
): InterfaceLanguageCode {
  if (!value) return DEFAULT_INTERFACE_LANGUAGE

  const trimmed = value.trim()
  if (!trimmed) return DEFAULT_INTERFACE_LANGUAGE

  const lower = trimmed.replaceAll('_', '-').toLowerCase()
  if (
    lower === 'zhtw' ||
    lower === 'zh-tw' ||
    lower === 'zh-hk' ||
    lower === 'zh-mo' ||
    lower.startsWith('zh-hant')
  ) {
    return 'zhTW'
  }
  if (lower.startsWith('zh')) {
    return 'zhCN'
  }

  const supported = INTERFACE_LANGUAGE_OPTIONS.map((lang) => lang.code)
  const exact = supported.find((code) => code.toLowerCase() === lower)
  if (exact) return exact

  const prefix = lower.split('-')[0]
  const prefixed = supported.find((code) => code.toLowerCase() === prefix)
  return prefixed ?? DEFAULT_INTERFACE_LANGUAGE
}

/**
 * Map a browser-detected locale onto the interface language codes this project
 * uses with i18next (`zhCN` / `zhTW`).
 *
 * Browsers report standard BCP-47 tags (`zh-CN`, `zh-TW`, `zh-Hant`, `zh`, ...),
 * but `supportedLngs`/resources use the non-standard camelCase codes, so without
 * this mapping a Chinese browser would never match and fall back to simplified Chinese.
 * Other browser tags are mapped onto supported interface codes (`en-US` -> `en`).
 * Unknown tags become simplified Chinese instead of being left as raw BCP-47 text.
 */
export function convertDetectedLanguage(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return value
  return normalizeInterfaceLanguage(trimmed)
}

/**
 * Convert an interface language code (the values i18next uses, such as `zhCN` /
 * `zhTW`) into a valid BCP-47 locale tag that the `Intl.*` APIs accept.
 *
 * `new Intl.NumberFormat('zhCN')` throws `RangeError: Invalid language tag`, so
 * any locale derived from `i18n.language` / `i18n.resolvedLanguage` MUST be run
 * through this before it reaches an `Intl` constructor. Unknown values fall back
 * to `undefined`; callers that need a guaranteed locale should use
 * `resolveIntlLocale` instead of letting `Intl` follow the OS language.
 */
export function toIntlLocale(value?: string | null): string | undefined {
  if (!value) return undefined
  switch (value) {
    case 'zhCN':
      return 'zh-CN'
    case 'zhTW':
      return 'zh-TW'
    default:
      break
  }
  try {
    return Intl.getCanonicalLocales(value)[0]
  } catch {
    return undefined
  }
}

export function resolveIntlLocale(value?: string | null): string {
  return toIntlLocale(value) ?? DEFAULT_INTL_LOCALE
}

export function currentIntlLocale(): string {
  return resolveIntlLocale(i18n.language || i18n.resolvedLanguage)
}

export function speechRecognitionLocale(value?: string | null): string {
  return resolveIntlLocale(value ?? (i18n.language || i18n.resolvedLanguage))
}

export async function changeInterfaceLanguage(
  code: string,
  persist?: (code: string) => Promise<void>
): Promise<void> {
  const previous = i18n.language
  const next = normalizeInterfaceLanguage(code)
  if (!persist) {
    await i18n.changeLanguage(next)
    return
  }
  try {
    await persist(next)
    await i18n.changeLanguage(next)
  } catch (error) {
    await i18n.changeLanguage(previous)
    throw error
  }
}

export function applyDocumentLang(language?: string | null): void {
  if (typeof document === 'undefined') return
  document.documentElement.lang = resolveIntlLocale(language)
}

export function dayjsLocaleForLanguage(value?: string | null): string {
  switch (normalizeInterfaceLanguage(value)) {
    case 'zhCN':
      return 'zh-cn'
    case 'zhTW':
      return 'zh-tw'
    case 'fr':
      return 'fr'
    case 'ru':
      return 'ru'
    case 'ja':
      return 'ja'
    case 'vi':
      return 'vi'
    case 'en':
      return 'en'
  }
}
