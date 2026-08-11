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
export const PENDING_PAYMENT_STORAGE_KEY = 'newapi.pending-payment'

const PENDING_PAYMENT_MAX_AGE_MS = 24 * 60 * 60 * 1000

export interface PendingPayment {
  tradeNo: string
  amount?: number
  payAmount?: number
  paymentMethod?: string
  paymentKind: 'topup' | 'subscription'
  title?: string
  startedAt: number
}

function isPendingPayment(value: unknown): value is PendingPayment {
  if (!value || typeof value !== 'object') return false

  const payment = value as Record<string, unknown>
  return (
    typeof payment.tradeNo === 'string' &&
    payment.tradeNo.trim() !== '' &&
    (payment.paymentKind === 'topup' ||
      payment.paymentKind === 'subscription') &&
    typeof payment.startedAt === 'number' &&
    Number.isFinite(payment.startedAt)
  )
}

export function savePendingPayment(storage: Storage, payment: PendingPayment) {
  storage.setItem(PENDING_PAYMENT_STORAGE_KEY, JSON.stringify(payment))
}

export function clearPendingPayment(storage: Storage) {
  storage.removeItem(PENDING_PAYMENT_STORAGE_KEY)
}

export function loadPendingPayment(
  storage: Storage,
  now = Date.now()
): PendingPayment | null {
  const raw = storage.getItem(PENDING_PAYMENT_STORAGE_KEY)
  if (!raw) return null

  try {
    const payment: unknown = JSON.parse(raw)
    if (
      !isPendingPayment(payment) ||
      now - payment.startedAt < 0 ||
      now - payment.startedAt >= PENDING_PAYMENT_MAX_AGE_MS
    ) {
      clearPendingPayment(storage)
      return null
    }
    return payment
  } catch {
    clearPendingPayment(storage)
    return null
  }
}
