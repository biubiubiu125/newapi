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
import i18next from 'i18next'
import { useState, useCallback } from 'react'
import { toast } from 'sonner'

import { requestWaffoPayment, isApiSuccess } from '../api'
import type { PaymentInitiationResult } from '../types'

function getPaymentUrl(data: unknown): string | null {
  if (!data || typeof data !== 'object') {
    return null
  }

  if ('payment_url' in data && typeof data.payment_url === 'string') {
    return data.payment_url
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

function getErrorMessage(message: string | undefined, data: unknown): string {
  if (typeof data === 'string' && data.trim()) {
    return data
  }

  return message || i18next.t('Payment request failed')
}

/**
 * Hook for handling Waffo payment processing
 */
export function useWaffoPayment() {
  const [processing, setProcessing] = useState(false)

  const processWaffoPayment = useCallback(
    async (
      topupAmount: number,
      payMethodIndex?: number
    ): Promise<PaymentInitiationResult> => {
      setProcessing(true)

      try {
        const response = await requestWaffoPayment({
          amount: Math.floor(topupAmount),
          pay_method_index: payMethodIndex,
        })

        if (isApiSuccess(response)) {
          const paymentUrl = getPaymentUrl(response.data)

          if (paymentUrl) {
            window.open(paymentUrl, '_blank')
            toast.success(i18next.t('Redirecting to payment page...'))
            return {
              ok: true,
              tradeNo: getTradeNo(response.data),
              amount: topupAmount,
              payAmount: Math.floor(topupAmount),
              paymentKind: 'topup',
              paymentMethod: 'waffo',
            }
          }
        }

        toast.error(getErrorMessage(response.message, response.data))
        return { ok: false }
      } catch {
        toast.error(i18next.t('Payment request failed'))
        return { ok: false }
      } finally {
        setProcessing(false)
      }
    },
    []
  )

  return { processing, processWaffoPayment }
}
