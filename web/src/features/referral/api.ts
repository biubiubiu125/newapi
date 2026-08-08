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

import type {
  ApiResponse,
  PaginatedData,
  ReferralAffiliate,
  ReferralBinding,
  ReferralCommission,
  ReferralCommissionJob,
  ReferralRedemptionBackfillResult,
  ReferralLedger,
  ReferralOverview,
  ReferralProfile,
  ReferralSettings,
  ReferralSummary,
  ReferralWithdrawal,
  ReferralAdminAuditLog,
  ReferralAdminBadgeCounts,
} from './types'

export async function getReferralProfile(): Promise<
  ApiResponse<ReferralProfile | null>
> {
  const res = await api.get('/api/user/referral/profile')
  return res.data
}

export async function getReferralSummary(): Promise<
  ApiResponse<ReferralSummary>
> {
  const res = await api.get('/api/user/referral/summary')
  return res.data
}

export async function applyReferralAffiliate(
  applicantNote?: string
): Promise<ApiResponse<ReferralProfile>> {
  const res = await api.post('/api/user/referral/apply', {
    applicant_note: applicantNote || '',
  })
  return res.data
}

export async function listReferralCommissions(params: {
  p?: number
  page_size?: number
  status?: string
}): Promise<ApiResponse<PaginatedData<ReferralCommission>>> {
  const res = await api.get('/api/user/referral/commissions', { params })
  return res.data
}

export async function listReferralWithdrawals(params: {
  p?: number
  page_size?: number
  status?: string
}): Promise<ApiResponse<PaginatedData<ReferralWithdrawal>>> {
  const res = await api.get('/api/user/referral/withdrawals', { params })
  return res.data
}

export async function createReferralWithdrawal(payload: {
  amount: number
  account_type: string
  account_name?: string
  account_no: string
  account_network?: string
  qr_image_url?: string
  applicant_note?: string
  idempotency_key: string
}): Promise<ApiResponse<ReferralWithdrawal>> {
  const res = await api.post('/api/user/referral/withdrawals', payload, {
    headers: { 'Idempotency-Key': payload.idempotency_key },
  })
  return res.data
}

export async function cancelReferralWithdrawal(
  id: number
): Promise<ApiResponse<ReferralWithdrawal>> {
  const res = await api.post(`/api/user/referral/withdrawals/${id}/cancel`)
  return res.data
}

export async function uploadReferralAsset(
  file: File
): Promise<ApiResponse<{ url: string }>> {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('purpose', 'withdrawal_qr')
  const res = await api.post('/api/user/referral/upload', formData)
  return res.data
}

export async function getAdminReferralOverview(): Promise<
  ApiResponse<ReferralOverview>
> {
  const res = await api.get('/api/user/admin/referral/overview')
  return res.data
}

export async function getAdminReferralBadges(
  params?: URLSearchParams
): Promise<ApiResponse<ReferralAdminBadgeCounts>> {
  const query = params?.toString()
  const res = await api.get(
    `/api/user/admin/referral/badges${query ? `?${query}` : ''}`
  )
  return res.data
}

export async function getAdminReferralSettings(): Promise<
  ApiResponse<ReferralSettings>
> {
  const res = await api.get('/api/user/admin/referral/settings')
  return res.data
}

export async function updateAdminReferralSettings(
  payload: ReferralSettings
): Promise<ApiResponse<ReferralSettings>> {
  const res = await api.put('/api/user/admin/referral/settings', payload)
  return res.data
}

export async function listAdminReferralAffiliates(params: {
  p?: number
  page_size?: number
  status?: string
  keyword?: string
}): Promise<ApiResponse<PaginatedData<ReferralAffiliate>>> {
  const res = await api.get('/api/user/admin/referral/affiliates', { params })
  return res.data
}

export async function listAdminPendingReferralAffiliates(params: {
  p?: number
  page_size?: number
  keyword?: string
}): Promise<ApiResponse<PaginatedData<ReferralAffiliate>>> {
  const res = await api.get('/api/user/admin/referral/pending', { params })
  return res.data
}

export async function listAdminReferralBindings(
  userId: number,
  params: { p?: number; page_size?: number }
): Promise<ApiResponse<PaginatedData<ReferralBinding>>> {
  const res = await api.get(
    `/api/user/admin/referral/affiliates/${userId}/bindings`,
    {
      params,
    }
  )
  return res.data
}

export async function listAdminReferralCommissions(params: {
  p?: number
  page_size?: number
  status?: string
  affiliate_user_id?: number
}): Promise<ApiResponse<PaginatedData<ReferralCommission>>> {
  const res = await api.get('/api/user/admin/referral/commissions', { params })
  return res.data
}

