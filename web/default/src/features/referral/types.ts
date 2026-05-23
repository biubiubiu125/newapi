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
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface PaginatedData<T> {
  page: number
  page_size: number
  total: number
  items: T[]
}

export interface ReferralProfile {
  id: number
  user_id: number
  invite_code: string
  status: string
  source_type: string
  applicant_note: string
  rate_override?: number | null
  acquisition_enabled: boolean
  settlement_enabled: boolean
  withdrawal_enabled: boolean
  risk_reason: string
  risk_note: string
  approved_at: number
  disabled_at: number
}

export type ReferralSectionId =
  | 'center'
  | 'commissions'
  | 'withdraw'
  | 'withdrawals'

export interface ReferralSummary {
  status: string
  invite_code: string
  rate?: number | null
  acquisition_enabled: boolean
  settlement_enabled: boolean
  withdrawal_enabled: boolean
  click_count: number
  bound_user_count: number
  paid_user_count: number
  pending_amount: number
  available_amount: number
  frozen_amount: number
  withdrawn_amount: number
  settlement_currency: string
  min_withdraw_amount: number
  settle_freeze_days: number
}

export interface ReferralCommission {
  id: number
  affiliate_id: number
  affiliate_user_id: number
  affiliate_username?: string
  affiliate_email?: string
  source_type: string
  source_order_id: number
  source_trade_no: string
  invitee_user_id: number
  invitee_username?: string
  invitee_email?: string
  order_type: string
  base_amount: number
  paid_amount: number
  paid_currency: string
  paid_amount_cny: number
  paid_cny_fx_rate: number
  paid_cny_fx_missing: boolean
  settlement_currency: string
  settlement_fx_rate: number
  settlement_base_amount: number
  rate: number
  commission_amount: number
  status: string
  settle_at: number
  available_at: number
  frozen_at: number
  created_at: number
}

export interface ReferralWithdrawal {
  id: number
  affiliate_id: number
  user_id: number
  username?: string
  email?: string
  settlement_currency: string
  amount: number
  fee_amount: number
  net_amount: number
  account_type: string
  account_name: string
  account_no: string
  account_no_masked: string
  account_network: string
  qr_image_url: string
  applicant_note: string
  admin_note: string
  payment_proof_url: string
  payment_txn_no: string
  reject_proof_url: string
  status: string
  reject_reason: string
  submitted_at: number
  approved_at: number
  payout_deadline_at: number
  paid_at: number
  rejected_at: number
  canceled_at: number
}

export interface ReferralAffiliate {
  id: number
  user_id: number
  username: string
  email: string
  invite_code: string
  status: string
  source_type: string
  applicant_note: string
  rate_override?: number | null
  rate?: number | null
  acquisition_enabled: boolean
  settlement_enabled: boolean
  withdrawal_enabled: boolean
  risk_reason: string
  risk_note: string
  click_count: number
  bound_user_count: number
  paid_user_count: number
  pending_amount: number
  available_amount: number
  frozen_amount: number
  withdrawn_amount: number
  settlement_currency: string
  approved_at: number
  disabled_at: number
  created_at: number
  updated_at: number
}

export interface ReferralBinding {
  id: number
  invitee_user_id: number
  invitee_username: string
  invitee_email: string
  bound_at: number
}

export interface ReferralCommissionJob {
  id: number
  source_type: string
  source_trade_no: string
  order_type: string
  order_trade_no: string
  order_exists: boolean
  order_label: string
  retry_source_type: string
  affiliate_id: number
  status: string
  attempt_count: number
  last_error: string
  locked_at: number
  succeeded_at: number
  failed_at: number
  updated_at: number
}

export interface RetryReferralCommissionJobResponse {
  source_type: string
  trade_no: string
}

export interface ReferralLedger {
  id: number
  affiliate_id: number
  user_id: number
  username?: string
  email?: string
  commission_id: number
  withdrawal_id: number
  type: string
  ref_type: string
  ref_id: string
  external_ref_id: string
  settlement_currency: string
  delta_pending: number
  delta_available: number
  delta_frozen: number
  delta_withdrawn: number
  remark: string
  operator: string
  created_at: number
}

export interface ReferralAdminAuditLog {
  id: number
  action: string
  target_user_id: number
  target_username?: string
  affiliate_id: number
  admin_user_id: number
  reason: string
  ip: string
  user_agent: string
  old_value: string
  new_value: string
  created_at: number
}

export interface ReferralOverview {
  total_affiliates: number
  pending_affiliates: number
  approved_affiliates: number
  disabled_affiliates: number
  referral_click_count: number
  bound_user_count: number
  effective_paid_user_count: number
  pending_amount: number
  available_amount: number
  frozen_amount: number
  withdrawn_amount: number
  settlement_currency: string
  failed_commission_job_count: number
}

export interface ReferralSettings {
  enabled: boolean
  cookie_ttl_days: number
  default_rate: number
  settle_freeze_days: number
  min_withdraw_amount: number
  withdraw_fee: number
  redirect_path: string
  require_approval: boolean
  settlement_currency: string
  settlement_fx_rates: string
}
