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
import { api } from '@/lib/api'

export interface RechargeAuditOrder {
  order_type: 'topup' | 'subscription' | string
  id: number
  user_id: number
  username: string
  amount: number
  credit_amount: number
  credit_quota: number
  product_name: string
  money: number
  paid_amount: number
  paid_currency: string
  paid_amount_cny: number
  paid_cny_fx_rate: number
  paid_cny_fx_missing: boolean
  trade_no: string
  payment_method: string
  payment_provider: string
  price_snapshot: number
  usd_exchange_rate_snapshot: number
  quota_display_type_snapshot: string
  create_time: number
  complete_time: number
  status: string
  referral_commission_status: string
  referral_commission_error: string
}

export interface RechargeAuditSummary {
  totals: {
    total_count: number
    success_count: number
    pending_count: number
    failed_count: number
    paid_amount: number
    paid_amount_cny: number
    fx_missing_count: number
    credit_amount: number
  }
  by_currency: Array<{
    currency: string
    count: number
    paid_amount: number
    paid_amount_cny: number
    paid_cny_fx_rate: number
    fx_missing_count: number
  }>
  by_provider: Array<{
    payment_provider: string
    paid_currency: string
    count: number
    paid_amount: number
    paid_amount_cny: number
    paid_cny_fx_rate: number
    fx_missing_count: number
  }>
  by_status: Array<{
    status: string
    currency: string
    count: number
    paid_amount: number
    paid_amount_cny: number
    paid_cny_fx_rate: number
    fx_missing_count: number
  }>
  anomalies: Array<{
    type: string
    severity: string
    trade_no?: string
    user_id?: number
    username?: string
    message: string
    created_at?: number
  }>
  new_order_count?: number
  latest_order_cursor?: string
}

export interface PageResponse<T> {
  items: T[]
  total: number
}

export interface PaymentOrphanEvent {
  id: number
  provider: string
  event_id: string
  event_type: string
  reference_id: string
  session_id: string
  status: string
  reason: string
  error: string
  create_time: number
  update_time: number
  resolved_by: number
  resolved_at: number
  resolution: string
  resolution_note: string
  can_credit: boolean
}

export async function getRechargeAuditSummary(params: URLSearchParams) {
  const res = await api.get(
    `/api/user/admin/finance/recharge-audit/summary?${params.toString()}`
  )
  return res.data as {
    success: boolean
    message?: string
    data: RechargeAuditSummary
  }
}

export async function getRechargeAudit(params: URLSearchParams) {
  const res = await api.get(
    `/api/user/admin/finance/recharge-audit?${params.toString()}`
  )
  return res.data as {
    success: boolean
    message?: string
    data: PageResponse<RechargeAuditOrder>
  }
}

export type PaymentOrphanStatusFilter =
  | 'pending_review'
  | 'credited'
  | 'refunded'
  | 'dismissed'
  | 'all'

export type PaymentOrphanListOptions = {
  status?: PaymentOrphanStatusFilter
  page?: number
  pageSize?: number
}

export async function getPaymentOrphans(
  options: PaymentOrphanListOptions = {}
) {
  const { status = 'pending_review', page = 1, pageSize = 20 } = options
  const params = new URLSearchParams({
    p: String(page),
    page_size: String(pageSize),
    status,
  })
  const res = await api.get(
    `/api/user/admin/finance/payment-orphans?${params.toString()}`
  )
  return res.data as {
    success: boolean
    message?: string
    data: PageResponse<PaymentOrphanEvent>
  }
}

export async function creditPaymentOrphan(id: number) {
  const res = await api.post(
    `/api/user/admin/finance/payment-orphans/${id}/credit`
  )
  return res.data as { success: boolean; message?: string }
}

export async function resolvePaymentOrphan(
  id: number,
  status: 'refunded' | 'dismissed',
  note: string
) {
  const res = await api.post(
    `/api/user/admin/finance/payment-orphans/${id}/resolve`,
    { status, note }
  )
  return res.data as { success: boolean; message?: string }
}