export async function listAdminReferralCommissionJobs(params: {
  p?: number
  page_size?: number
  status?: string
}): Promise<ApiResponse<PaginatedData<ReferralCommissionJob>>> {
  const res = await api.get('/api/user/admin/referral/commission-jobs', {
    params,
  })
  return res.data
}

export async function retryAdminReferralCommissionJob(payload: {
  source_type: string
  trade_no: string
}): Promise<ApiResponse<{ source_type: string; trade_no: string }>> {
  const res = await api.post(
    '/api/user/admin/referral/commission-jobs/retry',
    payload
  )
  return res.data
}

export async function backfillAdminReferralRedemptionJobs(payload: {
  limit?: number
  succeeded_cursor_id?: number
  succeeded_scan_limit?: number
}): Promise<ApiResponse<ReferralRedemptionBackfillResult>> {
  const res = await api.post(
    '/api/user/admin/referral/commission-jobs/backfill-redemptions',
    payload
  )
  return res.data
}

export async function listAdminReferralLedgers(params: {
  p?: number
  page_size?: number
  keyword?: string
}): Promise<ApiResponse<PaginatedData<ReferralLedger>>> {
  const res = await api.get('/api/user/admin/referral/ledgers', { params })
  return res.data
}

export async function listAdminReferralAuditLogs(params: {
  p?: number
  page_size?: number
  keyword?: string
}): Promise<ApiResponse<PaginatedData<ReferralAdminAuditLog>>> {
  const res = await api.get('/api/user/admin/referral/audit-logs', { params })
  return res.data
}

export async function approveReferralAffiliate(
  userId: number,
  payload?: { rate_override?: number | null; reason?: string }
): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/approve`,
    payload || {}
  )
  return res.data
}

export async function updateReferralAffiliateRate(
  userId: number,
  payload: { rate_override?: number | null; reason?: string }
): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/rate`,
    payload
  )
  return res.data
}

export async function rejectReferralAffiliate(
  userId: number,
  payload?: { reason?: string }
): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/reject`,
    payload || {}
  )
  return res.data
}

export async function disableReferralAffiliate(
  userId: number,
  payload?: { reason?: string }
): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/disable`,
    payload || {}
  )
  return res.data
}

export async function restoreReferralAffiliate(
  userId: number
): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/restore`
  )
  return res.data
}

export async function adjustReferralAffiliate(payload: {
  user_id: number
  amount: number
  remark?: string
  idempotency_key: string
}): Promise<ApiResponse<ReferralAffiliate>> {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${payload.user_id}/adjust`,
    payload,
    {
      headers: { 'Idempotency-Key': payload.idempotency_key },
    }
  )
  return res.data
}

export async function freezeReferralSettlement(
  userId: number,
  payload?: { reason?: string }
) {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/settlement/freeze`,
    payload || {}
  )
  return res.data
}

export async function restoreReferralSettlement(userId: number) {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/settlement/restore`
  )
  return res.data
}

export async function freezeReferralWithdrawal(
  userId: number,
  payload?: { reason?: string }
) {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/withdrawal/freeze`,
    payload || {}
  )
  return res.data
}

export async function restoreReferralWithdrawal(userId: number) {
  const res = await api.post(
    `/api/user/admin/referral/affiliates/${userId}/withdrawal/restore`
  )
  return res.data
}

export async function runReferralSettlementBatch(): Promise<
  ApiResponse<unknown>
> {
  const res = await api.post('/api/user/admin/referral/settlements/run')
  return res.data
}

export async function listAdminReferralWithdrawals(params: {
  p?: number
  page_size?: number
  status?: string
  affiliate_user_id?: number
}): Promise<ApiResponse<PaginatedData<ReferralWithdrawal>>> {
  const res = await api.get('/api/user/admin/referral/withdrawals', { params })
  return res.data
}

export async function approveReferralWithdrawal(
  id: number,
  payload?: { admin_note?: string }
) {
  const res = await api.post(
    `/api/user/admin/referral/withdrawals/${id}/approve`,
    payload || {}
  )
  return res.data
}

export async function rejectReferralWithdrawal(
  id: number,
  payload: {
    admin_note?: string
    reject_reason?: string
    reject_proof_url?: string
  }
) {
  const res = await api.post(
    `/api/user/admin/referral/withdrawals/${id}/reject`,
    payload
  )
  return res.data
}

export async function markReferralWithdrawalPaid(
  id: number,
  payload?: {
    admin_note?: string
    payment_proof_url?: string
    payment_txn_no?: string
  }
) {
  const res = await api.post(
    `/api/user/admin/referral/withdrawals/${id}/pay`,
    payload || {}
  )
  return res.data
}

export async function uploadAdminReferralAsset(
  file: File
): Promise<ApiResponse<{ url: string }>> {
  const formData = new FormData()
  formData.append('file', file)
  formData.append('purpose', 'payment_proof')
  const res = await api.post('/api/user/admin/referral/upload', formData, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return res.data
}
