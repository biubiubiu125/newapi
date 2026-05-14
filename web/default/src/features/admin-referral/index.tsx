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
import { useEffect, useState, type ReactNode } from 'react'
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
  approveReferralAffiliate,
  approveReferralWithdrawal,
  disableReferralAffiliate,
  freezeReferralSettlement,
  freezeReferralWithdrawal,
  getAdminReferralOverview,
  getAdminReferralSettings,
  listAdminPendingReferralAffiliates,
  listAdminReferralAffiliates,
  listAdminReferralCommissions,
  listAdminReferralCommissionJobs,
  listAdminReferralWithdrawals,
  markReferralWithdrawalPaid,
  rejectReferralAffiliate,
  rejectReferralWithdrawal,
  restoreReferralAffiliate,
  restoreReferralSettlement,
  restoreReferralWithdrawal,
  runReferralSettlementBatch,
  updateAdminReferralSettings,
} from '@/features/referral/api'
import type {
  ReferralAffiliate,
  ReferralCommission,
  ReferralCommissionJob,
  ReferralOverview,
  ReferralSettings,
  ReferralWithdrawal,
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
    description: 'Review commission records and background jobs',
  },
  withdrawals: {
    title: 'Referral Withdrawals',
    description: 'Review withdrawal requests and payout status',
  },
}

