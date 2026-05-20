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
import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { formatTimestamp } from '@/lib/format'
import { Button, buttonVariants } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { AssetImagePreview } from './components/asset-image-preview'
import {
  applyReferralAffiliate,
  cancelReferralWithdrawal,
  createReferralWithdrawal,
  getReferralProfile,
  getReferralSummary,
  listReferralCommissions,
  listReferralWithdrawals,
  uploadReferralAsset,
} from './api'
import {
  isReferralSectionId,
  REFERRAL_DEFAULT_SECTION,
} from './section-registry'
import type {
  ReferralCommission,
  ReferralProfile,
  ReferralSectionId,
  ReferralSummary,
  ReferralWithdrawal,
} from './types'

const route = getRouteApi('/_authenticated/referral/$section')

const SECTION_META: Record<
  ReferralSectionId,
  { title: string; description: string }
> = {
  center: {
    title: 'Referral Center',
    description: 'View your referral performance and commission balance',
  },
  commissions: {
    title: 'Commission Details',
    description: 'Review referral commission records and settlement progress',
  },
  withdraw: {
    title: 'Withdraw Application',
    description: 'Submit a referral withdrawal request',
  },
  withdrawals: {
    title: 'Withdrawal Records',
    description: 'Track referral withdrawal applications and payouts',
  },
}

const ACCOUNT_TYPE_OPTIONS = [
  { value: 'alipay', label: 'Alipay' },
  { value: 'usdt', label: 'USDT' },
] as const

const ACCOUNT_NETWORK_OPTIONS = [
  { value: 'TRC20', label: 'TRC20' },
  { value: 'BEP20', label: 'BEP20' },
  { value: 'Polygon', label: 'Polygon' },
] as const

type WithdrawalSubmission = {
  amount: number
  account_type: string
  account_name: string
  account_no: string
  account_network: string
  qr_image_url: string
  applicant_note: string
}

function formatMoney(value: number): string {
  const amount = Number.isFinite(value) ? value : 0
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) >= 1 ? 2 : 4,
  }).format(amount)
  return `\u00a5${formatted}`
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

function accountNetworkLabel(
  value: string,
  t: (key: string) => string
): string {
  switch (value) {
    case 'TRC20':
      return 'TRC20'
    case 'BEP20':
      return 'BEP20'
    case 'POLYGON':
    case 'Polygon':
      return t('Polygon')
    default:
      return value || '-'
  }
}

function accountNumberPlaceholder(
  accountType: string,
  t: (key: string) => string
): string {
  switch (accountType) {
    case 'alipay':
      return t('Alipay account')
    case 'usdt':
      return t('USDT wallet address')
    default:
      return t('Account Number')
  }
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

function statusLabel(value: string, t: (key: string) => string): string {
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
    case 'available':
      return t('Available')
    case 'frozen':
      return t('Frozen')
    case 'disabled':
      return t('Disabled')
    default:
      return value || '-'
  }
}

