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
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestamp } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { AssetImagePreview } from '@/features/referral/components/asset-image-preview'
import {
  adjustReferralAffiliate,
  approveReferralAffiliate,
  approveReferralWithdrawal,
  backfillAdminReferralRedemptionJobs,
  disableReferralAffiliate,
  freezeReferralSettlement,
  freezeReferralWithdrawal,
  getAdminReferralOverview,
  getAdminReferralSettings,
  listAdminPendingReferralAffiliates,
  listAdminReferralAffiliates,
  listAdminReferralBindings,
  listAdminReferralCommissions,
  listAdminReferralCommissionJobs,
  listAdminReferralLedgers,
  listAdminReferralWithdrawals,
  listAdminReferralAuditLogs,
  markReferralWithdrawalPaid,
  rejectReferralAffiliate,
  rejectReferralWithdrawal,
  retryAdminReferralCommissionJob,
  restoreReferralAffiliate,
  restoreReferralSettlement,
  restoreReferralWithdrawal,
  runReferralSettlementBatch,
  uploadAdminReferralAsset,
  updateAdminReferralSettings,
  updateReferralAffiliateRate,
} from '@/features/referral/api'
import type {
  ReferralAffiliate,
  ReferralBinding,
  ReferralCommission,
  ReferralCommissionJob,
  ReferralLedger,
  ReferralOverview,
  ReferralSettings,
  ReferralWithdrawal,
  ReferralAdminAuditLog,
} from '@/features/referral/types'
import {
  formatAdminReferralBadgeCount,
  useAdminReferralBadges,
} from './hooks/use-admin-referral-badges'
import {
  ADMIN_REFERRAL_DEFAULT_SECTION,
  type AdminReferralSectionId,
  isAdminReferralSectionId,
} from './section-registry'

const route = getRouteApi('/_authenticated/admin-referral/$section')
const FIXED_REFERRAL_REDIRECT_PATH = '/sign-up'

const SECTION_META: Record<
  AdminReferralSectionId,
  { title: string; description: string }
> = {
  overview: {
    title: 'Referral Overview',
    description: 'Review platform-wide referral performance and balances',
  },
  settings: {
    title: 'Referral Settings',
    description: 'Configure referral rates, settlement, and withdrawal rules',
  },
  pending: {
    title: 'Pending Affiliates',
    description: 'Review and process pending affiliate applications',
  },
  affiliates: {
    title: 'Affiliates',
    description: 'Manage affiliates, bindings, and account controls',
  },
  commissions: {
    title: 'Referral Commissions',
    description: 'Review commission records and settlement status',
  },
  withdrawals: {
    title: 'Referral Withdrawals',
    description: 'Review withdrawal requests and payout status',
  },
  ledgers: {
    title: 'Referral Ledgers',
    description: 'Review account balance movements and settlement records',
  },
  'audit-logs': {
    title: 'Referral Audit Logs',
    description: 'Review administrator actions and audit trails',
  },
}

type PendingDecision =
  | { kind: 'approve'; item: ReferralAffiliate }
  | { kind: 'reject'; item: ReferralAffiliate }
  | null

type AffiliateAction =
  | { kind: 'disable'; item: ReferralAffiliate }
  | { kind: 'restore'; item: ReferralAffiliate }
  | { kind: 'freeze_settlement'; item: ReferralAffiliate }
  | { kind: 'restore_settlement'; item: ReferralAffiliate }
  | { kind: 'freeze_withdrawal'; item: ReferralAffiliate }
  | { kind: 'restore_withdrawal'; item: ReferralAffiliate }
  | { kind: 'update_rate'; item: ReferralAffiliate }
  | { kind: 'adjust_increase'; item: ReferralAffiliate }
  | { kind: 'adjust_decrease'; item: ReferralAffiliate }
  | null

type WithdrawalAction =
  | { kind: 'approve'; item: ReferralWithdrawal }
  | { kind: 'reject'; item: ReferralWithdrawal }
  | { kind: 'pay'; item: ReferralWithdrawal }
  | null

type AuditTimelineItem = {
  key: string
  time: number
  cells: ReactNode[]
  action?: ReactNode
}

function formatMoney(value: number): string {
  const amount = Number.isFinite(value) ? value : 0
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) >= 1 ? 2 : 4,
  }).format(amount)
  return `\u00a5${formatted}`
}

function formatSettlementMoney(value: number, currency?: string): string {
  const symbol =
    (currency || 'CNY').toUpperCase() === 'CNY'
      ? '\u00a5'
      : `${currency || ''} `
  const amount = Number.isFinite(value) ? value : 0
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) >= 1 ? 2 : 4,
  }).format(amount)
  return `${symbol}${formatted}`
}

function formatOriginalPaidAmount(item: ReferralCommission): string {
  const currency = (item.paid_currency || '-').toUpperCase()
  const amount = Number.isFinite(item.paid_amount) ? item.paid_amount : 0
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) >= 1 ? 2 : 4,
  }).format(amount)
  return `${currency} ${formatted}`
}

function formatPaidAmountCNY(
  item: ReferralCommission,
  t: (key: string) => string
): string {
  if (item.paid_cny_fx_missing) {
    return t('Missing referral FX rate')
  }
  return formatSettlementMoney(item.paid_amount_cny || 0, 'CNY')
}

function paidAmountDetail(
  item: ReferralCommission,
  t: (key: string) => string
): string {
  const currency = (item.paid_currency || '').toUpperCase()
  if (!currency) {
    return ''
  }
  const rateDetail =
    !item.paid_cny_fx_missing && item.paid_cny_fx_rate > 0
      ? ` · ${t('FX Rate')}: ${item.paid_cny_fx_rate}`
      : ''
  if (currency === 'CNY' && !rateDetail) {
    return ''
  }
  return `${t('Original paid')}: ${formatOriginalPaidAmount(item)}${rateDetail}`
}

function commissionJobDisplaySource(item: ReferralCommissionJob) {
  const orderType = item.order_type || item.source_type || ''
  const tradeNo = item.order_trade_no || item.source_trade_no || ''
  return {
    orderType,
    tradeNo,
    retrySourceType: item.retry_source_type || orderType || item.source_type,
  }
}

function parseOptionalNumber(value: string): number | undefined {
  const trimmed = value.trim()
  if (trimmed === '') {
    return undefined
  }
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : undefined
}

function parseRequiredNumber(value: string, fallback: number): number {
  const parsed = parseOptionalNumber(value)
  return parsed ?? fallback
}

function buildIdempotencyKey(): string {
  if (
    typeof crypto !== 'undefined' &&
    typeof crypto.randomUUID === 'function'
  ) {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function AdminReferralTabLabel(props: { label: string; badge?: string }) {
  return (
    <span className='inline-flex items-center gap-1.5'>
      <span>{props.label}</span>
      {props.badge && (
        <Badge
          variant='destructive'
          className='h-4 min-w-4 rounded-full px-1 text-[10px] leading-none tabular-nums'
        >
          {props.badge}
        </Badge>
      )}
    </span>
  )
}

function affiliateStatusLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'pending':
      return t('Pending')
    case 'approved':
      return t('Approved')
    case 'rejected':
      return t('Rejected')
    case 'disabled':
      return t('Disabled')
    default:
      return value || '-'
  }
}

function commissionStatusLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'pending':
      return t('Pending')
    case 'available':
      return t('Available')
    case 'frozen':
      return t('Frozen')
    case 'paid':
      return t('Paid')
    default:
      return value || '-'
  }
}

function commissionJobStatusLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'pending':
      return '待处理'
    case 'processing':
      return '处理中'
    case 'skipped':
      return '已跳过'
    case 'succeeded':
      return '已成功'
    case 'failed':
      return '失败'
    default:
      return value ? t(value) : '-'
  }
}

