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
  useEffect,
  useEffectEvent,
  useState,
  type ReactNode,
} from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import {
  adjustReferralAffiliate,
  approveReferralAffiliate,
  approveReferralWithdrawal,
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
  ADMIN_REFERRAL_DEFAULT_SECTION,
  type AdminReferralSectionId,
  isAdminReferralSectionId,
} from './section-registry'

const route = getRouteApi('/_authenticated/admin-referral/$section')

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

function formatMoney(value: number): string {
  const amount = Number.isFinite(value) ? value : 0
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) >= 1 ? 2 : 4,
  }).format(amount)
  return `\u00a5${formatted}`
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
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
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
      return t('Pending')
    case 'processing':
      return t('Processing')
    case 'skipped':
      return t('Skipped')
    case 'succeeded':
      return t('Succeeded')
    case 'failed':
      return t('Failed')
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

function ledgerTypeLabel(
  value: string,
  t: (key: string) => string
): string {
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

function operatorLabel(
  value: string,
  t: (key: string) => string
): string {
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
    default:
      return value || '-'
  }
}

function auditReasonLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'settings updated':
      return t('Settings updated')
    case 'batch approve':
      return t('Batch approved')
    case 'batch paid':
      return t('Batch paid')
    case 'ui button smoke test':
      return t('UI button smoke test')
    case 'approve e2e':
      return t('Approve E2E')
    case 'paid e2e':
      return t('Paid E2E')
    case 'approve flow':
      return t('Approve flow')
    case 'paid flow':
      return t('Paid flow')
    case 'withdrawal flow':
      return t('Withdrawal flow')
    case 'e2e':
      return t('E2E test')
    default:
      return value || '-'
  }
}

function auditActionLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'referral_affiliate_approve':
      return t('Affiliate Approved')
    case 'referral_affiliate_reject':
      return t('Affiliate Rejected')
    case 'referral_affiliate_disable':
      return t('Affiliate Disabled')
    case 'referral_affiliate_restore':
      return t('Affiliate Restored')
    case 'referral_withdrawal_create':
      return t('Withdrawal Created')
    case 'referral_withdrawal_cancel':
      return t('Withdrawal Canceled')
    case 'referral_withdrawal_approve':
      return t('Withdrawal Approved')
    case 'referral_withdrawal_reject':
      return t('Withdrawal Rejected')
    case 'referral_withdrawal_paid':
      return t('Withdrawal Paid')
    case 'referral_settings_update':
      return t('Referral Settings Updated')
    case 'referral_withdrawal_freeze':
      return t('Withdrawal Frozen')
    case 'referral_withdrawal_restore':
      return t('Withdrawal Restored')
    case 'referral_settlement_freeze':
      return t('Settlement Frozen')
    case 'referral_settlement_restore':
      return t('Settlement Restored')
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
  const [commissionItems, setCommissionItems] = useState<ReferralCommission[]>([])
  const [commissionJobs, setCommissionJobs] = useState<ReferralCommissionJob[]>([])
  const [withdrawalItems, setWithdrawalItems] = useState<ReferralWithdrawal[]>([])
  const [ledgerItems, setLedgerItems] = useState<ReferralLedger[]>([])
  const [auditLogItems, setAuditLogItems] = useState<ReferralAdminAuditLog[]>([])
  const [affiliateKeyword, setAffiliateKeyword] = useState('')
  const [commissionStatus, setCommissionStatus] = useState('')
  const [withdrawalStatus, setWithdrawalStatus] = useState('')
  const [ledgerKeyword, setLedgerKeyword] = useState('')
  const [auditKeyword, setAuditKeyword] = useState('')
  const [pendingDecision, setPendingDecision] = useState<PendingDecision>(null)
  const [affiliateAction, setAffiliateAction] = useState<AffiliateAction>(null)
  const [withdrawalAction, setWithdrawalAction] = useState<WithdrawalAction>(null)
  const [reasonInput, setReasonInput] = useState('')
  const [rateOverrideInput, setRateOverrideInput] = useState('')
  const [paymentTxnNo, setPaymentTxnNo] = useState('')
  const [paymentProofURL, setPaymentProofURL] = useState('')
  const [adminNote, setAdminNote] = useState('')
  const [runningSettlement, setRunningSettlement] = useState(false)
  const [savingSettings, setSavingSettings] = useState(false)
  const [adjustAmountInput, setAdjustAmountInput] = useState('')
  const [detailAffiliate, setDetailAffiliate] = useState<ReferralAffiliate | null>(null)
  const [detailMode, setDetailMode] = useState<'bindings' | 'commissions' | 'withdrawals' | null>(null)

  const pageMeta = SECTION_META[activeSection]

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

  const loadCurrentSection = useEffectEvent(async function loadCurrentSection() {
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
  })

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
      const res = await updateAdminReferralSettings(settings)
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
      const rateOverride =
        parseOptionalNumber(rateOverrideInput)
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
      const latest = refreshed.data?.items?.find((candidate) => candidate.user_id === item.user_id)
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
    await loadWithdrawals()
    await loadLedgers()
    await loadAuditLogs()
    if (detailMode === 'withdrawals' && detailAffiliate) {
      const res = await listAdminReferralWithdrawals({
        p: 1,
        page_size: 100,
        affiliate_user_id: detailAffiliate.user_id,
      })
      setWithdrawalItems(res.data?.items || [])
    }
  }

  async function handleUploadPaymentProof(event: React.ChangeEvent<HTMLInputElement>) {
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

  async function handleRetryCommissionJob(item: ReferralCommissionJob) {
    const res = await retryAdminReferralCommissionJob({
      source_type: item.source_type,
      trade_no: item.source_trade_no,
    })
    if (!res.success) {
      toast.error(res.message || t('Retry failed'))
      return
    }
    toast.success(t('Commission retry submitted'))
    await loadCommissionJobs()
    await loadOverview()
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
          <Button onClick={() => void handleRunSettlement()} disabled={runningSettlement}>
            {runningSettlement ? t('Running...') : t('Run Settlement')}
          </Button>
        )}
        {activeSection === 'settings' && (
          <Button onClick={() => void handleSaveSettings()} disabled={savingSettings}>
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
                  {t(SECTION_META[section].title)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>

          {loading ? (
            <Card>
              <CardContent className='py-10 text-sm text-muted-foreground'>
                {t('Loading...')}
              </CardContent>
            </Card>
          ) : activeSection === 'overview' && overview ? (
            <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
              <MetricCard title={t('Total Affiliates')} value={String(overview.total_affiliates)} />
              <MetricCard title={t('Pending Affiliates')} value={String(overview.pending_affiliates)} />
              <MetricCard title={t('Approved Affiliates')} value={String(overview.approved_affiliates)} />
              <MetricCard title={t('Referral Clicks')} value={String(overview.referral_click_count)} />
              <MetricCard title={t('Bound Users')} value={String(overview.bound_user_count)} />
              <MetricCard title={t('Paid Users')} value={String(overview.effective_paid_user_count)} />
              <MetricCard title={t('Pending Amount')} value={formatMoney(overview.pending_amount)} />
              <MetricCard title={t('Available Amount')} value={formatMoney(overview.available_amount)} />
              <MetricCard title={t('Frozen Amount')} value={formatMoney(overview.frozen_amount)} />
              <MetricCard title={t('Withdrawn Amount')} value={formatMoney(overview.withdrawn_amount)} />
              <MetricCard title={t('Failed Jobs')} value={String(overview.failed_commission_job_count)} />
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
                    setSettings((prev) => (prev ? { ...prev, enabled: checked } : prev))
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
                  value={settings.redirect_path}
                  onChange={(value) =>
                    setSettings((prev) =>
                      prev ? { ...prev, redirect_path: value } : prev
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
                            item.rate_override == null ? '' : String(item.rate_override)
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
                            kind: item.status === 'approved' ? 'disable' : 'restore',
                            item,
                          })
                          setReasonInput(item.risk_reason || '')
                          setRateOverrideInput('')
                        }}
                      >
                        {item.status === 'approved' ? t('Disable') : t('Restore')}
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
                            item.rate_override == null ? '' : String(item.rate_override)
                          )
                        }}
                      >
                        {t('Set Rate')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => {
                          setAffiliateAction({ kind: 'adjust_increase', item })
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
                          setAffiliateAction({ kind: 'adjust_decrease', item })
                          setAdjustAmountInput('')
                          setReasonInput('')
                        }}
                      >
                        {t('Decrease')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => void openAffiliateDetail(item, 'bindings')}
                      >
                        {t('Bindings')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => void openAffiliateDetail(item, 'commissions')}
                      >
                        {t('Commissions')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => void openAffiliateDetail(item, 'withdrawals')}
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
                    formatMoney(item.commission_amount),
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
              />
              <SimpleAdminTable
                headers={[
                  t('Action'),
                  t('Admin User ID'),
                  t('Target Username'),
                  t('Reason'),
                  t('Created'),
                ]}
                rows={auditLogItems.map((item) => ({
                  key: item.id,
                  cells: [
                    auditActionLabel(item.action, t),
                    String(item.admin_user_id),
                    item.target_username ||
                      (item.target_user_id > 0 ? `#${item.target_user_id}` : '-'),
                    auditReasonLabel(item.reason, t),
                    formatTimestamp(item.created_at),
                  ],
                }))}
              />
              <SectionTitle
                title={t('Commission Jobs')}
                description={t('Background commission generation and retry status')}
              />
              <SimpleAdminTable
                headers={[
                  t('Trade No'),
                  t('Affiliate ID'),
                  t('Status'),
                  t('Attempts'),
                  t('Last Error'),
                ]}
                rows={commissionJobs.map((item) => ({
                  key: item.id,
                  cells: [
                    item.source_trade_no,
                    String(item.affiliate_id),
                    commissionJobStatusLabel(item.status, t),
                    String(item.attempt_count),
                    item.last_error || '-',
                  ],
                  action:
                    item.status === 'failed' ? (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => void handleRetryCommissionJob(item)}
                      >
                        {t('Retry')}
                      </Button>
                    ) : undefined,
                }))}
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
                  t('Account'),
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
                    item.account_no_masked || '-',
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
                            }}
                          >
                            {t('Reject')}
                          </Button>
                        </>
                      )}
                      {item.status === 'approved' && (
                        <Button
                          size='sm'
                          onClick={() => {
                            setWithdrawalAction({ kind: 'pay', item })
                            setReasonInput('')
                            setAdminNote(item.admin_note || '')
                            setPaymentTxnNo(item.payment_txn_no || '')
                            setPaymentProofURL(item.payment_proof_url || '')
                          }}
                        >
                          {t('Mark Paid')}
                        </Button>
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
            {(pendingDecision?.kind === 'approve' || affiliateAction?.kind === 'update_rate') && (
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
        handleConfirm={() => void (pendingDecision ? handlePendingDecision() : handleAffiliateAction())}
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
              <div className='space-y-2'>
                <div className='text-sm font-medium'>{t('Reject Reason')}</div>
                <textarea
                  className='border-input min-h-[96px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                  value={reasonInput}
                  onChange={(e) => setReasonInput(e.target.value)}
                />
              </div>
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
                <LabeledInput
                  label={t('Payment Proof URL')}
                  value={paymentProofURL}
                  onChange={setPaymentProofURL}
                />
                <div className='space-y-2'>
                  <div className='text-sm font-medium'>{t('Upload Payment Proof')}</div>
                  <Input type='file' accept='image/*' onChange={(e) => void handleUploadPaymentProof(e)} />
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
          <div className='max-h-[420px] overflow-auto'>
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
                headers={[t('Trade No'), t('Invitee'), t('Commission'), t('Status')]}
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
                headers={[t('Amount'), t('Account'), t('Status'), t('Submitted')]}
                rows={withdrawalItems.map((item) => ({
                  key: item.id,
                  cells: [
                    formatMoney(item.amount),
                    item.account_no_masked || '-',
                    item.status,
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
        <div className='text-sm text-muted-foreground'>{props.title}</div>
        <div className='mt-2 text-2xl font-semibold'>{props.value}</div>
      </CardContent>
    </Card>
  )
}

function SectionTitle(props: { title: string; description?: string }) {
  return (
    <div className='space-y-1'>
      <h3 className='text-sm font-semibold'>{props.title}</h3>
      {props.description && (
        <p className='text-sm text-muted-foreground'>{props.description}</p>
      )}
    </div>
  )
}

function SimpleAdminTable(props: {
  headers: string[]
  rows: Array<{
    key: number | string
    cells: string[]
    action?: ReactNode
  }>
}) {
  const { t } = useTranslation()
  return (
    <Card>
      <CardContent className='overflow-x-auto pt-6'>
        {props.rows.length === 0 ? (
          <div className='py-10 text-sm text-muted-foreground'>
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
                    <td key={`${row.key}-${index}`} className='px-3 py-2 align-top'>
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
  value: string
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-2'>
      <div className='text-sm font-medium'>{props.label}</div>
      <Input value={props.value} onChange={(e) => props.onChange(e.target.value)} />
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
