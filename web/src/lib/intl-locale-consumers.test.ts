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
import { waitFor } from '@testing-library/react'
import i18n from 'i18next'
import { afterEach, describe, expect, it } from 'vitest'

import { formatChartNumber } from '@/components/ui/chart'
import { formatRateLimit } from '@/features/pricing/lib/mock-stats'
import { formatTokenCount } from '@/features/pricing/lib/model-metadata'
import { formatCnyPrice as formatSubscriptionCnyPrice } from '@/features/subscriptions/lib/format'
import {
  formatCurrency,
  formatCnyPrice,
  formatQuotaShort,
} from '@/features/wallet/lib/format'
import { applyDocumentLang } from '@/i18n/languages'
import { applyAuthBundle, getCommonHeaders } from '@/lib/auth-session'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import type { AuthBundle } from '@/stores/auth-store'

const authBundle: AuthBundle = {
  access_token: 'token',
  token_type: 'Bearer',
  access_expires_at: Math.floor(Date.now() / 1000) + 3600,
  user: { id: 1, username: 'user', role: 1, language: 'zhCN' },
  session: {
    sid: 'sid-1',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'test',
    created_at: 1,
    last_active_at: 1,
    expires_at: 9_999_999_999,
  },
}

describe('interface-language Intl consumers', () => {
  afterEach(async () => {
    await i18n.changeLanguage('en')
  })

  it('formats currency, wallet amounts, and token counts with the active locale', async () => {
    await i18n.changeLanguage('fr')
    const number = new Intl.NumberFormat('fr', {
      minimumFractionDigits: 0,
      maximumFractionDigits: 4,
    }).format(1234.5)
    const price = new Intl.NumberFormat('fr', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    }).format(12.5)
    const tokens = new Intl.NumberFormat('fr', {
      maximumFractionDigits: 1,
    }).format(1.5)

    expect(formatCurrency(1234.5)).toBe(number)
    expect(formatLocalCurrencyAmount(1234.5, { showSymbol: false })).toBe(
      number
    )
    expect(formatCnyPrice(12.5)).toBe(`\u00a5${price}`)
    expect(formatSubscriptionCnyPrice(12.5)).toBe(`\u00a5${price}`)
    expect(formatTokenCount(1_500_000)).toBe(`${tokens}M`)
    expect(formatQuotaShort(1_500_000)).toBe(
      new Intl.NumberFormat('fr', {
        notation: 'compact',
        maximumFractionDigits: 1,
      }).format(1_500_000)
    )
    expect(formatChartNumber(1234.5)).toBe(
      new Intl.NumberFormat('fr').format(1234.5)
    )
    expect(formatRateLimit(123)).toBe(new Intl.NumberFormat('fr').format(123))
  })

  it('puts BCP-47 Accept-Language on common request headers', async () => {
    await i18n.changeLanguage('zhCN')
    expect(getCommonHeaders()['Accept-Language']).toBe('zh-CN')
  })

  it('writes the active BCP-47 locale onto document.documentElement.lang', () => {
    applyDocumentLang('zhCN')
    expect(document.documentElement.lang).toBe('zh-CN')
    applyDocumentLang('en')
    expect(document.documentElement.lang).toBe('en')
  })

  it('applies the saved interface language when accepting an auth bundle', async () => {
    await i18n.changeLanguage('en')
    applyAuthBundle(authBundle)
    await waitFor(() => {
      expect(i18n.language).toBe('zhCN')
    })
  })
})