function formatMoney(value: number): string {
  return `¥${(value || 0).toFixed(2)}`
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
  const [commissionItems, setCommissionItems] = useState<ReferralCommission[]>(
    []
  )
  const [commissionJobs, setCommissionJobs] = useState<ReferralCommissionJob[]>(
    []
  )
  const [withdrawalItems, setWithdrawalItems] = useState<ReferralWithdrawal[]>(
    []
  )

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
    })
    setPendingItems(res.data?.items || [])
  }

  async function loadAffiliates() {
    const res = await listAdminReferralAffiliates({ p: 1, page_size: 50 })
    setAffiliateItems(res.data?.items || [])
  }

  async function loadCommissions() {
    const [commissionRes, jobsRes] = await Promise.all([
      listAdminReferralCommissions({ p: 1, page_size: 50 }),
      listAdminReferralCommissionJobs({ p: 1, page_size: 50 }),
    ])
    setCommissionItems(commissionRes.data?.items || [])
    setCommissionJobs(jobsRes.data?.items || [])
  }

  async function loadWithdrawals() {
    const res = await listAdminReferralWithdrawals({ p: 1, page_size: 50 })
    setWithdrawalItems(res.data?.items || [])
  }

  async function loadCurrentSection() {
    setLoading(true)
    try {
      if (activeSection === 'overview') await loadOverview()
      if (activeSection === 'settings') await loadSettings()
      if (activeSection === 'pending') await loadPending()
      if (activeSection === 'affiliates') await loadAffiliates()
      if (activeSection === 'commissions') await loadCommissions()
      if (activeSection === 'withdrawals') await loadWithdrawals()
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadCurrentSection()
  }, [activeSection])

  async function handleRunSettlement() {
    const res = await runReferralSettlementBatch()
    if (res.success) {
      toast.success(t('Settlement batch completed'))
      await loadOverview()
      if (activeSection === 'commissions') {
        await loadCommissions()
      }
    }
  }

  async function handleApproveAffiliate(userId: number) {
    const res = await approveReferralAffiliate(userId)
    if (res.success) {
      toast.success(t('Affiliate approved'))
      await loadPending()
      await loadAffiliates()
    }
  }

  async function handleRejectAffiliate(userId: number) {
    const res = await rejectReferralAffiliate(userId, { reason: 'Rejected' })
    if (res.success) {
      toast.success(t('Affiliate rejected'))
      await loadPending()
      await loadAffiliates()
    }
  }

  async function handleToggleAffiliate(
    item: ReferralAffiliate,
    target: 'disable' | 'restore'
  ) {
    const res =
      target === 'disable'
        ? await disableReferralAffiliate(item.user_id, { reason: 'Disabled' })
        : await restoreReferralAffiliate(item.user_id)
    if (res.success) {
      toast.success(
        target === 'disable' ? t('Affiliate disabled') : t('Affiliate restored')
      )
      await loadAffiliates()
    }
  }

  async function handleToggleSettlement(
    item: ReferralAffiliate,
    target: 'freeze' | 'restore'
  ) {
    const res =
      target === 'freeze'
        ? await freezeReferralSettlement(item.user_id, { reason: 'Frozen' })
        : await restoreReferralSettlement(item.user_id)
    if (res.success) {
      toast.success(
        target === 'freeze'
          ? t('Settlement frozen')
          : t('Settlement restored')
      )
      await loadAffiliates()
    }
  }

  async function handleToggleWithdrawal(
    item: ReferralAffiliate,
    target: 'freeze' | 'restore'
  ) {
    const res =
      target === 'freeze'
        ? await freezeReferralWithdrawal(item.user_id, { reason: 'Frozen' })
        : await restoreReferralWithdrawal(item.user_id)
    if (res.success) {
      toast.success(
        target === 'freeze'
          ? t('Withdrawal frozen')
          : t('Withdrawal restored')
      )
      await loadAffiliates()
    }
  }

  async function handleApproveWithdrawal(id: number) {
    const res = await approveReferralWithdrawal(id)
    if (res.success) {
      toast.success(t('Withdrawal approved'))
      await loadWithdrawals()
    }
  }

  async function handleRejectWithdrawal(id: number) {
    const res = await rejectReferralWithdrawal(id, { reject_reason: 'Rejected' })
    if (res.success) {
      toast.success(t('Withdrawal rejected'))
      await loadWithdrawals()
    }
  }

  async function handlePaidWithdrawal(id: number) {
    const res = await markReferralWithdrawalPaid(id)
    if (res.success) {
      toast.success(t('Withdrawal marked as paid'))
      await loadWithdrawals()
    }
  }

  async function handleSaveSettings() {
    if (!settings) return
    const res = await updateAdminReferralSettings(settings)
    if (res.success && res.data) {
      toast.success(t('Settings saved'))
      setSettings(res.data)
    }
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t(pageMeta.title)}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(pageMeta.description)}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        {activeSection === 'overview' && (
          <Button onClick={() => void handleRunSettlement()}>
            {t('Run Settlement')}
          </Button>
        )}
        {activeSection === 'settings' && (
          <Button onClick={() => void handleSaveSettings()}>
            {t('Save Settings')}
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
                  ['overview', t('Overview')],
                  ['settings', t('Settings')],
                  ['pending', t('Pending')],
                  ['affiliates', t('Affiliates')],
                  ['commissions', t('Commissions')],
                  ['withdrawals', t('Withdrawals')],
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
                title={t('Referral Clicks')}
                value={String(overview.referral_click_count)}
              />
              <MetricCard
                title={t('Failed Jobs')}
                value={String(overview.failed_commission_job_count)}
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
            </div>
          ) : activeSection === 'settings' && settings ? (
            <Card>
              <CardHeader>
                <CardTitle>{t('Referral Settings')}</CardTitle>
              </CardHeader>
              <CardContent className='grid gap-4 md:grid-cols-2'>
                <Input
                  value={String(settings.cookie_ttl_days)}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? { ...prev, cookie_ttl_days: Number(e.target.value) }
                        : prev
                    )
                  }
                  placeholder={t('Cookie TTL Days')}
                />
                <Input
                  value={String(settings.default_rate)}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? { ...prev, default_rate: Number(e.target.value) }
                        : prev
                    )
                  }
                  placeholder={t('Default Rate')}
                />
                <Input
                  value={String(settings.settle_freeze_days)}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? {
                            ...prev,
                            settle_freeze_days: Number(e.target.value),
                          }
                        : prev
                    )
                  }
                  placeholder={t('Settlement Freeze Days')}
                />
                <Input
                  value={String(settings.min_withdraw_amount)}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? {
                            ...prev,
                            min_withdraw_amount: Number(e.target.value),
                          }
                        : prev
                    )
                  }
                  placeholder={t('Min Withdraw Amount')}
                />
                <Input
                  value={String(settings.withdraw_fee)}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? { ...prev, withdraw_fee: Number(e.target.value) }
                        : prev
                    )
                  }
                  placeholder={t('Withdraw Fee')}
                />
                <Input
                  value={settings.redirect_path}
                  onChange={(e) =>
                    setSettings((prev) =>
                      prev
                        ? { ...prev, redirect_path: e.target.value }
                        : prev
                    )
                  }
                  placeholder={t('Redirect Path')}
                />
              </CardContent>
            </Card>
          ) : activeSection === 'pending' ? (
            <SimpleAdminTable
              headers={[
                t('Username'),
                t('Invite Code'),
                t('Status'),
                t('Submitted'),
                t('Action'),
              ]}
              rows={pendingItems.map((item) => ({
                key: item.id,
                cells: [
                  item.username,
                  item.invite_code,
                  item.status,
                  formatTimestamp(item.created_at),
                ],
                action: (
                  <div className='flex gap-2'>
                    <Button
                      size='sm'
                      onClick={() => void handleApproveAffiliate(item.user_id)}
                    >
                      {t('Approve')}
                    </Button>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => void handleRejectAffiliate(item.user_id)}
                    >
                      {t('Reject')}
                    </Button>
                  </div>
                ),
              }))}
            />
          ) : activeSection === 'affiliates' ? (
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
                  item.rate ? `${item.rate}%` : '-',
                  formatMoney(item.available_amount),
                  item.status,
                ],
                action: (
                  <div className='flex flex-wrap gap-2'>
                    {item.status === 'approved' ? (
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() =>
                          void handleToggleAffiliate(item, 'disable')
                        }
                      >
                        {t('Disable')}
                      </Button>
                    ) : (
                      <Button
                        size='sm'
                        onClick={() => void handleToggleAffiliate(item, 'restore')}
                      >
                        {t('Restore')}
                      </Button>
                    )}
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() =>
                        void handleToggleSettlement(
                          item,
                          item.settlement_enabled ? 'freeze' : 'restore'
                        )
                      }
                    >
                      {item.settlement_enabled
                        ? t('Freeze Settlement')
                        : t('Restore Settlement')}
                    </Button>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() =>
                        void handleToggleWithdrawal(
                          item,
                          item.withdrawal_enabled ? 'freeze' : 'restore'
                        )
                      }
                    >
                      {item.withdrawal_enabled
                        ? t('Freeze Withdrawal')
                        : t('Restore Withdrawal')}
                    </Button>
                  </div>
                ),
              }))}
            />
          ) : activeSection === 'commissions' ? (
            <div className='space-y-4'>
              <SimpleAdminTable
                headers={[
                  t('Trade No'),
                  t('Order Type'),
                  t('Commission'),
                  t('Status'),
                  t('Created'),
                ]}
                rows={commissionItems.map((item) => ({
                  key: item.id,
                  cells: [
                    item.source_trade_no,
                    item.order_type,
                    formatMoney(item.commission_amount),
                    item.status,
                    formatTimestamp(item.created_at),
                  ],
                }))}
              />
              <SimpleAdminTable
                headers={[
                  t('Job Source'),
                  t('Affiliate ID'),
                  t('Status'),
                  t('Attempts'),
                  t('Updated'),
                ]}
                rows={commissionJobs.map((item) => ({
                  key: item.id,
                  cells: [
                    item.source_trade_no,
                    String(item.affiliate_id),
                    item.status,
                    String(item.attempt_count),
                    formatTimestamp(item.updated_at),
                  ],
                }))}
              />
            </div>
          ) : (
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
                  item.username || '-',
                  formatMoney(item.amount),
                  formatMoney(item.net_amount),
                  item.account_no_masked,
                  item.status,
                  formatTimestamp(item.submitted_at),
                ],
                action: (
                  <div className='flex gap-2'>
                    {item.status === 'pending' && (
                      <>
                        <Button
                          size='sm'
                          onClick={() =>
                            void handleApproveWithdrawal(item.id)
                          }
                        >
                          {t('Approve')}
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            void handleRejectWithdrawal(item.id)
                          }
                        >
                          {t('Reject')}
                        </Button>
                      </>
                    )}
                    {item.status === 'approved' && (
                      <Button
                        size='sm'
                        onClick={() => void handlePaidWithdrawal(item.id)}
                      >
                        {t('Mark Paid')}
                      </Button>
                    )}
                  </div>
                ),
              }))}
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

function SimpleAdminTable(props: {
  headers: string[]
  rows: Array<{
    key: number | string
    cells: string[]
    action?: ReactNode
  }>
}) {
  return (
    <Card>
      <CardContent className='overflow-x-auto pt-6'>
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
                  <td key={`${row.key}-${index}`} className='px-3 py-2'>
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
      </CardContent>
    </Card>
  )
}
