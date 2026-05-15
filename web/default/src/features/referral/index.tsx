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
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatCurrencyUSD, formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
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
import type {
  ReferralCommission,
  ReferralProfile,
  ReferralSectionId,
  ReferralSummary,
  ReferralWithdrawal,
} from './types'
import {
  isReferralSectionId,
  REFERRAL_DEFAULT_SECTION,
} from './section-registry'

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
  { value: 'wechat', label: 'WeChat Pay' },
  { value: 'usdt', label: 'USDT' },
] as const

const ACCOUNT_NETWORK_OPTIONS = ['TRC20', 'BEP20', 'POLYGON'] as const

function formatMoney(value: number): string {
  return formatCurrencyUSD(value || 0)
}

function buildIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
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
  const [uploading, setUploading] = useState(false)
  const [cancelTarget, setCancelTarget] = useState<ReferralWithdrawal | null>(null)
  const fileInputRef = useRef<HTMLInputElement | null>(null)

  const canViewDashboard =
    profile?.status === 'approved' || profile?.status === 'disabled'
  const canWithdraw = profile?.status === 'approved' && profile.withdrawal_enabled
  const pageMeta = SECTION_META[activeSection]

  const inviteLink = useMemo(() => {
    if (!summary?.invite_code || typeof window === 'undefined') return ''
    return `${window.location.origin}/r/${encodeURIComponent(summary.invite_code)}`
  }, [summary?.invite_code])

  const rejectedReason = profile?.risk_reason || profile?.risk_note || ''

  function updateWithdrawForm(
    key: keyof typeof withdrawForm,
    value: string
  ): void {
    setWithdrawForm((prev) => ({ ...prev, [key]: value }))
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

  async function loadCommissions(page = commissionPage, pageSize = commissionPageSize) {
    const res = await listReferralCommissions({
      p: page,
      page_size: pageSize,
    })
    setCommissions(res.data?.items || [])
    setCommissionTotal(res.data?.total || 0)
  }

  async function loadWithdrawals(page = withdrawalPage, pageSize = withdrawalPageSize) {
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
    try {
      await navigator.clipboard.writeText(inviteLink)
      toast.success(t('Referral link copied'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Copy failed'))
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

  async function handleUploadReferralAsset(event: React.ChangeEvent<HTMLInputElement>) {
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

  async function handleWithdrawalSubmit() {
    if (withdrawForm.amount.trim() === '') {
      toast.error(t('Please enter a valid amount'))
      return
    }
    const amount = Number(withdrawForm.amount)
    if (!Number.isFinite(amount) || amount <= 0) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    if (!withdrawForm.account_no.trim()) {
      toast.error(t('Please enter an account number'))
      return
    }
    if (withdrawForm.account_type !== 'usdt' && !withdrawForm.account_name.trim()) {
      toast.error(t('Please enter the payee name'))
      return
    }
    if (withdrawForm.account_type === 'usdt' && !withdrawForm.account_network.trim()) {
      toast.error(t('Please select the network'))
      return
    }

    setSubmittingWithdrawal(true)
    try {
      const idempotencyKey = buildIdempotencyKey()
      const res = await createReferralWithdrawal({
        amount,
        account_type: withdrawForm.account_type,
        account_name:
          withdrawForm.account_type === 'usdt'
            ? ''
            : withdrawForm.account_name.trim(),
        account_no: withdrawForm.account_no.trim(),
        account_network:
          withdrawForm.account_type === 'usdt'
            ? withdrawForm.account_network.trim()
            : '',
        qr_image_url: withdrawForm.qr_image_url.trim(),
        applicant_note: withdrawForm.applicant_note.trim(),
        idempotency_key: idempotencyKey,
      })
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
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
    } finally {
      setSubmittingWithdrawal(false)
    }
  }

  async function handleConfirmCancelWithdrawal() {
    if (!cancelTarget) return
    const res = await cancelReferralWithdrawal(cancelTarget.id)
    if (res.success) {
      toast.success(t('Withdrawal canceled'))
      setCancelTarget(null)
      await loadBase()
      await loadWithdrawals(withdrawalPage, withdrawalPageSize)
    } else {
      toast.error(res.message || t('Failed to cancel withdrawal'))
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
              <CardContent className='py-10 text-sm text-muted-foreground'>
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
                        <div className='text-sm text-muted-foreground'>
                          {t('Invite Code')}
                        </div>
                        <Input value={summary.invite_code || ''} readOnly />
                      </div>
                      <div className='space-y-2'>
                        <div className='text-sm text-muted-foreground'>
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
                          {rejectedReason || t('Your affiliate account is disabled.')}
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
                  <p className='text-sm text-muted-foreground'>
                    {profile?.status === 'pending'
                      ? t('Your affiliate application is under review.')
                      : profile?.status === 'rejected'
                        ? rejectedReason || t('Your affiliate application was rejected.')
                        : profile?.status === 'disabled'
                          ? rejectedReason || t('Your affiliate account is disabled.')
                          : t('Affiliate access requires admin approval.')}
                  </p>
                  {(profile?.status === 'rejected' || !profile) && (
                    <div className='space-y-3'>
                      <textarea
                        className='border-input min-h-[140px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                        value={applicantNote}
                        onChange={(e) => setApplicantNote(e.target.value)}
                        placeholder={t('Describe your promotion plan or channels')}
                      />
                      <Button onClick={() => void handleApply()} disabled={applying}>
                        {applying ? t('Submitting...') : t('Submit Application')}
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
                  item.order_type,
                  item.invitee_username || item.invitee_email || '-',
                  `${item.rate}%`,
                  `${formatMoney(item.commission_amount)} ${item.paid_currency || ''}`.trim(),
                  statusLabel(item.status, t),
                  formatTimestamp(item.available_at || item.settle_at || item.created_at),
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
                <CardContent className='space-y-4'>
                  <Input
                    type='number'
                    value={withdrawForm.amount}
                    onChange={(e) => updateWithdrawForm('amount', e.target.value)}
                    placeholder={t('Withdraw Amount')}
                  />
                  <div className='grid gap-3 md:grid-cols-2'>
                    <Select
                      value={withdrawForm.account_type}
                      onValueChange={(value) =>
                        updateWithdrawForm('account_type', value || '')
                      }
                    >
                      <SelectTrigger className='w-full'>
                        <SelectValue placeholder={t('Account Type')} />
                      </SelectTrigger>
                      <SelectContent>
                        {ACCOUNT_TYPE_OPTIONS.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {withdrawForm.account_type === 'usdt' ? (
                      <Select
                        value={withdrawForm.account_network}
                        onValueChange={(value) =>
                          updateWithdrawForm('account_network', value || '')
                        }
                      >
                        <SelectTrigger className='w-full'>
                          <SelectValue placeholder={t('Network')} />
                        </SelectTrigger>
                        <SelectContent>
                          {ACCOUNT_NETWORK_OPTIONS.map((network) => (
                            <SelectItem key={network} value={network}>
                              {network}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input
                        value={withdrawForm.account_name}
                        onChange={(e) =>
                          updateWithdrawForm('account_name', e.target.value)
                        }
                        placeholder={t('Account Name')}
                      />
                    )}
                  </div>
                  <Input
                    value={withdrawForm.account_no}
                    onChange={(e) => updateWithdrawForm('account_no', e.target.value)}
                    placeholder={t('Account Number')}
                  />
                  <div className='space-y-2'>
                    <div className='text-sm text-muted-foreground'>
                      {t('QR Code')}
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
                        value={withdrawForm.qr_image_url}
                        onChange={(e) =>
                          updateWithdrawForm('qr_image_url', e.target.value)
                        }
                        placeholder={t('QR code URL')}
                      />
                    </div>
                    <input
                      ref={fileInputRef}
                      type='file'
                      accept='image/*'
                      className='hidden'
                      onChange={(event) => void handleUploadReferralAsset(event)}
                    />
                  </div>
                  <textarea
                    className='border-input min-h-[110px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                    value={withdrawForm.applicant_note}
                    onChange={(e) =>
                      updateWithdrawForm('applicant_note', e.target.value)
                    }
                    placeholder={t('Notes')}
                  />
                  <Button
                    onClick={() => void handleWithdrawalSubmit()}
                    disabled={!canWithdraw || submittingWithdrawal}
                  >
                    {submittingWithdrawal
                      ? t('Submitting...')
                      : t('Submit Withdrawal')}
                  </Button>
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
            <SimpleTableCard
              title={t('Withdrawal Records')}
              rows={withdrawals.map((item) => ({
                key: item.id,
                cells: [
                  formatMoney(item.amount),
                  formatMoney(item.net_amount),
                  `${item.account_type}${item.account_network ? ` / ${item.account_network}` : ''}`,
                  statusLabel(item.status, t),
                  formatTimestamp(item.submitted_at),
                ],
                action:
                  item.status === 'pending' ? (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => setCancelTarget(item)}
                    >
                      {t('Cancel')}
                    </Button>
                  ) : null,
              }))}
              headers={[
                t('Amount'),
                t('Net Amount'),
                t('Account'),
                t('Status'),
                t('Submitted At'),
              ]}
              emptyText={t('No withdrawal records')}
              page={withdrawalPage}
              pageSize={withdrawalPageSize}
              total={withdrawalTotal}
              onPageChange={(page, pageSize) => {
                setWithdrawalPage(page)
                setWithdrawalPageSize(pageSize)
                void loadWithdrawals(page, pageSize)
              }}
            />
          )}
        </div>
      </SectionPageLayout.Content>
      <ConfirmDialog
        open={!!cancelTarget}
        onOpenChange={(open) => {
          if (!open) setCancelTarget(null)
        }}
        title={t('Cancel withdrawal?')}
        desc={t('This will release the frozen amount back to your available balance.')}
        confirmText={t('Cancel Withdrawal')}
        handleConfirm={() => void handleConfirmCancelWithdrawal()}
      />
    </SectionPageLayout>
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

function BalanceLine(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between text-sm'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='font-medium tabular-nums'>{props.value}</span>
    </div>
  )
}

function SimpleTableCard(props: {
  title: string
  headers: string[]
  rows: Array<{
    key: number | string
    cells: string[]
    action?: ReactNode
  }>
  emptyText: string
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number, pageSize: number) => void
}) {
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize))

  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
      </CardHeader>
      <CardContent className='space-y-4 overflow-x-auto'>
        {props.rows.length === 0 ? (
          <div className='py-10 text-sm text-muted-foreground'>
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
                <th className='px-3 py-2 font-medium'>Action</th>
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
                  <td className='px-3 py-2'>{row.action}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        <div className='flex items-center justify-between text-sm text-muted-foreground'>
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
              Prev
            </Button>
            <Button
              size='sm'
              variant='outline'
              onClick={() => props.onPageChange(props.page + 1, props.pageSize)}
              disabled={props.page >= totalPages}
            >
              Next
            </Button>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
