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
  id: number
  user_id: number
  username: string
  amount: number
  credit_amount: number
  money: number
  paid_amount: number
  paid_currency: string
  trade_no: string
  payment_method: string
  payment_provider: string
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
    credit_amount: number
  }
  by_currency: Array<{
    currency: string
    count: number
    paid_amount: number
  }>
  by_provider: Array<{
    payment_provider: string
    paid_currency: string
    count: number
    paid_amount: number
  }>
  by_status: Array<{
    status: string
    currency: string
    count: number
    paid_amount: number
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
}

export interface PageResponse<T> {
  items: T[]
  total: number
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
