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
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestamp } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
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

function formatMoney(value: number): string {
  return `¥${(value || 0).toFixed(2)}`
}

function statusLabel(value: string): string {
  switch (value) {
    case 'pending':
      return 'Pending'
    case 'approved':
      return 'Approved'
    case 'paid':
      return 'Paid'
    case 'rejected':
      return 'Rejected'
    case 'canceled':
      return 'Canceled'
    case 'available':
      return 'Available'
    case 'frozen':
      return 'Frozen'
    case 'disabled':
      return 'Disabled'
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
  const [applicantNote, setApplicantNote] = useState('')
  const [withdrawAmount, setWithdrawAmount] = useState('')
  const [accountType, setAccountType] = useState('alipay')
  const [accountName, setAccountName] = useState('')
  const [accountNo, setAccountNo] = useState('')
  const [accountNetwork, setAccountNetwork] = useState('TRC20')
  const [applying, setApplying] = useState(false)
  const [submittingWithdrawal, setSubmittingWithdrawal] = useState(false)

  const canViewDashboard =
    profile?.status === 'approved' || profile?.status === 'disabled'
  const canWithdraw = profile?.status === 'approved' && profile.withdrawal_enabled

  const pageMeta = SECTION_META[activeSection]

  const inviteLink = useMemo(() => {
    if (!summary?.invite_code || typeof window === 'undefined') return ''
    return `${window.location.origin}/r/${encodeURIComponent(summary.invite_code)}`
  }, [summary?.invite_code])

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

  async function loadCommissions() {
    const res = await listReferralCommissions({ p: 1, page_size: 20 })
    setCommissions(res.data?.items || [])
  }

  async function loadWithdrawals() {
    const res = await listReferralWithdrawals({ p: 1, page_size: 20 })
    setWithdrawals(res.data?.items || [])
  }

  async function handleApply() {
    setApplying(true)
    try {
      const res = await applyReferralAffiliate(applicantNote)
      if (res.success) {
        toast.success(t('Application submitted'))
        setApplicantNote('')
        await loadBase()
      }
    } finally {
      setApplying(false)
    }
  }

  async function handleWithdrawalSubmit() {
    const amount = Number(withdrawAmount)
    if (!amount || amount <= 0) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    setSubmittingWithdrawal(true)
    try {
      const idempotencyKey =
        globalThis.crypto?.randomUUID?.() ||
        `${Date.now()}-${Math.random().toString(36).slice(2)}`
      const res = await createReferralWithdrawal({
        amount,
        account_type: accountType,
        account_name: accountType === 'usdt' ? '' : accountName,
        account_no: accountNo,
        account_network: accountType === 'usdt' ? accountNetwork : '',
        applicant_note: applicantNote,
        idempotency_key: idempotencyKey,
      })
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
        setWithdrawAmount('')
        setAccountName('')
        setAccountNo('')
        setApplicantNote('')
        await loadBase()
        await loadWithdrawals()
        void navigate({
          to: '/referral/$section',
          params: { section: 'withdrawals' },
        })
      }
    } finally {
      setSubmittingWithdrawal(false)
    }
  }

  async function handleCancelWithdrawal(id: number) {
    const res = await cancelReferralWithdrawal(id)
    if (res.success) {
      toast.success(t('Withdrawal canceled'))
      await loadBase()
      await loadWithdrawals()
    }
  }

  useEffect(() => {
    void loadBase()
  }, [])

  useEffect(() => {
    if (activeSection === 'commissions') {
      void loadCommissions()
    }
    if (activeSection === 'withdrawals') {
      void loadWithdrawals()
    }
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
                    value={summary.rate ? `${summary.rate}%` : '-'}
                  />
                </div>
                <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
                  <Card>
                    <CardHeader>
                      <CardTitle>{t('Invite Link')}</CardTitle>
                    </CardHeader>
                    <CardContent className='space-y-4'>
                      <div className='space-y-2'>
                        <div className='text-sm text-muted-foreground'>
                          {t('Invite Code')}
                        </div>
                        <Input value={summary.invite_code} readOnly />
                      </div>
                      <div className='space-y-2'>
                        <div className='text-sm text-muted-foreground'>
                          {t('Invite Link')}
                        </div>
                        <Input value={inviteLink} readOnly />
                      </div>
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
                      : profile?.status === 'disabled'
                        ? t('Your affiliate account is disabled.')
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
                      <Button onClick={handleApply} disabled={applying}>
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
                  formatMoney(item.commission_amount),
                  statusLabel(item.status),
                  formatTimestamp(item.available_at || item.settle_at || item.created_at),
                ],
              }))}
              headers={[
                t('Order Type'),
                t('Commission Amount'),
                t('Status'),
                t('Settlement Time'),
              ]}
              emptyText={t('No commission records')}
            />
          ) : activeSection === 'withdraw' ? (
            <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
              <Card>
                <CardHeader>
                  <CardTitle>{t('Withdraw Application')}</CardTitle>
                </CardHeader>
                <CardContent className='space-y-4'>
                  <Input
                    type='number'
                    value={withdrawAmount}
                    onChange={(e) => setWithdrawAmount(e.target.value)}
                    placeholder={t('Withdraw Amount')}
                  />
                  <div className='grid gap-3 md:grid-cols-2'>
                    <Input
                      value={accountType}
                      onChange={(e) => setAccountType(e.target.value)}
                      placeholder={t('Account Type')}
                    />
                    {accountType === 'usdt' ? (
                      <Input
                        value={accountNetwork}
                        onChange={(e) => setAccountNetwork(e.target.value)}
                        placeholder={t('Network')}
                      />
                    ) : (
                      <Input
                        value={accountName}
                        onChange={(e) => setAccountName(e.target.value)}
                        placeholder={t('Account Name')}
                      />
                    )}
                  </div>
                  <Input
                    value={accountNo}
                    onChange={(e) => setAccountNo(e.target.value)}
                    placeholder={t('Account Number')}
                  />
                  <textarea
                    className='border-input min-h-[110px] w-full rounded-md border bg-transparent px-3 py-2 text-sm'
                    value={applicantNote}
                    onChange={(e) => setApplicantNote(e.target.value)}
                    placeholder={t('Notes')}
                  />
                  <Button
                    onClick={handleWithdrawalSubmit}
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
                  statusLabel(item.status),
                  formatTimestamp(item.submitted_at),
                ],
                action:
                  item.status === 'pending' ? (
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => void handleCancelWithdrawal(item.id)}
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
            />
          )}
        </div>
      </SectionPageLayout.Content>
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
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
      </CardHeader>
      <CardContent className='overflow-x-auto'>
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
      </CardContent>
    </Card>
  )
}