export function Referral() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = route.useParams()
  const activeSection: ReferralSectionId =
    params.section && isReferralSectionId(params.section)
      ? params.section
      : REFERRAL_DEFAULT_SECTION

  const [loading, setLoading] = useState(true)
  const [profile, setProfile] = useState<ReferralProfile | null>(null)
  const [summary, setSummary] = useState<ReferralSummary | null>(null)
  const [commissions, setCommissions] = useState<ReferralCommission[]>([])
  const [withdrawals, setWithdrawals] = useState<ReferralWithdrawal[]>([])
  const [commissionPage, setCommissionPage] = useState(1)
  const [commissionPageSize, setCommissionPageSize] = useState(20)
  const [commissionTotal, setCommissionTotal] = useState(0)
  const [withdrawalPage, setWithdrawalPage] = useState(1)
  const [withdrawalPageSize, setWithdrawalPageSize] = useState(20)
  const [withdrawalTotal, setWithdrawalTotal] = useState(0)
  const [applicantNote, setApplicantNote] = useState('')
  const [withdrawForm, setWithdrawForm] = useState({
    amount: '',
    account_type: 'alipay',
    account_name: '',
    account_no: '',
    account_network: 'TRC20',
    qr_image_url: '',
    applicant_note: '',
  })
  const [applying, setApplying] = useState(false)
  const [submittingWithdrawal, setSubmittingWithdrawal] = useState(false)
  const [pendingWithdrawalSubmission, setPendingWithdrawalSubmission] =
    useState<WithdrawalSubmission | null>(null)
  const [uploading, setUploading] = useState(false)
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const submittingWithdrawalRef = useRef(false)

  const canViewDashboard =
    profile?.status === 'approved' || profile?.status === 'disabled'
  const pageMeta = SECTION_META[activeSection]

  const inviteLink = useMemo(() => {
    if (
      !summary?.invite_code ||
      !summary.acquisition_enabled ||
      profile?.status !== 'approved' ||
      typeof window === 'undefined'
    )
      return ''
    return `${window.location.origin}/r/${encodeURIComponent(summary.invite_code)}`
  }, [profile?.status, summary?.acquisition_enabled, summary?.invite_code])

  const rejectedReason = profile?.risk_reason || profile?.risk_note || ''

  function updateWithdrawForm(
    key: keyof typeof withdrawForm,
    value: string
  ): void {
    setWithdrawForm((prev) => {
      if (key === 'account_type') {
        return {
          ...prev,
          account_type: value,
          account_name: '',
          account_no: '',
          account_network:
            value === 'usdt' ? prev.account_network || 'TRC20' : 'TRC20',
          qr_image_url: '',
        }
      }
      return { ...prev, [key]: value }
    })
  }

  async function loadBase() {
    setLoading(true)
    try {
      const [profileRes, summaryRes] = await Promise.all([
        getReferralProfile(),
        getReferralSummary().catch(
          () =>
            ({
              success: false,
              data: null,
            }) as { success: boolean; data: ReferralSummary | null }
        ),
      ])
      setProfile((profileRes.data as ReferralProfile | null) || null)
      setSummary((summaryRes.data as ReferralSummary | null) || null)
    } finally {
      setLoading(false)
    }
  }

  async function loadCommissions(
    page = commissionPage,
    pageSize = commissionPageSize
  ) {
    const res = await listReferralCommissions({
      p: page,
      page_size: pageSize,
    })
    setCommissions(res.data?.items || [])
    setCommissionTotal(res.data?.total || 0)
  }

  async function loadWithdrawals(
    page = withdrawalPage,
    pageSize = withdrawalPageSize
  ) {
    const res = await listReferralWithdrawals({
      p: page,
      page_size: pageSize,
    })
    setWithdrawals(res.data?.items || [])
    setWithdrawalTotal(res.data?.total || 0)
  }

  async function handleCopyInviteLink() {
    if (!inviteLink) {
      toast.error(t('No referral link is available yet'))
      return
    }
    const copied = await copyToClipboard(inviteLink)
    if (copied) {
      toast.success(t('Referral link copied'))
    } else {
      toast.error(t('Failed to copy to clipboard'))
    }
  }

  async function handleApply() {
    setApplying(true)
    try {
      const res = await applyReferralAffiliate(applicantNote.trim())
      if (res.success) {
        toast.success(t('Application submitted'))
        setApplicantNote('')
        await loadBase()
      } else {
        toast.error(res.message || t('Application failed'))
      }
    } finally {
      setApplying(false)
    }
  }

  async function handleUploadReferralAsset(
    event: React.ChangeEvent<HTMLInputElement>
  ) {
    const file = event.target.files?.[0]
    if (!file) return
    setUploading(true)
    try {
      const res = await uploadReferralAsset(file)
      if (res.success && res.data?.url) {
        updateWithdrawForm('qr_image_url', res.data.url)
        toast.success(t('QR code uploaded'))
      } else {
        toast.error(res.message || t('Upload failed'))
      }
    } finally {
      setUploading(false)
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
    }
  }

  function buildWithdrawalSubmission(
    source: typeof withdrawForm = withdrawForm
  ): WithdrawalSubmission | null {
    if (source.amount.trim() === '') {
      toast.error(t('Please enter a valid amount'))
      return null
    }
    const amount = Number(source.amount)
    if (!Number.isFinite(amount) || amount <= 0) {
      toast.error(t('Please enter a valid amount'))
      return null
    }
    if (summary?.min_withdraw_amount && amount < summary.min_withdraw_amount) {
      toast.error(
        t('Withdraw amount must be at least {{amount}}', {
          amount: formatMoney(summary.min_withdraw_amount),
        })
      )
      return null
    }
    if (
      summary?.available_amount != null &&
      amount > summary.available_amount
    ) {
      toast.error(t('Available referral balance is insufficient'))
      return null
    }
    if (source.account_type !== 'alipay' && source.account_type !== 'usdt') {
      toast.error(t('Please select a withdrawal method'))
      return null
    }
    if (source.account_type === 'alipay' && !source.account_name.trim()) {
      toast.error(t('Please enter the payee name'))
      return null
    }
    if (source.account_type === 'alipay' && !source.account_no.trim()) {
      toast.error(t('Please enter an Alipay account'))
      return null
    }
    if (source.account_type === 'usdt' && !source.account_network.trim()) {
      toast.error(t('Please select the network'))
      return null
    }
    if (source.account_type === 'usdt' && !source.account_no.trim()) {
      toast.error(t('Please enter the USDT wallet address'))
      return null
    }

    return {
      amount,
      account_type: source.account_type,
      account_name:
        source.account_type === 'usdt' ? '' : source.account_name.trim(),
      account_no: source.account_no.trim(),
      account_network:
        source.account_type === 'usdt' ? source.account_network.trim() : '',
      qr_image_url: source.qr_image_url.trim(),
      applicant_note: source.applicant_note.trim(),
    }
  }

  function handleWithdrawalSubmit() {
    if (submittingWithdrawal || submittingWithdrawalRef.current) return
    if (uploading) {
      toast.error(t('Please wait for the upload to finish'))
      return
    }
    if (!profile) {
      toast.error(t('Referral profile is still loading'))
      return
    }
    if (profile?.status !== 'approved') {
      toast.error(t('Only approved affiliates can submit withdrawals'))
      return
    }
    if (!profile.withdrawal_enabled) {
      toast.error(t('Withdrawal is disabled for this affiliate account'))
      return
    }
    const submission = buildWithdrawalSubmission(withdrawForm)
    if (!submission) return
    setPendingWithdrawalSubmission(submission)
  }

  async function submitWithdrawal(submission: WithdrawalSubmission) {
    if (submittingWithdrawalRef.current) return
    submittingWithdrawalRef.current = true
    setSubmittingWithdrawal(true)
    try {
      const idempotencyKey = buildIdempotencyKey()
      const res = await createReferralWithdrawal({
        ...submission,
        idempotency_key: idempotencyKey,
      })
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
        setPendingWithdrawalSubmission(null)
        setWithdrawForm({
          amount: '',
          account_type: 'alipay',
          account_name: '',
          account_no: '',
          account_network: 'TRC20',
          qr_image_url: '',
          applicant_note: '',
        })
        await loadBase()
        setWithdrawalPage(1)
        await loadWithdrawals(1, withdrawalPageSize)
        void navigate({
          to: '/referral/$section',
          params: { section: 'withdrawals' },
        })
      } else {
        toast.error(res.message || t('Withdrawal request failed'))
      }
    } catch (error) {
      const message =
        error instanceof Error && error.message ? error.message : ''
      toast.error(message || t('Withdrawal request failed'))
    } finally {
      submittingWithdrawalRef.current = false
      setSubmittingWithdrawal(false)
    }
  }

  async function handleCancelWithdrawal(item: ReferralWithdrawal) {
    const res = await cancelReferralWithdrawal(item.id)
    if (res.success) {
      toast.success(t('Withdrawal canceled'))
      await loadBase()
      await loadWithdrawals()
    } else {
      toast.error(res.message || t('Cancel withdrawal failed'))
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadBase()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [])

  const loadSectionData = useEffectEvent(async function loadSectionData() {
    if (activeSection === 'commissions') {
      await loadCommissions()
    }
    if (activeSection === 'withdrawals') {
      await loadWithdrawals()
    }
  })

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadSectionData()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [activeSection])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t(pageMeta.title)}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(pageMeta.description)}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <Tabs
            value={activeSection}
            onValueChange={(section) =>
              void navigate({
                to: '/referral/$section',
                params: { section: section as ReferralSectionId },
              })
            }
          >
            <TabsList className='h-auto flex-wrap justify-start'>
              {(
                [
                  ['center', t('Referral Center')],
                  ['commissions', t('Commission Details')],
                  ['withdraw', t('Withdraw Application')],
                  ['withdrawals', t('Withdrawal Records')],
                ] as const
              ).map(([value, label]) => (
                <TabsTrigger key={value} value={value}>
                  {label}
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
          ) : activeSection === 'center' ? (
            canViewDashboard && summary ? (
              <div className='space-y-4'>
                <div className='grid gap-4 md:grid-cols-2 xl:grid-cols-4'>
                  <MetricCard
                    title={t('Referral Link Clicks')}
                    value={String(summary.click_count)}
                  />
                  <MetricCard
                    title={t('Bound Users')}
                    value={String(summary.bound_user_count)}
                  />
                  <MetricCard
                    title={t('Paid Users')}
                    value={String(summary.paid_user_count)}
                  />
                  <MetricCard
                    title={t('Current Rate')}
                    value={summary.rate != null ? `${summary.rate}%` : '-'}
                  />
                </div>
                <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]'>
                  <Card>
                    <CardHeader>
                      <CardTitle>{t('Referral Link')}</CardTitle>
                    </CardHeader>
                    <CardContent className='space-y-4'>
                      <div className='space-y-2'>
                        <div className='text-muted-foreground text-sm'>
                          {t('Invite Code')}
                        </div>
                        <Input value={summary.invite_code || ''} readOnly />
                      </div>
                      <div className='space-y-2'>
                        <div className='text-muted-foreground text-sm'>
                          {t('Invite Link')}
                        </div>
                        <div className='flex gap-2'>
                          <Input value={inviteLink} readOnly />
                          <Button onClick={() => void handleCopyInviteLink()}>
                            {t('Copy')}
                          </Button>
                        </div>
                      </div>
                      {profile?.status === 'disabled' && (
                        <div className='rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700'>
                          {rejectedReason ||
                            t('Your affiliate account is disabled.')}
                        </div>
                      )}
                    </CardContent>
                  </Card>
                  <Card>
                    <CardHeader>
                      <CardTitle>{t('Commission Balance')}</CardTitle>
                    </CardHeader>
                    <CardContent className='space-y-3'>
                      <BalanceLine
                        label={t('Pending')}
                        value={formatMoney(summary.pending_amount)}
                      />
                      <BalanceLine
                        label={t('Available')}
                        value={formatMoney(summary.available_amount)}
                      />
                      <BalanceLine
                        label={t('Frozen')}
                        value={formatMoney(summary.frozen_amount)}
                      />
                      <BalanceLine
                        label={t('Withdrawn')}
                        value={formatMoney(summary.withdrawn_amount)}
                      />
                      <BalanceLine
                        label={t('Min Withdraw Amount')}
                        value={formatMoney(summary.min_withdraw_amount)}
                      />
                    </CardContent>
                  </Card>
                </div>
              </div>
            ) : (
              <Card>
                <CardHeader>
                  <CardTitle>{t('Referral Center')}</CardTitle>
                </CardHeader>
                <CardContent className='space-y-4'>
                  <p className='text-muted-foreground text-sm'>
                    {profile?.status === 'pending'
                      ? t('Your affiliate application is under review.')
                      : profile?.status === 'rejected'
                        ? rejectedReason ||
                          t('Your affiliate application was rejected.')
                        : profile?.status === 'disabled'
                          ? rejectedReason ||
                            t('Your affiliate account is disabled.')
                          : t('Affiliate access requires admin approval.')}
                  </p>
                  {(profile?.status === 'rejected' || !profile) && (
                    <div className='space-y-3'>
                      <textarea
                        className='border-input min-h-[140px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                        value={applicantNote}
                        onChange={(e) => setApplicantNote(e.target.value)}
                        placeholder={t(
                          'Describe your promotion plan or channels'
                        )}
                      />
                      <Button
                        onClick={() => void handleApply()}
                        disabled={applying}
                      >
                        {applying
                          ? t('Submitting...')
                          : t('Submit Application')}
                      </Button>
                    </div>
                  )}
                </CardContent>
              </Card>
            )
          ) : activeSection === 'commissions' ? (
            <SimpleTableCard
              title={t('Commission Details')}
              rows={commissions.map((item) => ({
                key: item.id,
                cells: [
                  orderTypeLabel(item.order_type, t),
                  item.invitee_username || item.invitee_email || '-',
                  `${item.rate}%`,
                  formatMoney(item.commission_amount),
                  statusLabel(item.status, t),
                  formatTimestamp(
                    item.available_at || item.settle_at || item.created_at
                  ),
                ],
              }))}
              headers={[
                t('Order Type'),
                t('Invitee'),
                t('Rate'),
                t('Commission Amount'),
                t('Status'),
                t('Settlement Time'),
              ]}
              emptyText={t('No commission records')}
              page={commissionPage}
              pageSize={commissionPageSize}
              total={commissionTotal}
              onPageChange={(page, pageSize) => {
                setCommissionPage(page)
                setCommissionPageSize(pageSize)
                void loadCommissions(page, pageSize)
              }}
            />
          ) : activeSection === 'withdraw' ? (
            <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]'>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Withdraw Application')}</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className='space-y-4'>
                    <input
                      type='hidden'
                      name='account_type'
                      value={withdrawForm.account_type}
                    />
                    <input
                      type='hidden'
                      name='account_network'
                      value={withdrawForm.account_network}
                    />
                    <div className='space-y-1.5'>
                      <div className='text-sm font-medium'>
                        {t('Withdraw Amount')}
                      </div>
                      <Input
                        name='amount'
                        type='number'
                        value={withdrawForm.amount}
                        onChange={(e) =>
                          updateWithdrawForm('amount', e.target.value)
                        }
                        placeholder={t('Withdraw Amount')}
                      />
                    </div>
                    <div className='grid gap-3 md:grid-cols-2'>
                      <div className='space-y-1.5'>
                        <div className='text-sm font-medium'>
                          {t('Withdrawal Method')}
                        </div>
                        <Select
                          value={withdrawForm.account_type}
                          onValueChange={(value) =>
                            updateWithdrawForm('account_type', value || '')
                          }
                        >
                          <SelectTrigger type='button' className='w-full'>
                            <SelectValue>
                              {accountTypeLabel(withdrawForm.account_type, t)}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            {ACCOUNT_TYPE_OPTIONS.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                              >
                                {accountTypeLabel(option.value, t)}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      {withdrawForm.account_type === 'usdt' ? (
                        <div className='space-y-1.5'>
                          <div className='text-sm font-medium'>
                            {t('USDT Blockchain Network')}
                          </div>
                          <Select
                            value={withdrawForm.account_network}
                            onValueChange={(value) =>
                              updateWithdrawForm('account_network', value || '')
                            }
                          >
                            <SelectTrigger type='button' className='w-full'>
                              <SelectValue>
                                {accountNetworkLabel(
                                  withdrawForm.account_network,
                                  t
                                )}
                              </SelectValue>
                            </SelectTrigger>
                            <SelectContent alignItemWithTrigger={false}>
                              {ACCOUNT_NETWORK_OPTIONS.map((network) => (
                                <SelectItem
                                  key={network.value}
                                  value={network.value}
                                >
                                  {network.label}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <div className='text-muted-foreground text-xs'>
                            {t(
                              'Please select the same blockchain network as your receiving address.'
                            )}
                          </div>
                        </div>
                      ) : (
                        <div className='space-y-1.5'>
                          <div className='text-sm font-medium'>
                            {t('Account Name')}
                          </div>
                          <Input
                            name='account_name'
                            value={withdrawForm.account_name}
                            onChange={(e) =>
                              updateWithdrawForm('account_name', e.target.value)
                            }
                            placeholder={t('Account Name')}
                          />
                        </div>
                      )}
                    </div>
                    <div className='space-y-1.5'>
                      <div className='text-sm font-medium'>
                        {accountNumberPlaceholder(withdrawForm.account_type, t)}
                      </div>
                      <Input
                        name='account_no'
                        value={withdrawForm.account_no}
                        onChange={(e) =>
                          updateWithdrawForm('account_no', e.target.value)
                        }
                        placeholder={accountNumberPlaceholder(
                          withdrawForm.account_type,
                          t
                        )}
                      />
                    </div>
                    <div className='space-y-2'>
                      <div className='text-sm font-medium'>
                        {t('QR Code Optional')}
                      </div>
                      <div className='flex gap-2'>
                        <Button
                          type='button'
                          variant='outline'
                          onClick={() => fileInputRef.current?.click()}
                          disabled={uploading}
                        >
                          {uploading ? t('Uploading...') : t('Upload Image')}
                        </Button>
                        <Input
                          name='qr_image_url'
                          type='hidden'
                          value={withdrawForm.qr_image_url}
                        />
                        <div className='border-input text-muted-foreground flex min-h-9 min-w-0 flex-1 items-center rounded-md border px-3 text-sm'>
                          {withdrawForm.qr_image_url
                            ? t('QR code uploaded')
                            : t('No QR code uploaded')}
                        </div>
                      </div>
                      {withdrawForm.qr_image_url ? (
                        <AssetImagePreview
                          url={withdrawForm.qr_image_url}
                          label={t('QR Code')}
                          thumbnailClassName='h-28 w-28'
                        />
                      ) : null}
                      <input
                        ref={fileInputRef}
                        type='file'
                        accept='image/*'
                        className='hidden'
                        onChange={(event) =>
                          void handleUploadReferralAsset(event)
                        }
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <div className='text-sm font-medium'>
                        {t('Notes Optional')}
                      </div>
                      <textarea
                        name='applicant_note'
                        className='border-input min-h-[110px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                        value={withdrawForm.applicant_note}
                        onChange={(e) =>
                          updateWithdrawForm('applicant_note', e.target.value)
                        }
                        placeholder={t('Notes')}
                      />
                    </div>
                    <button
                      type='button'
                      className={buttonVariants({ variant: 'default' })}
                      onPointerDown={(event) => {
                        event.preventDefault()
                        event.stopPropagation()
                        handleWithdrawalSubmit()
                      }}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          event.stopPropagation()
                          handleWithdrawalSubmit()
                        }
                      }}
                      disabled={submittingWithdrawal || uploading}
                    >
                      {submittingWithdrawal
                        ? t('Submitting...')
                        : t('Submit Withdrawal')}
                    </button>
                  </div>
                </CardContent>
              </Card>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Available Balance')}</CardTitle>
                </CardHeader>
                <CardContent className='space-y-3'>
                  <BalanceLine
                    label={t('Available')}
                    value={formatMoney(summary?.available_amount || 0)}
                  />
                  <BalanceLine
                    label={t('Pending')}
                    value={formatMoney(summary?.pending_amount || 0)}
                  />
                  <BalanceLine
                    label={t('Frozen')}
                    value={formatMoney(summary?.frozen_amount || 0)}
                  />
                  <BalanceLine
                    label={t('Withdrawn')}
                    value={formatMoney(summary?.withdrawn_amount || 0)}
                  />
                </CardContent>
              </Card>
            </div>
          ) : (
            <WithdrawalRecordsCard
              rows={withdrawals}
              page={withdrawalPage}
              pageSize={withdrawalPageSize}
              total={withdrawalTotal}
              onPageChange={(page, pageSize) => {
                setWithdrawalPage(page)
                setWithdrawalPageSize(pageSize)
                void loadWithdrawals(page, pageSize)
              }}
              onCancel={(item) => void handleCancelWithdrawal(item)}
            />
          )}
        </div>
      </SectionPageLayout.Content>
      <ConfirmDialog
        open={!!pendingWithdrawalSubmission}
        onOpenChange={(open) => {
          if (!open) {
            setPendingWithdrawalSubmission(null)
          }
        }}
        title={t('Confirm withdrawal information')}
        desc={
          <div className='space-y-3 text-sm'>
            <p>
              {t(
                'Please carefully verify that the receiving information you entered is correct. If losses are caused by incorrect receiving information, you shall bear them yourself and this site assumes no responsibility.'
              )}
            </p>
            {pendingWithdrawalSubmission && (
              <div className='space-y-2 rounded-md border p-3'>
                <BalanceLine
                  label={t('Withdraw Amount')}
                  value={formatMoney(pendingWithdrawalSubmission.amount)}
                />
                <BalanceLine
                  label={t('Withdrawal Method')}
                  value={`${accountTypeLabel(pendingWithdrawalSubmission.account_type, t)}${
                    pendingWithdrawalSubmission.account_network
                      ? ` / ${accountNetworkLabel(pendingWithdrawalSubmission.account_network, t)}`
                      : ''
                  }`}
                />
                <BalanceLine
                  label={
                    pendingWithdrawalSubmission.account_type === 'usdt'
                      ? t('Blockchain')
                      : t('Account Name')
                  }
                  value={
                    pendingWithdrawalSubmission.account_type === 'usdt'
                      ? pendingWithdrawalSubmission.account_network || '-'
                      : pendingWithdrawalSubmission.account_name || '-'
                  }
                />
                <BalanceLine
                  label={accountNumberPlaceholder(
                    pendingWithdrawalSubmission.account_type,
                    t
                  )}
                  value={pendingWithdrawalSubmission.account_no || '-'}
                />
                <BalanceLine
                  label={t('QR Code')}
                  value={
                    pendingWithdrawalSubmission.qr_image_url
                      ? t('Uploaded')
                      : t('Not uploaded')
                  }
                />
              </div>
            )}
          </div>
        }
        cancelBtnText={t('Back to Edit')}
        confirmText={t('Confirm and Submit')}
        handleConfirm={() => {
          if (pendingWithdrawalSubmission) {
            void submitWithdrawal(pendingWithdrawalSubmission)
          }
        }}
        disabled={submittingWithdrawal}
        isLoading={submittingWithdrawal}
      />
    </SectionPageLayout>
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

