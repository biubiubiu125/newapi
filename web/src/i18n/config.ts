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
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import { applyDayjsLocale } from '@/lib/dayjs'

import {
  applyDocumentLang,
  convertDetectedLanguage,
  DEFAULT_INTERFACE_LANGUAGE,
  GUEST_LANGUAGE_DETECTION_ORDER,
} from './languages'
import en from './locales/en.json'
import fr from './locales/fr.json'
import ja from './locales/ja.json'
import ru from './locales/ru.json'
import vi from './locales/vi.json'
import zhTW from './locales/zh-TW.json'
import zhCN from './locales/zh.json'

export const resources = {
  en,
  zhCN,
  fr,
  ru,
  ja,
  vi,
  zhTW,
} as const

function applyInterfaceRuntime(language?: string | null) {
  applyDayjsLocale(language)
  applyDocumentLang(language)
}

i18n.on('languageChanged', applyInterfaceRuntime)

applyInterfaceRuntime(DEFAULT_INTERFACE_LANGUAGE)

export const whenInterfaceLanguageReady: Promise<void> = i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: DEFAULT_INTERFACE_LANGUAGE,
    returnEmptyString: false,
    supportedLngs: ['en', 'zhCN', 'fr', 'ru', 'ja', 'vi', 'zhTW'],
    load: 'currentOnly',
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env.DEV,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: [...GUEST_LANGUAGE_DETECTION_ORDER],
      caches: ['localStorage'],
      // Browsers report `zh-CN`/`zh-TW`/`en-US`; map them onto interface codes.
      // Unknown tags become simplified Chinese.
      convertDetectedLanguage,
    },
  })
  .then(() => {
    applyInterfaceRuntime(i18n.language || i18n.resolvedLanguage)
  })
  .then(() => undefined)

export default i18n