function referralErrorLabel(value: string, t: (key: string) => string): string {
  const normalized = (value || '').trim()
  if (!normalized) {
    return '-'
  }
  switch (normalized) {
    case 'fx_rate_missing':
      return '返佣汇率缺失'
    case 'missing_referral_snapshot':
      return '订单没有返佣快照，已跳过佣金生成'
    case 'zero_commission_amount':
      return '佣金金额为 0，已跳过佣金生成'
    case 'affiliate_not_eligible':
      return '推广员未通过审核或结算已关闭'
    case 'unsupported source_type':
      return '不支持的订单来源类型'
    case 'trade_no is required':
      return '缺少订单号'
    case 'failed to update referral pending amount':
      return '更新推广员待结算余额失败'
    case 'record not found':
      return '关联记录不存在'
    case 'subscription order not found':
      return '订阅订单不存在'
    case 'topup order not found':
      return '充值订单不存在'
    case 'duplicate_job_superseded_by_subscription':
      return '同一订单已按订阅订单重新生成佣金'
    case 'paid_amount must be a positive finite number':
      return '实付金额必须大于 0'
    default:
      if (normalized.includes('UNIQUE constraint failed')) {
        return '佣金记录已存在或唯一约束冲突'
      }
      if (normalized.includes('duplicate key value')) {
        return '佣金记录已存在或唯一约束冲突'
      }
      if (normalized.includes('record not found')) {
        return '关联记录不存在'
      }
      if (normalized.includes('subscription order not found')) {
        return '订阅订单不存在'
      }
      if (normalized.includes('topup order not found')) {
        return '充值订单不存在'
      }
      return normalized || t('Unknown error')
  }
}

function commissionJobSourceLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'topup':
      return t('Top-up')
    case 'subscription':
      return t('Subscription')
    case 'redemption':
      return t('Redemption Code')
    default:
      return value || '-'
  }
}

function withdrawalStatusLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'pending':
      return t('Pending')
    case 'approved':
      return t('Approved')
    case 'paid':
      return t('Paid')
    case 'rejected':
      return t('Rejected')
    case 'canceled':
      return t('Canceled')
    default:
      return value || '-'
  }
}

function ledgerTypeLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'commission_settle':
      return t('Commission Settlement')
    case 'withdrawal_paid':
      return t('Withdrawal Paid')
    case 'withdrawal_approve':
      return t('Withdrawal Approved')
    case 'withdrawal_freeze':
      return t('Withdrawal Frozen')
    case 'commission_adjust_increase':
      return t('Commission Adjustment Increase')
    case 'commission_adjust_decrease':
      return t('Commission Adjustment Decrease')
    case 'commission_accrue':
      return t('Commission Accrual')
    default:
      return value || '-'
  }
}

function operatorLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'system':
      return t('System')
    case 'admin':
      return t('Admin')
    case 'user':
      return t('User')
    default:
      return value || '-'
  }
}

function orderTypeLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'topup':
      return t('Top-up')
    case 'subscription':
      return t('Subscription')
    case 'redemption':
      return t('Redemption Code')
    default:
      return value || '-'
  }
}

function auditReasonLabel(value: string): string {
  const normalized = (value || '').trim()
  if (!normalized) {
    return '-'
  }
  const lower = normalized.toLowerCase()
  switch (lower) {
    case '':
      return '-'
    case 'settings updated':
      return '设置已更新'
    case 'batch approve':
      return '批量审核通过'
    case 'batch paid':
      return '批量标记已打款'
    case 'ui button smoke test':
      return '界面按钮冒烟测试'
    case 'approve e2e':
      return '审核端到端测试'
    case 'paid e2e':
      return '打款端到端测试'
    case 'approve flow':
      return '审核流程'
    case 'paid flow':
      return '打款流程'
    case 'withdrawal flow':
      return '提现流程'
    case 'e2e':
      return '端到端测试'
    case 'test reject release':
      return '测试拒绝后释放冻结金额'
    case 'test machine withdrawal':
      return '测试机提现申请'
    case 'paid in test-machine chain':
      return '测试机链路已打款'
    case 'approved in test-machine chain':
      return '测试机链路审核通过'
    case 'test machine paid':
      return '测试机打款测试'
    case 'test machine approve':
      return '测试机审核测试'
    case 'user canceled within 30 minutes':
    case 'user cancelled within 30 minutes':
      return '用户在 30 分钟内取消'
    case 'user canceled':
    case 'user cancelled':
      return '用户取消'
    case 'manual retry':
    case 'retry':
      return '手动重试'
    case 'fx_rate_missing':
      return '返佣结算汇率缺失'
    case 'missing_referral_snapshot':
      return '订单缺少推广快照'
    case 'zero_commission_amount':
      return '佣金金额为 0'
    case 'affiliate_not_eligible':
      return '推广员当前不可结算'
    case 'affiliate_not_found':
      return '推广员不存在'
    case 'affiliate_not_approved':
      return '推广员未审核通过'
    case 'affiliate_acquisition_disabled':
      return '推广员拉新已冻结'
    case 'affiliate_settlement_disabled':
      return '推广员结算已冻结'
    case 'no_binding':
      return '订单用户没有有效邀请绑定'
    case 'invalid_rate':
      return '返佣比例无效'
    case 'record not found':
      return '关联记录不存在'
    case 'subscription order not found':
      return '订阅订单不存在'
    case 'topup order not found':
      return '充值订单不存在'
    case 'duplicate_job_superseded_by_subscription':
      return '同一订单已按订阅订单重新生成佣金'
    default:
      return auditReasonPatternLabel(normalized)
  }
}

function auditReasonPatternLabel(value: string): string {
  const lower = value.toLowerCase()
  if (lower.includes('reject') && lower.includes('release')) {
    return '拒绝后释放冻结金额'
  }
  if (lower.includes('approved') || lower.includes('approve')) {
    return lower.includes('test')
      ? '测试链路审核通过'
      : '审核通过'
  }
  if (lower.includes('paid') || lower.includes('payment')) {
    return lower.includes('test')
      ? '测试链路已打款'
      : '已打款'
  }
  if (lower.includes('withdrawal')) {
    return lower.includes('test') ? '测试提现流程' : '提现流程'
  }
  if (lower.includes('canceled') || lower.includes('cancelled')) {
    return lower.includes('30 minutes') ? '用户在 30 分钟内取消' : '用户取消'
  }
  if (lower.includes('test-machine') || lower.includes('test machine')) {
    return `测试机记录：${value.replace(/test-machine|test machine/gi, '').trim() || value}`
  }
  return value || '-'
}

function auditActionLabel(value: string): string {
  switch (value) {
    case 'referral_affiliate_approve':
      return '推广员审核通过'
    case 'referral_affiliate_reject':
      return '推广员审核拒绝'
    case 'referral_affiliate_disable':
      return '推广员已禁用'
    case 'referral_affiliate_restore':
      return '推广员已恢复'
    case 'referral_affiliate_adjust':
      return '推广员余额调整'
    case 'referral_affiliate_rate':
      return '推广员比例更新'
    case 'referral_withdrawal_create':
      return '提现申请创建'
    case 'referral_withdrawal_cancel':
      return '提现申请取消'
    case 'referral_withdrawal_approve':
      return '提现审核通过'
    case 'referral_withdrawal_reject':
      return '提现审核拒绝'
    case 'referral_withdrawal_paid':
      return '提现已打款'
    case 'referral_settings_update':
      return '推广设置更新'
    case 'referral_withdrawal_freeze':
      return '提现已冻结'
    case 'referral_withdrawal_restore':
      return '提现已恢复'
    case 'referral_settlement_freeze':
      return '结算已冻结'
    case 'referral_settlement_restore':
      return '结算已恢复'
    default:
      return value || '-'
  }
}

