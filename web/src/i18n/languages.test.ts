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
along with this program. If you did not receive a copy of the GNU Affero
General Public License along with this program, see
<https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'
import { describe, expect, it } from 'vitest'

import {
  convertDetectedLanguage,
  currentIntlLocale,
  dayjsLocaleForLanguage,
  normalizeInterfaceLanguage,
  resolveIntlLocale,
  toIntlLocale,
} from './languages'

describe('normalizeInterfaceLanguage', () => {
  it('keeps the project interface codes', () => {
    expect(normalizeInterfaceLanguage('zhCN')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zhTW')).toBe('zhTW')
    expect(normalizeInterfaceLanguage('en')).toBe('en')
    expect(normalizeInterfaceLanguage('fr')).toBe('fr')
  })

  it('maps browser and backend Chinese tags onto zhCN / zhTW', () => {
    expect(normalizeInterfaceLanguage('zh-CN')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh_CN')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh-cn')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh-Hans')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh-SG')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh_SG')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('zh-TW')).toBe('zhTW')
    expect(normalizeInterfaceLanguage('zh-hk')).toBe('zhTW')
    expect(normalizeInterfaceLanguage('zh-Hant')).toBe('zhTW')
  })

  it('maps regional western tags onto supported primary codes', () => {
    expect(normalizeInterfaceLanguage('en-US')).toBe('en')
    expect(normalizeInterfaceLanguage('fr-FR')).toBe('fr')
    expect(normalizeInterfaceLanguage('ja-JP')).toBe('ja')
    expect(normalizeInterfaceLanguage('ru-RU')).toBe('ru')
    expect(normalizeInterfaceLanguage('vi-VN')).toBe('vi')
  })

  it('falls back to simplified Chinese for empty or unknown values', () => {
    expect(normalizeInterfaceLanguage(undefined)).toBe('zhCN')
    expect(normalizeInterfaceLanguage('')).toBe('zhCN')
    expect(normalizeInterfaceLanguage('pt-BR')).toBe('zhCN')
  })
})

describe('convertDetectedLanguage', () => {
  it('keeps already-normalized interface codes, including zhTW from localStorage', () => {
    expect(convertDetectedLanguage('zhCN')).toBe('zhCN')
    expect(convertDetectedLanguage('zhTW')).toBe('zhTW')
    expect(convertDetectedLanguage('zhtw')).toBe('zhTW')
    expect(convertDetectedLanguage('en')).toBe('en')
  })

  it('maps browser Chinese tags onto zhCN / zhTW', () => {
    expect(convertDetectedLanguage('zh-CN')).toBe('zhCN')
    expect(convertDetectedLanguage('zh_CN')).toBe('zhCN')
    expect(convertDetectedLanguage('zh-Hans')).toBe('zhCN')
    expect(convertDetectedLanguage('zh')).toBe('zhCN')
    expect(convertDetectedLanguage('zh-SG')).toBe('zhCN')
    expect(convertDetectedLanguage('zh_SG')).toBe('zhCN')
    expect(convertDetectedLanguage('zh-TW')).toBe('zhTW')
    expect(convertDetectedLanguage('zh_TW')).toBe('zhTW')
    expect(convertDetectedLanguage('zh-HK')).toBe('zhTW')
    expect(convertDetectedLanguage('zh-Hant')).toBe('zhTW')
  })

  it('leaves non-Chinese tags unchanged for i18next supportedLngs matching', () => {
    expect(convertDetectedLanguage('fr-FR')).toBe('fr-FR')
    expect(convertDetectedLanguage('ja-JP')).toBe('ja-JP')
    expect(convertDetectedLanguage('en-US')).toBe('en-US')
  })
})

describe('resolveIntlLocale', () => {
  it('maps interface codes onto BCP-47 tags for HTTP and Intl', () => {
    expect(resolveIntlLocale('zhCN')).toBe('zh-CN')
    expect(resolveIntlLocale('zhTW')).toBe('zh-TW')
    expect(resolveIntlLocale('en')).toBe('en')
    expect(toIntlLocale('zhCN')).toBe('zh-CN')
  })

  it('falls back to zh-CN when the language is missing', () => {
    expect(resolveIntlLocale(undefined)).toBe('zh-CN')
    expect(resolveIntlLocale('')).toBe('zh-CN')
  })
})

describe('dayjsLocaleForLanguage', () => {
  it('maps interface languages onto dayjs locale ids', () => {
    expect(dayjsLocaleForLanguage('zhCN')).toBe('zh-cn')
    expect(dayjsLocaleForLanguage('zhTW')).toBe('zh-tw')
    expect(dayjsLocaleForLanguage('en')).toBe('en')
    expect(dayjsLocaleForLanguage('ja')).toBe('ja')
    expect(dayjsLocaleForLanguage(undefined)).toBe('zh-cn')
  })
})

describe('currentIntlLocale', () => {
  it('follows the active i18next language', async () => {
    await i18n.changeLanguage('zhCN')
    expect(currentIntlLocale()).toBe('zh-CN')
    await i18n.changeLanguage('en')
    expect(currentIntlLocale()).toBe('en')
  })
})
