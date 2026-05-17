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

export interface RiskSignal {
  type: string
  severity: string
  user_id?: number
  username?: string
  ip?: string
  count: number
  amount?: number
  message: string
  first_seen_at?: number
  last_seen_at?: number
}

export interface RiskUser {
  user_id: number
  username: string
  status: number
  role: number
  created_at: number
  last_login_at: number
  quota: number
  used_quota: number
  request_count: number
  topup_count: number
  topup_paid_amount: number
  error_count: number
  consume_count: number
  consume_quota: number
  signal_count: number
  severity: string
}

export async function getRiskOverview(params: URLSearchParams) {
  const res = await api.get(
    `/api/user/admin/risk/overview?${params.toString()}`
  )
  return res.data as {
    success: boolean
    message?: string
    data: {
      window_hours: number
      signal_count: number
      disabled_users: number
      signals: RiskSignal[]
    }
  }
}

export async function getRiskUsers(params: URLSearchParams) {
  const res = await api.get(`/api/user/admin/risk/users?${params.toString()}`)
  return res.data as {
    success: boolean
    message?: string
    data: {
      items: RiskUser[]
      total: number
    }
  }
}