export function AdminReferral() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeSection: AdminReferralSectionId =
    params.section && isAdminReferralSectionId(params.section)
      ? params.section
      : ADMIN_REFERRAL_DEFAULT_SECTION

  const [loading, setLoading] = useState(true)
  const [overview, setOverview] = useState<ReferralOverview | null>(null)
  const [settings, setSettings] = useState<ReferralSettings | null>(null)
  const [pendingItems, setPendingItems] = useState<ReferralAffiliate[]>([])
  const [affiliateItems, setAffiliateItems] = useState<ReferralAffiliate[]>([])
  const [bindingItems, setBindingItems] = useState<ReferralBinding[]>([])
  const [commissionItems, setCommissionItems] = useState<ReferralCommission[]>(
    []
  )
  const [commissionJobs, setCommissionJobs] = useState<ReferralCommissionJob[]>(
    []
  )
  const [withdrawalItems, setWithdrawalItems] = useState<ReferralWithdrawal[]>(
    []
  )
  const [ledgerItems, setLedgerItems] = useState<ReferralLedger[]>([])
  const [auditLogItems, setAuditLogItems] = useState<ReferralAdminAuditLog[]>(
    []
  )
  const [affiliateKeyword, setAffiliateKeyword] = useState('')
  const [commissionStatus, setCommissionStatus] = useState('')
  const [withdrawalStatus, setWithdrawalStatus] = useState('')
  const [ledgerKeyword, setLedgerKeyword] = useState('')
  const [auditKeyword, setAuditKeyword] = useState('')
  const [redemptionBackfillCursorId, setRedemptionBackfillCursorId] =
    useState(0)
  const [pendingDecision, setPendingDecision] = useState<PendingDecision>(null)
  const [affiliateAction, setAffiliateAction] = useState<AffiliateAction>(null)
  const [withdrawalAction, setWithdrawalAction] =
    useState<WithdrawalAction>(null)
  const [reasonInput, setReasonInput] = useState('')
  const [rateOverrideInput, setRateOverrideInput] = useState('')
  const [paymentTxnNo, setPaymentTxnNo] = useState('')
  const [paymentProofURL, setPaymentProofURL] = useState('')
  const [rejectProofURL, setRejectProofURL] = useState('')
  const [adminNote, setAdminNote] = useState('')
  const [runningSettlement, setRunningSettlement] = useState(false)
  const [savingSettings, setSavingSettings] = useState(false)
  const [adjustAmountInput, setAdjustAmountInput] = useState('')
  const [detailAffiliate, setDetailAffiliate] =
    useState<ReferralAffiliate | null>(null)
  const [detailMode, setDetailMode] = useState<
    'bindings' | 'commissions' | 'withdrawals' | null
  >(null)
  const { counts: badgeCounts, refetch: refetchBadges } =
    useAdminReferralBadges()

  const pageMeta = SECTION_META[activeSection]
  const pendingAffiliateBadge = formatAdminReferralBadgeCount(
    badgeCounts.pendingAffiliates
  )
  const pendingWithdrawalBadge = formatAdminReferralBadgeCount(
    badgeCounts.pendingWithdrawals
  )

  async function loadOverview() {
    const res = await getAdminReferralOverview()
    setOverview((res.data as ReferralOverview | null) || null)
  }

  async function loadSettings() {
    const res = await getAdminReferralSettings()
    setSettings((res.data as ReferralSettings | null) || null)
  }

  async function loadPending() {
    const res = await listAdminPendingReferralAffiliates({
      p: 1,
      page_size: 50,
      keyword: affiliateKeyword.trim() || undefined,
    })
    setPendingItems(res.data?.items || [])
  }

  async function loadAffiliates() {
    const res = await listAdminReferralAffiliates({
      p: 1,
      page_size: 50,
      keyword: affiliateKeyword.trim() || undefined,
    })
    setAffiliateItems(res.data?.items || [])
  }

  async function loadCommissions() {
    const commissionRes = await listAdminReferralCommissions({
      p: 1,
      page_size: 50,
      status: commissionStatus || undefined,
    })
    setCommissionItems(commissionRes.data?.items || [])
  }

  async function loadCommissionJobs() {
    const jobsRes = await listAdminReferralCommissionJobs({
      p: 1,
      page_size: 50,
    })
    setCommissionJobs(jobsRes.data?.items || [])
  }

  async function loadWithdrawals() {
    const res = await listAdminReferralWithdrawals({
      p: 1,
      page_size: 50,
      status: withdrawalStatus || undefined,
    })
    setWithdrawalItems(res.data?.items || [])
  }

  async function loadAffiliateBindings(userId: number) {
    const res = await listAdminReferralBindings(userId, {
      p: 1,
      page_size: 100,
    })
    setBindingItems(res.data?.items || [])
  }

  async function loadLedgers() {
    const res = await listAdminReferralLedgers({
      p: 1,
      page_size: 50,
      keyword: ledgerKeyword.trim() || undefined,
    })
    setLedgerItems(res.data?.items || [])
  }

  async function loadAuditLogs() {
    const res = await listAdminReferralAuditLogs({
      p: 1,
      page_size: 50,
      keyword: auditKeyword.trim() || undefined,
    })
    setAuditLogItems(res.data?.items || [])
  }

  const handleRetryCommissionJob = useCallback(
    async (item: ReferralCommissionJob) => {
      const displaySource = commissionJobDisplaySource(item)
      const res = await retryAdminReferralCommissionJob({
        source_type: displaySource.retrySourceType,
        trade_no: displaySource.tradeNo,
      })
      if (!res.success) {
        toast.error(res.message || t('Retry failed'))
        return
      }
      toast.success(t('Commission retry submitted'))
      const [jobsRes, overviewRes] = await Promise.all([
        listAdminReferralCommissionJobs({
          p: 1,
          page_size: 50,
        }),
        getAdminReferralOverview(),
      ])
      setCommissionJobs(jobsRes.data?.items || [])
      setOverview((overviewRes.data as ReferralOverview | null) || null)
    },
    [t]
  )

  const handleBackfillRedemptionJobs = useCallback(async () => {
    const res = await backfillAdminReferralRedemptionJobs({
      limit: 200,
      succeeded_cursor_id: redemptionBackfillCursorId || undefined,
      succeeded_scan_limit: 1000,
    })
    if (!res.success) {
      toast.error(res.message || t('Backfill failed'))
      return
    }
    const result = res.data
    const nextCursor = result?.has_more_succeeded
      ? result.next_succeeded_cursor_id || 0
      : 0
    setRedemptionBackfillCursorId(nextCursor)
    toast.success(
      `${t('Redemption commission backfill finished')}: ${t('Scanned')} ${
        result?.scanned || 0
      }, ${t('Processed')} ${result?.processed || 0}, ${t('Failed')} ${
        result?.failed || 0
      }, ${t('Succeeded scanned')} ${result?.succeeded_scanned || 0}${
        result?.has_more_succeeded ? `, ${t('Click again to continue')}` : ''
      }`
    )
    const [jobsRes, overviewRes, auditRes] = await Promise.all([
      listAdminReferralCommissionJobs({
        p: 1,
        page_size: 50,
      }),
      getAdminReferralOverview(),
      listAdminReferralAuditLogs({
        p: 1,
        page_size: 50,
        keyword: auditKeyword.trim() || undefined,
      }),
    ])
    setCommissionJobs(jobsRes.data?.items || [])
    setOverview((overviewRes.data as ReferralOverview | null) || null)
    setAuditLogItems(auditRes.data?.items || [])
  }, [auditKeyword, redemptionBackfillCursorId, t])

  const auditTimelineRows = useMemo<AuditTimelineItem[]>(() => {
    const rows: AuditTimelineItem[] = []
    for (const item of auditLogItems) {
      rows.push({
        key: `audit-${item.id}`,
        time: item.created_at,
        cells: [
          '管理员操作',
          auditActionLabel(item.action),
          item.admin_user_id > 0 ? `管理员 #${item.admin_user_id}` : '-',
          item.target_username ||
            (item.target_user_id > 0 ? `#${item.target_user_id}` : '-'),
          auditReasonLabel(item.reason),
          formatTimestamp(item.created_at),
        ],
      })
    }
    for (const item of commissionJobs) {
      const time =
        item.failed_at || item.succeeded_at || item.updated_at || item.locked_at
      const displaySource = commissionJobDisplaySource(item)
      const detailParts = [
        `${commissionJobSourceLabel(displaySource.orderType, t)} ${displaySource.tradeNo || '-'}`,
        `推广员 #${item.affiliate_id || '-'}`,
      ]
      if (item.order_label) {
        detailParts.push(item.order_label)
      }
      if (!item.order_exists) {
        detailParts.push('订单未找到')
      }
      if (
        item.source_type &&
        item.source_trade_no &&
        (item.source_type !== displaySource.orderType ||
          item.source_trade_no !== displaySource.tradeNo)
      ) {
        detailParts.push(
          `任务来源 ${commissionJobSourceLabel(item.source_type, t)} ${item.source_trade_no}`
        )
      }
      if (item.attempt_count > 0) {
        detailParts.push(`尝试 ${item.attempt_count} 次`)
      }
      rows.push({
        key: `commission-job-${item.id}`,
        time,
        cells: [
          '佣金任务',
          '佣金生成任务',
          '系统',
          detailParts.join(' / '),
          item.last_error
            ? referralErrorLabel(item.last_error, t)
            : commissionJobStatusLabel(item.status, t),
          formatTimestamp(time),
        ],
        action:
          item.status === 'failed' ? (
            <Button
              size='sm'
              variant='outline'
              onClick={() => void handleRetryCommissionJob(item)}
            >
              重试生成佣金
            </Button>
          ) : undefined,
      })
    }
    const keyword = auditKeyword.trim().toLowerCase()
    const filtered = keyword
      ? rows.filter((row) =>
          row.cells.some((cell) =>
            String(cell || '')
              .toLowerCase()
              .includes(keyword)
          )
        )
      : rows
    return filtered.sort((a, b) => b.time - a.time)
  }, [auditKeyword, auditLogItems, commissionJobs, handleRetryCommissionJob, t])

  const loadCurrentSection = useEffectEvent(
    async function loadCurrentSection() {
      setLoading(true)
      try {
        if (activeSection === 'overview') await loadOverview()
        if (activeSection === 'settings') await loadSettings()
        if (activeSection === 'pending') await loadPending()
        if (activeSection === 'affiliates') await loadAffiliates()
        if (activeSection === 'commissions') await loadCommissions()
        if (activeSection === 'withdrawals') await loadWithdrawals()
        if (activeSection === 'ledgers') await loadLedgers()
        if (activeSection === 'audit-logs') {
          await loadAuditLogs()
          await loadCommissionJobs()
        }
      } finally {
        setLoading(false)
      }
    }
  )

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadCurrentSection()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [activeSection])

  async function handleRunSettlement() {
    setRunningSettlement(true)
    try {
      const res = await runReferralSettlementBatch()
      if (res.success) {
        toast.success(t('Settlement batch completed'))
        await loadOverview()
        if (activeSection === 'commissions') {
          await loadCommissions()
        } else if (activeSection === 'audit-logs') {
          await loadAuditLogs()
          await loadCommissionJobs()
        }
      } else {
        toast.error(res.message || t('Settlement run failed'))
      }
    } finally {
      setRunningSettlement(false)
    }
  }

  async function handleSaveSettings() {
    if (!settings) return
    setSavingSettings(true)
    try {
      const res = await updateAdminReferralSettings({
        ...settings,
        redirect_path: FIXED_REFERRAL_REDIRECT_PATH,
      })
      if (res.success && res.data) {
        toast.success(t('Settings saved'))
        setSettings(res.data)
      } else {
        toast.error(res.message || t('Failed to save settings'))
      }
    } finally {
      setSavingSettings(false)
    }
  }

  async function handlePendingDecision() {
    if (!pendingDecision) return
    const { item } = pendingDecision
    if (pendingDecision.kind === 'approve') {
      const rateOverride = parseOptionalNumber(rateOverrideInput)
      if (rateOverrideInput.trim() !== '' && rateOverride === undefined) {
        toast.error(t('Please enter a valid rate'))
        return
      }
      const res = await approveReferralAffiliate(item.user_id, {
        rate_override: rateOverride,
        reason: reasonInput.trim(),
      })
      if (!res.success) {
        toast.error(res.message || t('Approve failed'))
        return
      }
      toast.success(t('Affiliate approved'))
    } else {
      const res = await rejectReferralAffiliate(item.user_id, {
        reason: reasonInput.trim(),
      })
      if (!res.success) {
        toast.error(res.message || t('Reject failed'))
        return
      }
      toast.success(t('Affiliate rejected'))
    }
    setPendingDecision(null)
    setReasonInput('')
    setRateOverrideInput('')
    await loadPending()
    await loadAffiliates()
    void refetchBadges()
  }

  async function handleAffiliateAction() {
    if (!affiliateAction) return
    const { item } = affiliateAction
    let success = false
    let message = ''
    switch (affiliateAction.kind) {
      case 'disable': {
        const res = await disableReferralAffiliate(item.user_id, {
          reason: reasonInput.trim(),
        })
        success = res.success
        message = res.message || t('Disable failed')
        break
      }
      case 'restore': {
        const res = await restoreReferralAffiliate(item.user_id)
        success = res.success
        message = res.message || t('Restore failed')
        break
      }
      case 'freeze_settlement': {
        const res = await freezeReferralSettlement(item.user_id, {
          reason: reasonInput.trim(),
        })
        success = res.success
        message = res.message || t('Settlement freeze failed')
        break
      }
      case 'restore_settlement': {
        const res = await restoreReferralSettlement(item.user_id)
        success = res.success
        message = res.message || t('Settlement restore failed')
        break
      }
      case 'freeze_withdrawal': {
        const res = await freezeReferralWithdrawal(item.user_id, {
          reason: reasonInput.trim(),
        })
        success = res.success
        message = res.message || t('Withdrawal freeze failed')
        break
      }
      case 'restore_withdrawal': {
        const res = await restoreReferralWithdrawal(item.user_id)
        success = res.success
        message = res.message || t('Withdrawal restore failed')
        break
      }
      case 'update_rate': {
        const value = parseOptionalNumber(rateOverrideInput)
        if (rateOverrideInput.trim() !== '' && value === undefined) {
          toast.error(t('Please enter a valid rate'))
          return
        }
        const res = await updateReferralAffiliateRate(item.user_id, {
          rate_override: value,
          reason: reasonInput.trim(),
        })
        success = res.success
        message = res.message || t('Rate update failed')
        break
      }
      case 'adjust_increase':
      case 'adjust_decrease': {
        const amount = parseOptionalNumber(adjustAmountInput)
        if (amount === undefined || amount <= 0) {
          toast.error(t('Please enter a valid amount'))
          return
        }
        const signedAmount =
          affiliateAction.kind === 'adjust_increase' ? amount : -amount
        const res = await adjustReferralAffiliate({
          user_id: item.user_id,
          amount: signedAmount,
          remark: reasonInput.trim(),
          idempotency_key: buildIdempotencyKey(),
        })
        success = res.success
        message = res.message || t('Adjust failed')
        break
      }
    }

    if (!success) {
      toast.error(message)
      return
    }
    toast.success(t('Affiliate updated'))
    setAffiliateAction(null)
    setReasonInput('')
    setRateOverrideInput('')
    setAdjustAmountInput('')
    await loadAffiliates()
    if (detailAffiliate?.user_id === item.user_id) {
      const refreshed = await listAdminReferralAffiliates({
        p: 1,
        page_size: 1,
        keyword: item.username || item.invite_code,
      })
      const latest = refreshed.data?.items?.find(
        (candidate) => candidate.user_id === item.user_id
      )
      if (latest) {
        setDetailAffiliate(latest)
      }
    }
  }

  async function handleWithdrawalAction() {
    if (!withdrawalAction) return
    const { item } = withdrawalAction
    let res: { success: boolean; message?: string } | undefined
    switch (withdrawalAction.kind) {
      case 'approve':
        res = await approveReferralWithdrawal(item.id, {
          admin_note: adminNote.trim(),
        })
        break
      case 'reject':
        res = await rejectReferralWithdrawal(item.id, {
          admin_note: adminNote.trim(),
          reject_reason: reasonInput.trim(),
          reject_proof_url: rejectProofURL.trim(),
        })
        break
      case 'pay':
        res = await markReferralWithdrawalPaid(item.id, {
          admin_note: adminNote.trim(),
          payment_txn_no: paymentTxnNo.trim(),
          payment_proof_url: paymentProofURL.trim(),
        })
        break
    }

    if (!res?.success) {
      toast.error(res?.message || t('Withdrawal action failed'))
      return
    }
    toast.success(t('Withdrawal updated'))
    setWithdrawalAction(null)
    setReasonInput('')
    setAdminNote('')
    setPaymentTxnNo('')
    setPaymentProofURL('')
    setRejectProofURL('')
    await loadWithdrawals()
    await loadLedgers()
    await loadAuditLogs()
    void refetchBadges()
    if (detailMode === 'withdrawals' && detailAffiliate) {
      const res = await listAdminReferralWithdrawals({
        p: 1,
        page_size: 100,
        affiliate_user_id: detailAffiliate.user_id,
      })
      setWithdrawalItems(res.data?.items || [])
    }
  }

  async function handleUploadPaymentProof(
    event: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = event.target.files?.[0]
    if (!file) return
    try {
      const res = await uploadAdminReferralAsset(file)
      if (res.success && res.data?.url) {
        setPaymentProofURL(res.data.url)
        toast.success(t('Payment proof uploaded'))
      } else {
        toast.error(res.message || t('Upload failed'))
      }
    } finally {
      event.target.value = ''
    }
  }

  async function handleUploadRejectProof(
    event: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = event.target.files?.[0]
    if (!file) return
    try {
      const res = await uploadAdminReferralAsset(file)
      if (res.success && res.data?.url) {
        setRejectProofURL(res.data.url)
        toast.success(t('Reject proof uploaded'))
      } else {
        toast.error(res.message || t('Upload failed'))
      }
    } finally {
      event.target.value = ''
    }
  }

  async function openAffiliateDetail(
    item: ReferralAffiliate,
    mode: 'bindings' | 'commissions' | 'withdrawals'
  ) {
    setDetailAffiliate(item)
    setDetailMode(mode)
    if (mode === 'bindings') {
      await loadAffiliateBindings(item.user_id)
      return
    }
    if (mode === 'commissions') {
      const res = await listAdminReferralCommissions({
        p: 1,
        page_size: 100,
        affiliate_user_id: item.user_id,
      })
      setCommissionItems(res.data?.items || [])
      return
    }
    const res = await listAdminReferralWithdrawals({
      p: 1,
      page_size: 100,
      affiliate_user_id: item.user_id,
    })
    setWithdrawalItems(res.data?.items || [])
  }

  const actionTitle = (() => {
    if (pendingDecision?.kind === 'approve') return t('Approve affiliate?')
    if (pendingDecision?.kind === 'reject') return t('Reject affiliate?')
    switch (affiliateAction?.kind) {
      case 'disable':
        return t('Disable affiliate?')
      case 'restore':
        return t('Restore affiliate?')
      case 'freeze_settlement':
        return t('Freeze settlement?')
      case 'restore_settlement':
        return t('Restore settlement?')
      case 'freeze_withdrawal':
        return t('Freeze withdrawal?')
      case 'restore_withdrawal':
        return t('Restore withdrawal?')
      case 'update_rate':
        return t('Update affiliate rate?')
      default:
        return t('Confirm action')
    }
  })()

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t(pageMeta.title)}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(pageMeta.description)}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          {activeSection === 'overview' && (
            <Button
              onClick={() => void handleRunSettlement()}
              disabled={runningSettlement}
            >
              {runningSettlement ? t('Running...') : t('Run Settlement')}
            </Button>
          )}
          {activeSection === 'settings' && (
            <Button
              onClick={() => void handleSaveSettings()}
              disabled={savingSettings}
            >
              {savingSettings ? t('Saving...') : t('Save Settings')}
            </Button>
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            <Tabs
              value={activeSection}
              onValueChange={(section) =>
                void navigate({
                  to: '/admin-referral/$section',
                  params: { section: section as AdminReferralSectionId },
                })
              }
            >
              <TabsList className='h-auto flex-wrap justify-start'>
                {(
                  [
                    'overview',
                    'settings',
                    'pending',
                    'affiliates',
                    'commissions',
                    'withdrawals',
                    'ledgers',
                    'audit-logs',
                  ] as AdminReferralSectionId[]
                ).map((section) => (
                  <TabsTrigger key={section} value={section}>
                    <AdminReferralTabLabel
                      label={t(SECTION_META[section].title)}
                      badge={
                        section === 'pending'
                          ? pendingAffiliateBadge
                          : section === 'withdrawals'
                            ? pendingWithdrawalBadge
                            : undefined
                      }
                    />
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>

            {loading ? (
              <Card>
                <CardContent className='text-muted-foreground py-10 text-sm'>
                  {t('Loading...')}
                </CardContent>
              </Card>
            ) : activeSection === 'overview' && overview ? (
              <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
                <MetricCard
                  title={t('Total Affiliates')}
                  value={String(overview.total_affiliates)}
                />
                <MetricCard
                  title={t('Pending Affiliates')}
                  value={String(overview.pending_affiliates)}
                />
                <MetricCard
                  title={t('Approved Affiliates')}
                  value={String(overview.approved_affiliates)}
                />
                <MetricCard
                  title={t('Referral Link Clicks')}
                  value={String(overview.referral_click_count)}
                />
                <MetricCard
                  title={t('Bound Users')}
                  value={String(overview.bound_user_count)}
                />
                <MetricCard
                  title={t('Paid Users')}
                  value={String(overview.effective_paid_user_count)}
                />
                <MetricCard
                  title={t('Pending Amount')}
                  value={formatMoney(overview.pending_amount)}
                />
                <MetricCard
                  title={t('Available Amount')}
                  value={formatMoney(overview.available_amount)}
                />
                <MetricCard
                  title={t('Frozen Amount')}
                  value={formatMoney(overview.frozen_amount)}
                />
                <MetricCard
                  title={t('Withdrawn Amount')}
                  value={formatMoney(overview.withdrawn_amount)}
                />
                <MetricCard
                  title={t('Failed Jobs')}
                  value={String(overview.failed_commission_job_count)}
                />
              </div>
            ) : activeSection === 'settings' && settings ? (
              <Card>
                <CardHeader>
                  <CardTitle>{t('Referral Settings')}</CardTitle>
                </CardHeader>
                <CardContent className='grid gap-4 md:grid-cols-2'>
                  <SettingSwitch
                    label={t('Enabled')}
                    checked={settings.enabled}
                    onCheckedChange={(checked) =>
                      setSettings((prev) =>
                        prev ? { ...prev, enabled: checked } : prev
                      )
                    }
                  />
                  <SettingSwitch
                    label={t('Require Approval')}
                    checked={settings.require_approval}
                    onCheckedChange={(checked) =>
                      setSettings((prev) =>
                        prev ? { ...prev, require_approval: checked } : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Cookie TTL Days')}
                    value={String(settings.cookie_ttl_days)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              cookie_ttl_days: parseRequiredNumber(
                                value,
                                prev.cookie_ttl_days
                              ),
                            }
                          : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Default Rate')}
                    value={String(settings.default_rate)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              default_rate: parseRequiredNumber(
                                value,
                                prev.default_rate
                              ),
                            }
                          : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Settlement Freeze Days')}
                    value={String(settings.settle_freeze_days)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              settle_freeze_days: parseRequiredNumber(
                                value,
                                prev.settle_freeze_days
                              ),
                            }
                          : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Min Withdraw Amount')}
                    value={String(settings.min_withdraw_amount)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              min_withdraw_amount: parseRequiredNumber(
                                value,
                                prev.min_withdraw_amount
                              ),
                            }
                          : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Withdraw Fee')}
                    value={String(settings.withdraw_fee)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              withdraw_fee: parseRequiredNumber(
                                value,
                                prev.withdraw_fee
                              ),
                            }
                          : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Redirect Path')}
                    description={t(
                      'Referral links will open this internal frontend path with ?aff=invite_code appended. Use /sign-up for the default template. /register is only kept as a legacy compatibility redirect.'
                    )}
                    value={FIXED_REFERRAL_REDIRECT_PATH}
                    readOnly
                    onChange={() => undefined}
                  />
                  <LabeledInput
                    label={t('Referral Settlement Currency')}
                    value={settings.settlement_currency || 'CNY'}
                    readOnly
                    onChange={() => undefined}
                  />
                  <LabeledTextarea
                    label={t('Referral FX Rates')}
                    description={t(
                      'JSON object for converting the order paid currency to CNY before commission settlement, for example {"USD":7.2,"EUR":7.8}. CNY is always 1. BEpusdt orders are currently recorded as CNY, so {"CNY":1} is enough unless other currencies are enabled.'
                    )}
                    value={settings.settlement_fx_rates || '{"CNY":1}'}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev ? { ...prev, settlement_fx_rates: value } : prev
                      )
                    }
                  />
                  <LabeledInput
                    label={t('Redemption Code Exchange Rate')}
                    description={t(
                      'How many CNY one USD quota from a redemption code counts as for referral commission.'
                    )}
                    value={String(settings.redemption_usd_to_cny_rate)}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              redemption_usd_to_cny_rate: parseRequiredNumber(
                                value,
                                prev.redemption_usd_to_cny_rate
                              ),
                            }
                          : prev
                      )
                    }
                  />
                </CardContent>
              </Card>
            ) : activeSection === 'pending' ? (
              <>
                <Toolbar
                  keyword={affiliateKeyword}
                  keywordPlaceholder={t('Search username or invite code')}
                  onKeywordChange={setAffiliateKeyword}
                  onSearch={() => void loadPending()}
                />
                <SimpleAdminTable
                  headers={[
                    t('Username'),
                    t('Invite Code'),
                    t('Applicant Note'),
                    t('Submitted'),
                    t('Action'),
                  ]}
                  rows={pendingItems.map((item) => ({
                    key: item.id,
                    cells: [
                      item.username,
                      item.invite_code,
                      item.applicant_note || '-',
                      formatTimestamp(item.created_at),
                    ],
                    action: (
                      <div className='flex gap-2'>
                        <Button
                          size='sm'
                          onClick={() => {
                            setPendingDecision({ kind: 'approve', item })
                            setReasonInput('')
                            setRateOverrideInput(
                              item.rate_override == null
                                ? ''
                                : String(item.rate_override)
                            )
                          }}
                        >
                          {t('Approve')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setPendingDecision({ kind: 'reject', item })
                            setReasonInput(item.risk_reason || '')
                            setRateOverrideInput('')
                          }}
                        >
                          {t('Reject')}
                        </Button>
                      </div>
                    ),
                  }))}
                />
              </>
            ) : activeSection === 'affiliates' ? (
              <>
                <Toolbar
                  keyword={affiliateKeyword}
                  keywordPlaceholder={t('Search username or invite code')}
                  onKeywordChange={setAffiliateKeyword}
                  onSearch={() => void loadAffiliates()}
                />
                <SimpleAdminTable
                  headers={[
                    t('Username'),
                    t('Invite Code'),
                    t('Referral Link Clicks'),
                    t('Rate'),
                    t('Available'),
                    t('Status'),
                    t('Action'),
                  ]}
                  rows={affiliateItems.map((item) => ({
                    key: item.id,
                    cells: [
                      item.username,
                      item.invite_code,
                      String(item.click_count || 0),
                      item.rate != null ? `${item.rate}%` : '-',
                      formatMoney(item.available_amount),
                      affiliateStatusLabel(item.status, t),
                    ],
                    action: (
                      <div className='flex flex-wrap gap-2'>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({
                              kind:
                                item.status === 'approved'
                                  ? 'disable'
                                  : 'restore',
                              item,
                            })
                            setReasonInput(item.risk_reason || '')
                            setRateOverrideInput('')
                          }}
                        >
                          {item.status === 'approved'
                            ? t('Disable')
                            : t('Restore')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({
                              kind: item.settlement_enabled
                                ? 'freeze_settlement'
                                : 'restore_settlement',
                              item,
                            })
                            setReasonInput(item.risk_reason || '')
                            setRateOverrideInput('')
                          }}
                        >
                          {item.settlement_enabled
                            ? t('Freeze Settlement')
                            : t('Restore Settlement')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({
                              kind: item.withdrawal_enabled
                                ? 'freeze_withdrawal'
                                : 'restore_withdrawal',
                              item,
                            })
                            setReasonInput(item.risk_reason || '')
                            setRateOverrideInput('')
                          }}
                        >
                          {item.withdrawal_enabled
                            ? t('Freeze Withdrawal')
                            : t('Restore Withdrawal')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({ kind: 'update_rate', item })
                            setReasonInput('')
                            setRateOverrideInput(
                              item.rate_override == null
                                ? ''
                                : String(item.rate_override)
                            )
                          }}
                        >
                          {t('Set Rate')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({
                              kind: 'adjust_increase',
                              item,
                            })
                            setAdjustAmountInput('')
                            setReasonInput('')
                          }}
                        >
                          {t('Increase')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() => {
                            setAffiliateAction({
                              kind: 'adjust_decrease',
                              item,
                            })
                            setAdjustAmountInput('')
                            setReasonInput('')
                          }}
                        >
                          {t('Decrease')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            void openAffiliateDetail(item, 'bindings')
                          }
                        >
                          {t('Bindings')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            void openAffiliateDetail(item, 'commissions')
                          }
                        >
                          {t('Commissions')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            void openAffiliateDetail(item, 'withdrawals')
                          }
                        >
                          {t('Withdrawals')}
                        </Button>
                      </div>
                    ),
                  }))}
                />
              </>
            ) : activeSection === 'commissions' ? (
              <div className='space-y-4'>
                <StatusToolbar
                  status={commissionStatus}
                  onStatusChange={setCommissionStatus}
                  onSearch={() => void loadCommissions()}
                />
                <SimpleAdminTable
                  headers={[
                    t('Trade No'),
                    t('Order Type'),
                    t('Invitee'),
                    t('Paid Amount CNY'),
                    t('Settlement Base'),
                    t('Commission'),
                    t('Status'),
                    t('Created'),
                  ]}
                  rows={commissionItems.map((item) => ({
                    key: item.id,
                    cells: [
                      item.source_trade_no,
                      orderTypeLabel(item.order_type, t),
                      item.invitee_username || item.invitee_email || '-',
                      (() => {
                        const detail = paidAmountDetail(item, t)
                        return (
                          <div className='min-w-[140px]'>
                            <div className='font-medium'>
                              {formatPaidAmountCNY(item, t)}
                            </div>
                            {detail ? (
                              <div className='text-muted-foreground text-xs'>
                                {detail}
                              </div>
                            ) : null}
                          </div>
                        )
                      })(),
                      formatSettlementMoney(
                        item.settlement_base_amount || item.base_amount,
                        item.settlement_currency
                      ),
                      formatSettlementMoney(
                        item.commission_amount,
                        item.settlement_currency
                      ),
                      commissionStatusLabel(item.status, t),
                      formatTimestamp(item.created_at),
                    ],
                  }))}
                />
              </div>
            ) : activeSection === 'ledgers' ? (
              <>
                <Toolbar
                  keyword={ledgerKeyword}
                  keywordPlaceholder={t('Search ledger type or external ref')}
                  onKeywordChange={setLedgerKeyword}
                  onSearch={() => void loadLedgers()}
                />
                <SimpleAdminTable
                  headers={[
                    t('Type'),
                    t('Operator'),
                    t('Delta Available'),
                    t('Delta Frozen'),
                    t('External Ref'),
                    t('Created'),
                  ]}
                  rows={ledgerItems.map((item) => ({
                    key: item.id,
                    cells: [
                      ledgerTypeLabel(item.type, t),
                      operatorLabel(item.operator, t),
                      formatMoney(item.delta_available),
                      formatMoney(item.delta_frozen),
                      item.external_ref_id,
                      formatTimestamp(item.created_at),
                    ],
                  }))}
                />
              </>
            ) : activeSection === 'audit-logs' ? (
              <>
                <Toolbar
                  keyword={auditKeyword}
                  keywordPlaceholder={t('Search action or reason')}
                  onKeywordChange={setAuditKeyword}
                  onSearch={() => void loadAuditLogs()}
                  action={
                    <Button
                      variant='outline'
                      onClick={() => void handleBackfillRedemptionJobs()}
                    >
                      {t('Backfill Redemption Commissions')}
                    </Button>
                  }
                />
                <SimpleAdminTable
                  headers={[
                    '类型',
                    t('Action'),
                    '执行方',
                    '对象',
                    '原因/错误',
                    t('Created'),
                  ]}
                  rows={auditTimelineRows}
                />
              </>
            ) : (
              <>
                <StatusToolbar
                  status={withdrawalStatus}
                  onStatusChange={setWithdrawalStatus}
                  onSearch={() => void loadWithdrawals()}
                />
                <SimpleAdminTable
                  headers={[
                    t('Username'),
                    t('Amount'),
                    t('Net Amount'),
                    t('Withdrawal Info'),
                    t('QR Code'),
                    t('Status'),
                    t('Submitted'),
                    t('Action'),
                  ]}
                  rows={withdrawalItems.map((item) => ({
                    key: item.id,
                    cells: [
                      item.username || item.email || '-',
                      formatMoney(item.amount),
                      formatMoney(item.net_amount),
                      <WithdrawalInfo item={item} />,
                      <AssetPreview
                        url={item.qr_image_url}
                        label={t('QR Code')}
                      />,
                      withdrawalStatusLabel(item.status, t),
                      formatTimestamp(item.submitted_at),
                    ],
                    action: (
                      <div className='flex gap-2'>
                        {item.status === 'pending' && (
                          <>
                            <Button
                              size='sm'
                              onClick={() => {
                                setWithdrawalAction({ kind: 'approve', item })
                                setReasonInput('')
                                setAdminNote('')
                                setPaymentTxnNo('')
                                setPaymentProofURL('')
                                setRejectProofURL('')
                              }}
                            >
                              {t('Approve')}
                            </Button>
                            <Button
                              size='sm'
                              variant='outline'
                              onClick={() => {
                                setWithdrawalAction({ kind: 'reject', item })
                                setReasonInput('')
                                setAdminNote('')
                                setPaymentTxnNo('')
                                setPaymentProofURL('')
                                setRejectProofURL('')
                              }}
                            >
                              {t('Reject')}
                            </Button>
                          </>
                        )}
                        {item.status === 'approved' && (
                          <>
                            <Button
                              size='sm'
                              onClick={() => {
                                setWithdrawalAction({ kind: 'pay', item })
                                setReasonInput('')
                                setAdminNote(item.admin_note || '')
                                setPaymentTxnNo(item.payment_txn_no || '')
                                setPaymentProofURL(item.payment_proof_url || '')
                                setRejectProofURL('')
                              }}
                            >
                              {t('Mark Paid')}
                            </Button>
                            <Button
                              size='sm'
                              variant='outline'
                              onClick={() => {
                                setWithdrawalAction({ kind: 'reject', item })
                                setReasonInput(item.reject_reason || '')
                                setAdminNote(item.admin_note || '')
                                setPaymentTxnNo('')
                                setPaymentProofURL('')
                                setRejectProofURL(item.reject_proof_url || '')
                              }}
                            >
                              {t('Reject')}
                            </Button>
                          </>
                        )}
                      </div>
                    ),
                  }))}
                />
              </>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <ConfirmDialog
        open={!!pendingDecision || !!affiliateAction}
        onOpenChange={(open) => {
          if (!open) {
            setPendingDecision(null)
            setAffiliateAction(null)
            setReasonInput('')
            setRateOverrideInput('')
            setAdjustAmountInput('')
          }
        }}
        title={actionTitle}
        desc={
          <div className='space-y-3'>
            {(pendingDecision?.kind === 'approve' ||
              affiliateAction?.kind === 'update_rate') && (
              <LabeledInput
                label={t('Rate Override')}
                value={rateOverrideInput}
                onChange={setRateOverrideInput}
              />
            )}
            {(affiliateAction?.kind === 'adjust_increase' ||
              affiliateAction?.kind === 'adjust_decrease') && (
              <LabeledInput
                label={t('Amount')}
                value={adjustAmountInput}
                onChange={setAdjustAmountInput}
              />
            )}
            <div className='space-y-2'>
              <div className='text-sm font-medium'>{t('Reason')}</div>
              <textarea
                className='border-input min-h-[96px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                value={reasonInput}
                onChange={(e) => setReasonInput(e.target.value)}
              />
            </div>
          </div>
        }
        confirmText={t('Confirm')}
        handleConfirm={() =>
          void (pendingDecision
            ? handlePendingDecision()
            : handleAffiliateAction())
        }
      />

      <ConfirmDialog
        open={!!withdrawalAction}
        onOpenChange={(open) => {
          if (!open) {
            setWithdrawalAction(null)
            setReasonInput('')
            setAdminNote('')
            setPaymentTxnNo('')
            setPaymentProofURL('')
            setRejectProofURL('')
          }
        }}
        title={
          withdrawalAction?.kind === 'approve'
            ? t('Approve withdrawal?')
            : withdrawalAction?.kind === 'reject'
              ? t('Reject withdrawal?')
              : t('Mark withdrawal as paid?')
        }
        desc={
          <div className='space-y-3'>
            {withdrawalAction?.kind === 'reject' && (
              <>
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('Reject Reason')}
                  </div>
                  <textarea
                    className='border-input min-h-[96px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                    value={reasonInput}
                    onChange={(e) => setReasonInput(e.target.value)}
                  />
                </div>
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('Upload Reject Proof Optional')}
                  </div>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='file'
                      accept='image/*'
                      onChange={(e) => void handleUploadRejectProof(e)}
                    />
                    <span className='text-muted-foreground text-xs whitespace-nowrap'>
                      {rejectProofURL ? t('Uploaded') : t('Not uploaded')}
                    </span>
                  </div>
                  {rejectProofURL ? (
                    <AssetPreview
                      url={rejectProofURL}
                      label={t('Reject Proof')}
                    />
                  ) : null}
                </div>
              </>
            )}
            <div className='space-y-2'>
              <div className='text-sm font-medium'>{t('Admin Note')}</div>
              <textarea
                className='border-input min-h-[96px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                value={adminNote}
                onChange={(e) => setAdminNote(e.target.value)}
              />
            </div>
            {withdrawalAction?.kind === 'pay' && (
              <>
                <LabeledInput
                  label={t('Payment Transaction No')}
                  value={paymentTxnNo}
                  onChange={setPaymentTxnNo}
                />
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>
                    {t('Upload Payment Proof')}
                  </div>
                  <div className='flex items-center gap-2'>
                    <Input
                      type='file'
                      accept='image/*'
                      onChange={(e) => void handleUploadPaymentProof(e)}
                    />
                    <span className='text-muted-foreground text-xs whitespace-nowrap'>
                      {paymentProofURL ? t('Uploaded') : t('Not uploaded')}
                    </span>
                  </div>
                  {paymentProofURL ? (
                    <AssetPreview
                      url={paymentProofURL}
                      label={t('Payment Proof')}
                    />
                  ) : null}
                </div>
              </>
            )}
          </div>
        }
        confirmText={t('Confirm')}
        handleConfirm={() => void handleWithdrawalAction()}
      />

      <ConfirmDialog
        open={!!detailAffiliate && !!detailMode}
        className='sm:max-w-4xl'
        onOpenChange={(open) => {
          if (!open) {
            setDetailAffiliate(null)
            setDetailMode(null)
            setBindingItems([])
          }
        }}
        title={
          detailMode === 'bindings'
            ? t('Affiliate Bindings')
            : detailMode === 'commissions'
              ? t('Affiliate Commissions')
              : t('Affiliate Withdrawals')
        }
        desc={
          <div className='max-h-[60vh] overflow-auto'>
            {detailMode === 'bindings' ? (
              <SimpleAdminTable
                headers={[t('Invitee'), t('Email'), t('Bound At')]}
                rows={bindingItems.map((item) => ({
                  key: item.id,
                  cells: [
                    item.invitee_username || '-',
                    item.invitee_email || '-',
                    formatTimestamp(item.bound_at),
                  ],
                }))}
              />
            ) : detailMode === 'commissions' ? (
              <SimpleAdminTable
                headers={[
                  t('Trade No'),
                  t('Invitee'),
                  t('Commission'),
                  t('Status'),
                ]}
                rows={commissionItems.map((item) => ({
                  key: item.id,
                  cells: [
                    item.source_trade_no,
                    item.invitee_username || item.invitee_email || '-',
                    formatMoney(item.commission_amount),
                    item.status,
                  ],
                }))}
              />
            ) : (
              <SimpleAdminTable
                headers={[
                  t('Amount'),
                  t('Withdrawal Info'),
                  t('QR Code'),
                  t('Status'),
                  t('Submitted'),
                ]}
                rows={withdrawalItems.map((item) => ({
                  key: item.id,
                  cells: [
                    formatMoney(item.amount),
                    <WithdrawalInfo item={item} />,
                    <AssetPreview
                      url={item.qr_image_url}
                      label={t('QR Code')}
                    />,
                    withdrawalStatusLabel(item.status, t),
                    formatTimestamp(item.submitted_at),
                  ],
                }))}
              />
            )}
          </div>
        }
        confirmText={t('Close')}
        handleConfirm={() => {
          setDetailAffiliate(null)
          setDetailMode(null)
          setBindingItems([])
        }}
      />
    </>
  )
}

