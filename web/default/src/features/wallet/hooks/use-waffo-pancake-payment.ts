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
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { requestWaffoPancakePayment, isApiSuccess } from '../api'
import type { PaymentInitiationResult } from '../types'

function getCheckoutUrl(data: unknown): string | null {
  if (!data || typeof data !== 'object') {
    return null
  }

  if ('checkout_url' in data && typeof data.checkout_url === 'string') {
    return data.checkout_url
  }

  return null
}

function getTradeNo(data: unknown): string | undefined {
  if (!data || typeof data !== 'object') {
    return undefined
  }

  if ('trade_no' in data && typeof data.trade_no === 'string') {
    return data.trade_no
  }

  if ('order_id' in data && typeof data.order_id === 'string') {
    return data.order_id
  }

  return undefined
}

/**
 * Reject non-navigable schemes (e.g. javascript:, data:) and relative URLs.
 * Only http/https are allowed for backend-provided redirect targets.
 */
function isSafeHttpCheckoutUrl(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed) {
    return false
  }
  try {
    const u = new URL(trimmed)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }

  return message || i18next.t('Payment request failed')
}

/**
 * Hook for the Waffo Pancake hosted-checkout flow.
 *
 * Same-tab redirect (window.location.href) rather than window.open: the
 * user-gesture context is lost across the await, so popups get blocked.
 */
export function useWaffoPancakePayment() {
  const [processing, setProcessing] = useState(false)

  const processWaffoPancakePayment = useCallback(
    async (topupAmount: number): Promise<PaymentInitiationResult> => {
      setProcessing(true)

      try {
        const response = await requestWaffoPancakePayment({
          amount: Math.floor(topupAmount),
        })

        if (isApiSuccess(response)) {
          const checkoutUrl = getCheckoutUrl(response.data)

          if (checkoutUrl) {
            if (!isSafeHttpCheckoutUrl(checkoutUrl)) {
              toast.error(i18next.t('Invalid payment redirect URL'))
              return { ok: false }
            }
            toast.success(i18next.t('Redirecting to payment page...'))
            window.location.href = checkoutUrl
            return {
              ok: true,
              tradeNo: getTradeNo(response.data),
              amount: topupAmount,
              payAmount: Math.floor(topupAmount),
              paymentKind: 'topup',
              paymentMethod: 'waffo_pancake',
            }
          }
        }

        toast.error(getErrorMessage(response.message, response.data))
        return { ok: false }
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return { ok: false }
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return { processing, processWaffoPancakePayment }
}
