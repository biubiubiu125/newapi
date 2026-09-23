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
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { formatRelativeTime as formatChannelRelativeTime } from '@/features/channels/lib/channel-utils'
import { formatRelativeTime as formatModelRelativeTime } from '@/features/models/lib/model-utils'
import zhCN from '@/i18n/locales/zh.json'

import { formatTimestamp, formatTokens, formatUseTime } from './format'

const zhResources = (zhCN as { translation: Record<string, string> })
  .translation

describe('localized empty timestamps and durations', () => {
  beforeEach(async () => {
    i18n.addResourceBundle('zhCN', 'translation', zhResources, true, true)
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        Never: 'Never',
        '{{seconds}}s': '{{seconds}}s',
        '{{minutes}}m {{seconds}}s': '{{minutes}}m {{seconds}}s',
      },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')
  })

  afterEach(async () => {
    await i18n.changeLanguage('en')
  })

  it('renders the Never sentinel in the active interface language', () => {
    expect(formatTimestamp(-1)).toBe('永不')
    expect(formatChannelRelativeTime(0)).toBe('永不')
    expect(formatModelRelativeTime(0)).toBe('永不')
  })

  it('formats durations with localized units', () => {
    expect(formatUseTime(1.2)).toBe('1.2秒')
    expect(formatUseTime(184)).toBe('3分4秒')
  })
})

describe('formatTokens', () => {
  it('uses compact notation for the active interface locale', async () => {
    await i18n.changeLanguage('fr')
    expect(formatTokens(0)).toBe('-')
    expect(formatTokens(1500)).toBe(
      new Intl.NumberFormat('fr', {
        notation: 'compact',
        maximumFractionDigits: 1,
      }).format(1500)
    )
    await i18n.changeLanguage('en')
  })
})