function MetricCard(props: { title: string; value: string }) {
  return (
    <Card>
      <CardContent className='p-5'>
        <div className='text-muted-foreground text-sm'>{props.title}</div>
        <div className='mt-2 text-2xl font-semibold'>{props.value}</div>
      </CardContent>
    </Card>
  )
}

function accountTypeLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'alipay':
      return t('Alipay')
    case 'usdt':
      return 'USDT'
    default:
      return value || '-'
  }
}

function WithdrawalInfo(props: { item: ReferralWithdrawal }) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <div className='min-w-[260px] space-y-1 text-sm'>
      <div>
        {accountTypeLabel(item.account_type, t)}
      </div>
      <div className='break-words'>
        {item.account_type === 'usdt' ? t('Blockchain') : t('Account Name')}:{' '}
        {item.account_type === 'usdt'
          ? item.account_network || '-'
          : item.account_name || '-'}
      </div>
      <div className='break-words'>
        {t('Account Number')}:{' '}
        {item.account_no || item.account_no_masked || '-'}
      </div>
      {item.applicant_note ? (
        <div className='text-muted-foreground text-xs break-words'>
          {t('Applicant Note')}: {item.applicant_note}
        </div>
      ) : null}
    </div>
  )
}

function AssetPreview(props: { url?: string; label: string }) {
  return <AssetImagePreview url={props.url} label={props.label} />
}

