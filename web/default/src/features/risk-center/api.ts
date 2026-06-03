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
  event_key?: string
  target_type?: string
  target_id?: string
  user_id?: number
  username?: string
  ip?: string
  token_id?: number
  trade_no?: string
  count: number
  amount?: number
  message: string
  first_seen_at?: number
  last_seen_at?: number
}

export interface RiskEvent {
  id: number
  event_key: string
  type: string
  target_type: string
  target_id: string
  user_id?: number
  username?: string
  ip?: string
  token_id?: number
  token_name?: string
  order_type?: string
  trade_no?: string
  referral_user_id?: number
  severity: string
  status: string
  title: string
  summary: string
  evidence?: string
  hit_count: number
  amount?: number
  window_hours: number
  first_seen_at?: number
  last_seen_at?: number
  reviewed_at?: number
  reviewed_by?: number
  resolved_at?: number
  resolved_by?: number
  resolve_note?: string
  created_at?: number
  updated_at?: number
}

export interface RiskUser {
  user_id: number
  username: string
  email?: string
  status: number
  role: number
  group?: string
  created_at: number
  last_login_at: number
  register_ip?: string
  quota: number
  used_quota: number
  request_count: number
  topup_count: number
  topup_paid_amount: number
  error_count: number
  consume_count: number
  consume_quota: number
  unique_ip_count: number
  token_count: number
  signal_count: number
  severity: string
}

export interface RiskLog {
  id: number
  user_id: number
  username: string
  type: number
  content: string
  quota: number
  ip: string
  token_id?: number
  token_name?: string
  model_name?: string
  created_at: number
}

export interface RiskOrder {
  order_type: string
  trade_no: string
  user_id: number
  username: string
  status: string
  paid_amount: number
  paid_currency: string
  payment_provider: string
  payment_method: string
  referral_commission_status: string
  referral_commission_error: string
  created_at: number
}

export interface RiskToken {
  token_id: number
  token_name: string
  user_id: number
  username: string
  status: number
  request_count: number
  error_count: number
  consume_quota: number
  unique_ip_count: number
  last_seen_at: number
}

export interface RiskIP {
  ip: string
  user_count: number
  token_count: number
  request_count: number
  error_count: number
  consume_quota: number
  failure_rate: number
  first_seen_at: number
  last_seen_at: number
  whitelisted: boolean
  whitelist_note?: string
}

export interface RiskReferral {
  affiliate_id: number
  inviter_user_id: number
  inviter_username: string
  invitee_count: number
  commission_count: number
  commission_amount: number
  withdrawal_count: number
  withdrawal_amount: number
  severity: string
  reason: string
}

export interface RiskAction {
  id: number
  event_id: number
  action: string
  target_type: string
  target_id: string
  user_id: number
  token_id: number
  ip: string
  operator_user_id: number
  operator_name: string
  reason: string
  old_value: string
  new_value: string
  evidence: string
  client_ip: string
  user_agent: string
  created_at: number
}

export interface RiskWhitelist {
  id: number
  target_type: string
  target_id: string
  reason: string
  operator_user_id: number
  operator_name: string
  expires_at: number
  created_at: number
  updated_at: number
}

export interface RiskDetail {
  type: string
  window_hours: number
  ip?: string
  user_id?: number
  token_id?: number
  trade_no?: string
  event?: RiskEvent
  users: RiskUser[]
  logs: RiskLog[]
  orders: RiskOrder[]
  tokens: RiskToken[]
  ips: RiskIP[]
  referrals: RiskReferral[]
  actions: RiskAction[]
  whitelists: RiskWhitelist[]
}

export interface RiskPage<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

export interface RiskActionPayload {
  event_id?: number
  reason?: string
  target_type?: string
  target_id?: string
  user_id?: number
  token_id?: number
  ip?: string
  expires_at?: number
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
      open_event_count: number
      high_event_count: number
      disabled_users: number
      new_user_count: number
      signals: RiskSignal[]
    }
  }
}

export async function scanRiskEvents(params: URLSearchParams) {
  const res = await api.post(`/api/user/admin/risk/scan?${params.toString()}`)
  return res.data as {
    success: boolean
    message?: string
    data: {
      window_hours: number
      count: number
      events: RiskEvent[]
    }
  }
}

export async function getRiskEvents(params: URLSearchParams) {
  const res = await api.get(`/api/user/admin/risk/events?${params.toString()}`)
  return res.data as {
    success: boolean
    message?: string
    data: RiskPage<RiskEvent>
  }
}

export async function getRiskDetail(params: URLSearchParams) {
  const res = await api.get(`/api/user/admin/risk/detail?${params.toString()}`)
  const result = res.data as {
    success: boolean
    message?: string
    data: RiskDetail
  }
  if (result.success && result.data) {
    result.data = normalizeRiskDetail(result.data)
  }
  return result
}

export async function getRiskUsers(params: URLSearchParams) {
  const res = await api.get(`/api/user/admin/risk/users?${params.toString()}`)
  return res.data as {
    success: boolean
    message?: string
    data: RiskPage<RiskUser>
  }
}

export async function markRiskEventViewed(eventId: number) {
  const res = await api.post(`/api/user/admin/risk/events/${eventId}/view`)
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function resolveRiskEvent(
  eventId: number,
  payload: RiskActionPayload
) {
  const res = await api.post(
    `/api/user/admin/risk/events/${eventId}/resolve`,
    payload
  )
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function ignoreRiskEvent(
  eventId: number,
  payload: RiskActionPayload
) {
  const res = await api.post(
    `/api/user/admin/risk/events/${eventId}/ignore`,
    payload
  )
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function banRiskUser(userId: number, payload: RiskActionPayload) {
  const res = await api.post(
    `/api/user/admin/risk/users/${userId}/ban`,
    payload
  )
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function unbanRiskUser(
  userId: number,
  payload: RiskActionPayload
) {
  const res = await api.post(
    `/api/user/admin/risk/users/${userId}/unban`,
    payload
  )
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function disableRiskToken(
  tokenId: number,
  payload: RiskActionPayload
) {
  const res = await api.post(
    `/api/user/admin/risk/tokens/${tokenId}/disable`,
    payload
  )
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function createRiskWhitelist(payload: RiskActionPayload) {
  const res = await api.post('/api/user/admin/risk/whitelist', payload)
  return res.data as {
    success: boolean
    message?: string
    data: RiskWhitelist
  }
}

export async function deleteRiskWhitelist(
  id: number,
  payload: Pick<RiskActionPayload, 'reason'>
) {
  const res = await api.delete(`/api/user/admin/risk/whitelist/${id}`, {
    data: payload,
  })
  return res.data as { success: boolean; message?: string; data: unknown }
}

export async function getRiskActions(params: URLSearchParams) {
  const res = await api.get(`/api/user/admin/risk/actions?${params.toString()}`)
  return res.data as {
    success: boolean
    message?: string
    data: RiskPage<RiskAction>
  }
}

function normalizeRiskDetail(data: RiskDetail): RiskDetail {
  return {
    ...data,
    users: data.users || [],
    logs: data.logs || [],
    orders: data.orders || [],
    tokens: data.tokens || [],
    ips: data.ips || [],
    referrals: data.referrals || [],
    actions: data.actions || [],
    whitelists: data.whitelists || [],
  }
}
