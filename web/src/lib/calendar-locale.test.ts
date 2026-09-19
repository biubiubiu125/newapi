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
import { describe, expect, it } from 'vitest'

import { resolveDayPickerLocale } from './calendar-locale'

describe('resolveDayPickerLocale', () => {
  it('uses simplified Chinese for zhCN instead of falling back to English', () => {
    expect(resolveDayPickerLocale('zhCN').code).toBe('zh-CN')
    expect(resolveDayPickerLocale('zh').code).toBe('zh-CN')
  })

  it('uses traditional Chinese for zhTW', () => {
    expect(resolveDayPickerLocale('zhTW').code).toBe('zh-TW')
  })

  it('keeps English when the interface language is en', () => {
    expect(resolveDayPickerLocale('en').code).toMatch(/^en/)
  })

  it('defaults to simplified Chinese when the language is missing', () => {
    expect(resolveDayPickerLocale(undefined).code).toBe('zh-CN')
  })
})
