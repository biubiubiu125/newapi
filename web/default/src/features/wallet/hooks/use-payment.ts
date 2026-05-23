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
import {
  calculateAmount,
  calculateStripeAmount,
  requestPayment,
  requestBEpusdtPayment,
  requestStripePayment,
  isApiSuccess,
} from '../api'
import { isStripePayment, isBEpusdtPayment, submitPaymentForm } from '../lib'
import type { PaymentInitiationResult } from '../types'

// ============================================================================
// Payment Hook
// ============================================================================

function getPaymentErrorMessage(response: unknown): string {
  const payload = response as
    | {
        message?: unknown
        data?: unknown
      }
    | undefined
  const message =
    typeof payload?.message === 'string' && payload.message !== 'error'
      ? payload.message
      : ''
  const dataMessage = typeof payload?.data === 'string' ? payload.data : ''
  return message || dataMessage || i18next.t('Payment request failed')
}

export function usePayment() {
  const [amount, setAmount] = useState<number>(0)
  const [calculating, setCalculating] = useState(false)
  const [processing, setProcessing] = useState(false)

  // Calculate payment amount
  const calculatePaymentAmount = useCallback(
    async (topupAmount: number, paymentType: string) => {
      try {
        setCalculating(true)

        const isStripe = isStripePayment(paymentType)
        const response = isStripe
          ? await calculateStripeAmount({ amount: topupAmount })
          : await calculateAmount({ amount: topupAmount })

        if (isApiSuccess(response) && response.data) {
          const calculatedAmount = parseFloat(response.data)
          setAmount(calculatedAmount)
          return calculatedAmount
        }

        // Don't show error for calculation, just set to 0
        setAmount(0)
        return 0
      } catch (_error) {
        setAmount(0)
        return 0
      } finally {
        setCalculating(false)
      }
    },
    []
  )

  const getResponseTradeNo = useCallback(
    (response: unknown, paymentData?: Record<string, unknown>) => {
      for (const key of ['trade_no', 'order_id', 'out_trade_no']) {
        const value = paymentData?.[key]
        if (typeof value === 'string' && value.trim()) {
          return value
        }
      }

      const payload =
        response && typeof response === 'object'
          ? (response as Record<string, unknown>)
          : undefined
      const tradeNo = payload?.trade_no
      const orderId = payload?.order_id
      return (
        (typeof tradeNo === 'string' && tradeNo) ||
        (typeof orderId === 'string' && orderId) ||
        undefined
      )
    },
    []
  )

  // Process payment
  const processPayment = useCallback(
    async (
      topupAmount: number,
      paymentType: string
    ): Promise<PaymentInitiationResult> => {
      try {
        setProcessing(true)

        const isStripe = isStripePayment(paymentType)
        const isBEpusdt = isBEpusdtPayment(paymentType)
        const amount = Math.floor(topupAmount)

        const response = isStripe
          ? await requestStripePayment({
              amount,
              payment_method: 'stripe',
            })
          : isBEpusdt
            ? await requestBEpusdtPayment({
                amount,
                payment_method: paymentType,
              })
            : await requestPayment({
                amount,
                payment_method: paymentType,
              })

        if (!isApiSuccess(response)) {
          toast.error(getPaymentErrorMessage(response))
          return { ok: false }
        }

        // Handle Stripe payment
        const paymentData = response.data as Record<string, unknown> | undefined
        const stripePayLink =
          typeof paymentData?.pay_link === 'string' ? paymentData.pay_link : ''
        const bepusdtPaymentUrl =
          typeof paymentData?.payment_url === 'string'
            ? paymentData.payment_url
            : ''

        if (isStripe && stripePayLink) {
          window.open(stripePayLink, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return {
            ok: true,
            tradeNo: getResponseTradeNo(response, paymentData),
            amount: topupAmount,
            payAmount: amount,
            paymentMethod: paymentType,
            paymentKind: 'topup',
          }
        }

        if (isBEpusdt && bepusdtPaymentUrl) {
          window.open(bepusdtPaymentUrl, '_blank')
          toast.success(i18next.t('Redirecting to payment page...'))
          return {
            ok: true,
            tradeNo: getResponseTradeNo(response, paymentData),
            amount: topupAmount,
            payAmount: amount,
            paymentMethod: paymentType,
            paymentKind: 'topup',
          }
        }

        // Handle non-Stripe payment
        if (!isStripe && !isBEpusdt && response.data) {
          const url = (response as unknown as { url?: string }).url
          if (url) {
            submitPaymentForm(url, response.data)
            toast.success(i18next.t('Redirecting to payment page...'))
            return {
              ok: true,
              tradeNo: getResponseTradeNo(response, paymentData),
              amount: topupAmount,
              payAmount: amount,
              paymentMethod: paymentType,
              paymentKind: 'topup',
            }
          }
        }

        return { ok: false }
      } catch (_error) {
        toast.error(i18next.t('Payment request failed'))
        return { ok: false }
      } finally {
        setProcessing(false)
      }
    },
    [getResponseTradeNo]
  )

  return {
    amount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
    setAmount,
  }
}