function SimpleAdminTable(props: {
  headers: string[]
  rows: Array<{
    key: number | string
    cells: ReactNode[]
    action?: ReactNode
  }>
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='overflow-x-auto pt-6'>
        {props.rows.length === 0 ? (
          <div className='text-muted-foreground py-10 text-sm'>
            {t('No data')}
          </div>
        ) : (
          <table className='w-full min-w-[820px] text-left text-sm'>
            <thead>
              <tr className='border-b'>
                {props.headers.map((header) => (
                  <th key={header} className='px-3 py-2 font-medium'>
                    {header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {props.rows.map((row) => (
                <tr key={row.key} className='border-b last:border-b-0'>
                  {row.cells.map((cell, index) => (
                    <td
                      key={`${row.key}-${index}`}
                      className='px-3 py-2 align-top'
                    >
                      {cell}
                    </td>
                  ))}
                  {row.action !== undefined && (
                    <td className='px-3 py-2'>{row.action}</td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardContent>
    </Card>
  )
}

function Toolbar(props: {
  keyword: string
  keywordPlaceholder: string
  onKeywordChange: (value: string) => void
  onSearch: () => void
  action?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='flex flex-col gap-3 pt-6 md:flex-row'>
        <Input
          value={props.keyword}
          onChange={(e) => props.onKeywordChange(e.target.value)}
          placeholder={props.keywordPlaceholder}
        />
        <Button onClick={props.onSearch}>{t('Search')}</Button>
        {props.action}
      </CardContent>
    </Card>
  )
}

function StatusToolbar(props: {
  status: string
  onStatusChange: (value: string) => void
  onSearch: () => void
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='flex flex-col gap-3 pt-6 md:flex-row'>
        <Input
          value={props.status}
          onChange={(e) => props.onStatusChange(e.target.value)}
          placeholder={t('pending / approved / paid / rejected')}
        />
        <Button onClick={props.onSearch}>{t('Filter')}</Button>
      </CardContent>
    </Card>
  )
}

function LabeledInput(props: {
  label: string
  description?: string
  value: string
  onChange: (value: string) => void
  readOnly?: boolean
}) {
  return (
    <div className='space-y-2'>
      <div className='text-sm font-medium'>{props.label}</div>
      {props.description && (
        <div className='text-muted-foreground text-xs'>{props.description}</div>
      )}
      <Input
        value={props.value}
        readOnly={props.readOnly}
        onChange={(e) => props.onChange(e.target.value)}
      />
    </div>
  )
}

function LabeledTextarea(props: {
  label: string
  description?: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-2 md:col-span-2'>
      <div className='text-sm font-medium'>{props.label}</div>
      {props.description && (
        <div className='text-muted-foreground text-xs'>{props.description}</div>
      )}
      <Textarea
        rows={5}
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
      />
    </div>
  )
}

function SettingSwitch(props: {
  label: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className='flex items-center justify-between rounded-md border p-3'>
      <span className='text-sm font-medium'>{props.label}</span>
      <Switch checked={props.checked} onCheckedChange={props.onCheckedChange} />
    </div>
  )
}