function BalanceLine(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between text-sm'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='font-medium tabular-nums'>{props.value}</span>
    </div>
  )
}

function WithdrawalInfo(props: { item: ReferralWithdrawal }) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <div className='min-w-[260px] space-y-1 text-sm'>
      <div>
        {accountTypeLabel(item.account_type, t)}
        {item.account_network
          ? ` / ${accountNetworkLabel(item.account_network, t)}`
          : ''}
      </div>
      <div className='break-words'>
        {item.account_type === 'usdt' ? t('Blockchain') : t('Account Name')}:{' '}
        {item.account_type === 'usdt'
          ? item.account_network || '-'
          : item.account_name || '-'}
      </div>
      <div className='break-words'>
        {t('Account Number')}: {item.account_no || item.account_no_masked || '-'}
      </div>
      {item.applicant_note ? (
        <div className='text-muted-foreground text-xs break-words'>
          {t('Applicant Note')}: {item.applicant_note}
        </div>
      ) : null}
    </div>
  )
}

function canCancelWithdrawal(item: ReferralWithdrawal): boolean {
  if (item.status !== 'pending' || !item.submitted_at) {
    return false
  }
  return Math.floor(Date.now() / 1000) - item.submitted_at <= 30 * 60
}

function WithdrawalRecordsCard(props: {
  rows: ReferralWithdrawal[]
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number, pageSize: number) => void
  onCancel: (item: ReferralWithdrawal) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize))

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Withdrawal Records')}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4 overflow-x-auto'>
        {props.rows.length === 0 ? (
          <div className='text-muted-foreground py-10 text-sm'>
            {t('No withdrawal records')}
          </div>
        ) : (
          <table className='w-full min-w-[980px] text-left text-sm'>
            <thead>
              <tr className='border-b'>
                {[
                  t('Amount'),
                  t('Net Amount'),
                  t('Withdrawal Info'),
                  t('QR Code'),
                  t('Status'),
                  t('Submitted At'),
                  t('Action'),
                ].map((header) => (
                  <th key={header} className='px-3 py-2 font-medium'>
                    {header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {props.rows.map((item) => (
                <tr key={item.id} className='border-b last:border-b-0'>
                  <td className='px-3 py-2'>{formatMoney(item.amount)}</td>
                  <td className='px-3 py-2'>{formatMoney(item.net_amount)}</td>
                  <td className='max-w-[360px] px-3 py-2'>
                    <WithdrawalInfo item={item} />
                  </td>
                  <td className='px-3 py-2'>
                    {item.qr_image_url ? (
                      <AssetImagePreview
                        url={item.qr_image_url}
                        label={t('QR Code')}
                      />
                    ) : (
                      '-'
                    )}
                  </td>
                  <td className='px-3 py-2'>{statusLabel(item.status, t)}</td>
                  <td className='px-3 py-2'>
                    {formatTimestamp(item.submitted_at)}
                  </td>
                  <td className='px-3 py-2'>
                    {canCancelWithdrawal(item) ? (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => props.onCancel(item)}
                      >
                        {t('Cancel Withdrawal')}
                      </Button>
                    ) : (
                      '-'
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div className='text-muted-foreground flex items-center justify-between text-sm'>
          <span>
            {props.total > 0
              ? `${(props.page - 1) * props.pageSize + 1}-${Math.min(
                  props.page * props.pageSize,
                  props.total
                )} / ${props.total}`
              : '0 / 0'}
          </span>
          <div className='flex gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() => props.onPageChange(props.page - 1, props.pageSize)}
              disabled={props.page <= 1}
            >
              {t('Previous')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              onClick={() => props.onPageChange(props.page + 1, props.pageSize)}
              disabled={props.page >= totalPages}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function SimpleTableCard(props: {
  title: string
  headers: string[]
  rows: Array<{
    key: number | string
    cells: string[]
  }>
  emptyText: string
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number, pageSize: number) => void
}) {
  const { t } = useTranslation()
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize))

  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4 overflow-x-auto'>
        {props.rows.length === 0 ? (
          <div className='text-muted-foreground py-10 text-sm'>
            {props.emptyText}
          </div>
        ) : (
          <table className='w-full min-w-[720px] text-left text-sm'>
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
                    <td key={`${row.key}-${index}`} className='px-3 py-2'>
                      {cell}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div className='text-muted-foreground flex items-center justify-between text-sm'>
          <span>
            {props.total > 0
              ? `${(props.page - 1) * props.pageSize + 1}-${Math.min(
                  props.page * props.pageSize,
                  props.total
                )} / ${props.total}`
              : '0 / 0'}
          </span>
          <div className='flex gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() => props.onPageChange(props.page - 1, props.pageSize)}
              disabled={props.page <= 1}
            >
              {t('Previous')}
            </Button>
            <Button
              size='sm'
              variant='outline'
              onClick={() => props.onPageChange(props.page + 1, props.pageSize)}
              disabled={props.page >= totalPages}
            >
              {t('Next')}
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
