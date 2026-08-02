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
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { formatQuota } from '@/lib/format'
import { formatSiteCreditAmount } from '@/features/wallet/lib'
import {
  getRechargeAudit,
  getRechargeAuditSummary,
  type RechargeAuditOrder,
  type RechargeAuditSummary,
} from './api'

function formatMoney(value: number, currency = 'CNY') {
  const normalizedCurrency = (currency || 'CNY').toUpperCase()
  const amount = Number(value || 0)
  if (normalizedCurrency === 'CNY') {
    return `\u00a5${amount.toFixed(2)}`
  }
  if (normalizedCurrency === 'USD') {
    return `$${amount.toFixed(2)}`
  }
  return `${normalizedCurrency} ${amount.toFixed(2)}`
}

function formatMoneyBreakdown(
  items: Array<{ currency: string; paid_amount: number }> | undefined
) {
  if (!items || items.length === 0) return formatMoney(0)
  return items
    .map((item) => formatMoney(item.paid_amount, item.currency))
    .join(' / ')
}

function formatPaidAmount(order: RechargeAuditOrder) {
  return formatMoney(order.paid_amount || order.money, orderPaidCurrency(order))
}

function orderPaidCurrency(order: RechargeAuditOrder) {
  const provider = (order.payment_provider || '').toLowerCase()
  if (provider === 'epay' || provider === 'bepusdt') {
    return 'CNY'
  }
  return order.paid_currency || 'CNY'
}

function paymentProviderLabel(provider: string) {
  switch ((provider || '').toLowerCase()) {
    case 'bepusdt':
      return 'BEpusdt'
    default:
      return provider || '-'
  }
}

function paidAmountDetail(
  order: RechargeAuditOrder,
  t: (key: string) => string
) {
  const currency = orderPaidCurrency(order).toUpperCase()
  if (order.paid_cny_fx_missing) {
    return `折合人民币: ${t('Missing referral FX rate')}`
  }
  if (currency !== 'CNY' && (order.paid_amount_cny || 0) > 0) {
    const rateDetail =
      order.paid_cny_fx_rate > 0
        ? ` / ${t('FX Rate')}: ${order.paid_cny_fx_rate}`
        : ''
    return `折合人民币: ${formatMoney(order.paid_amount_cny || 0, 'CNY')}${rateDetail}`
  }
  return ''
}

function formatTime(timestamp: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

function orderTypeLabel(orderType: string, t: (key: string) => string) {
  switch (orderType) {
    case 'topup':
      return t('Top-up')
    case 'subscription':
      return t('Subscription Purchase')
    default:
      return orderType || '-'
  }
}

function formatOrderDelivery(
  order: RechargeAuditOrder,
  t: (key: string) => string
) {
  if (order.order_type === 'subscription') {
    const parts: string[] = []
    if (order.product_name) {
      parts.push(`订阅套餐: ${order.product_name}`)
    }
    if (order.credit_quota > 0) {
      parts.push(
        `${t('Subscription Quota')}: ${formatQuota(order.credit_quota)}`
      )
    }
    return parts.length > 0 ? parts.join(' / ') : '订阅服务: 已开通'
  }
  return `余额充值: ${formatSiteCreditAmount(order.credit_amount ?? order.amount)}`
}

function statusVariant(status: string) {
  switch (status) {
    case 'success':
      return 'success'
    case 'pending':
      return 'warning'
    case 'failed':
    case 'expired':
      return 'danger'
    default:
      return 'neutral'
  }
}

function referralStatusVariant(status: string) {
  switch (status) {
    case 'succeeded':
    case 'skipped':
      return 'success'
    case 'pending':
    case 'processing':
      return 'warning'
    case 'failed':
      return 'danger'
    default:
      return 'neutral'
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

function referralErrorLabel(value: string, t: (key: string) => string): string {
  switch (value) {
    case 'fx_rate_missing':
      return t('Missing referral FX rate')
    default:
      return value || '-'
  }
}

function referralStatusText(
  order: RechargeAuditOrder,
  t: (key: string) => string
) {
  const status = order.referral_commission_status
  if (!status) return '-'
  const label = commissionJobStatusLabel(status, t)
  if (status === 'failed' && order.referral_commission_error) {
    return `${label}: ${referralErrorLabel(order.referral_commission_error, t)}`
  }
  return label
}

const ORDER_STATUS_OPTIONS = [
  { value: 'all', labelKey: 'All Statuses' },
  { value: 'pending', labelKey: 'pending' },
  { value: 'success', labelKey: 'success' },
  { value: 'failed', labelKey: 'failed' },
  { value: 'expired', labelKey: 'expired' },
]

const PAGE_SIZE_OPTIONS = [10, 20, 50, 100]

export function RechargeAudit() {
  const { t } = useTranslation()
  const initialKeyword =
    typeof window === 'undefined'
      ? ''
      : new URLSearchParams(window.location.search).get('keyword') || ''
  const initialUserId =
    typeof window === 'undefined'
      ? ''
      : new URLSearchParams(window.location.search).get('user_id') || ''
  const [summary, setSummary] = useState<RechargeAuditSummary | null>(null)
  const [orders, setOrders] = useState<RechargeAuditOrder[]>([])
  const [total, setTotal] = useState(0)
  const [keywordDraft, setKeywordDraft] = useState(initialKeyword)
  const [userIdDraft, setUserIdDraft] = useState(initialUserId)
  const [providerDraft, setProviderDraft] = useState('')
  const [statusDraft, setStatusDraft] = useState('all')
  const [orderTypeDraft, setOrderTypeDraft] = useState('all')
  const [keyword, setKeyword] = useState(initialKeyword)
  const [userId, setUserId] = useState(initialUserId)
  const [status, setStatus] = useState('all')
  const [provider, setProvider] = useState('')
  const [orderType, setOrderType] = useState('all')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [loading, setLoading] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)
  const requestSequence = useRef(0)

  const params = useMemo(() => {
    const p = new URLSearchParams({
      p: String(page),
      page_size: String(pageSize),
    })
    if (keyword.trim()) p.set('keyword', keyword.trim())
    if (userId.trim()) p.set('user_id', userId.trim())
    if (status !== 'all') p.set('status', status)
    if (provider.trim()) p.set('provider', provider.trim())
    if (orderType !== 'all') p.set('order_type', orderType)
    return p
  }, [keyword, userId, status, provider, orderType, page, pageSize])

  const load = useCallback(async () => {
    const requestId = requestSequence.current + 1
    requestSequence.current = requestId
    setLoading(true)
    try {
      const [summaryRes, orderRes] = await Promise.all([
        getRechargeAuditSummary(params),
        getRechargeAudit(params),
      ])
      if (requestId !== requestSequence.current) return
      if (summaryRes.success) setSummary(summaryRes.data)
      if (orderRes.success) {
        setOrders(orderRes.data?.items || [])
        setTotal(orderRes.data?.total || 0)
      }
    } finally {
      if (requestId === requestSequence.current) setLoading(false)
    }
  }, [params])

  const applyFilters = useCallback(() => {
    setKeyword(keywordDraft.trim())
    setUserId(userIdDraft.trim())
    setProvider(providerDraft.trim())
    setStatus(statusDraft || 'all')
    setOrderType(orderTypeDraft || 'all')
    setPage(1)
    setRefreshTick((value) => value + 1)
  }, [keywordDraft, orderTypeDraft, providerDraft, statusDraft, userIdDraft])

  useEffect(() => {
    void load()
  }, [load, refreshTick])

  const totals = summary?.totals
  const paidRevenueText = formatMoney(totals?.paid_amount_cny || 0, 'CNY')
  const paidRevenueDetail = formatMoneyBreakdown(summary?.by_currency)
  const siteCreditRevenueText = formatSiteCreditAmount(
    totals?.credit_amount || 0
  )
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  useEffect(() => {
    if (page > totalPages) {
      setPage(totalPages)
    }
  }, [page, totalPages])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Order Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Review recharge and subscription orders, payment channels, and financial anomalies')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-5'>
            <SummaryCard
              label={t('Actual Paid Revenue')}
              value={paidRevenueText}
              description={
                paidRevenueDetail !== paidRevenueText
                  ? `${t('Original paid')}: ${paidRevenueDetail}`
                  : undefined
              }
            />
            <SummaryCard
              label={t('Site Credit Credited')}
              value={siteCreditRevenueText}
            />
            <SummaryCard
              label={t('Successful Orders')}
              value={String(totals?.success_count || 0)}
            />
            <SummaryCard
              label={t('Pending Orders')}
              value={String(totals?.pending_count || 0)}
            />
            <SummaryCard
              label={t('Failed Orders')}
              value={String(totals?.failed_count || 0)}
            />
          </div>

          <Card>
            <CardContent className='space-y-3 p-4'>
              <form
                className='grid gap-2 md:grid-cols-[minmax(0,1fr)_120px_150px_150px_180px_auto]'
                onSubmit={(event) => {
                  event.preventDefault()
                  applyFilters()
                }}
              >
                <Input
                  value={keywordDraft}
                  onChange={(e) => {
                    setKeywordDraft(e.target.value)
                  }}
                  placeholder={t('Search order, user, or user ID')}
                />
                <Input
                  value={userIdDraft}
                  onChange={(e) => {
                    setUserIdDraft(e.target.value)
                  }}
                  placeholder={t('User ID')}
                />
                <Select
                  value={statusDraft}
                  onValueChange={(value) => {
                    setStatusDraft(value || 'all')
                  }}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue placeholder={t('Status')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {ORDER_STATUS_OPTIONS.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {t(option.labelKey)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <Input
                  value={providerDraft}
                  onChange={(e) => {
                    setProviderDraft(e.target.value)
                  }}
                  placeholder={t('Payment Gateway')}
                />
                <Select
                  value={orderTypeDraft}
                  onValueChange={(value) => {
                    setOrderTypeDraft(value || 'all')
                  }}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue placeholder={t('Order Type')} />
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      <SelectItem value='all'>{t('All Orders')}</SelectItem>
                      <SelectItem value='topup'>{t('Top-up')}</SelectItem>
                      <SelectItem value='subscription'>
                        {t('Subscription Purchase')}
                      </SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <Button type='submit' disabled={loading}>
                  {loading ? t('Loading...') : t('Search')}
                </Button>
              </form>

              {summary?.anomalies?.length ? (
                <div className='rounded-md border p-3'>
                  <div className='mb-2 text-sm font-medium'>
                    {t('Anomalies')}
                  </div>
                  <div className='space-y-1 text-sm'>
                    {summary.anomalies.slice(0, 6).map((item) => (
                      <div
                        key={`${item.type}-${item.trade_no || item.user_id}`}
                        className='text-muted-foreground'
                      >
                        <span className='text-foreground font-medium'>
                          {item.message}
                        </span>
                        {item.trade_no ? ` · ${item.trade_no}` : ''}
                        {item.username ? ` · ${item.username}` : ''}
                      </div>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Order')}</TableHead>
                      <TableHead>{t('Order Type')}</TableHead>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Payment Gateway')}</TableHead>
                      <TableHead>金额</TableHead>
                      <TableHead>交付内容</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Referral Status')}</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {orders.length > 0 ? (
                      orders.map((order) => (
                        <TableRow key={`${order.order_type}-${order.id}`}>
                          <TableCell className='font-mono text-xs'>
                            <div>{order.trade_no}</div>
                            {order.product_name ? (
                              <div className='text-muted-foreground mt-1 max-w-48 truncate font-sans text-xs'>
                                {order.product_name}
                              </div>
                            ) : null}
                          </TableCell>
                          <TableCell>
                            {orderTypeLabel(order.order_type, t)}
                          </TableCell>
                          <TableCell>
                            {order.username || order.user_id}
                          </TableCell>
                          <TableCell>
                            {paymentProviderLabel(order.payment_provider)}
                          </TableCell>
                          <TableCell>
                            <div className='font-medium'>
                              {formatPaidAmount(order)}
                            </div>
                            {paidAmountDetail(order, t) ? (
                              <div className='text-muted-foreground text-xs'>
                                {paidAmountDetail(order, t)}
                              </div>
                            ) : null}
                          </TableCell>
                          <TableCell>{formatOrderDelivery(order, t)}</TableCell>
                          <TableCell>
                            <StatusBadge
                              label={t(order.status)}
                              variant={statusVariant(order.status)}
                              copyable={false}
                            />
                          </TableCell>
                          <TableCell>
                            <StatusBadge
                              label={referralStatusText(order, t)}
                              variant={referralStatusVariant(
                                order.referral_commission_status
                              )}
                              copyable={false}
                            />
                          </TableCell>
                          <TableCell>{formatTime(order.create_time)}</TableCell>
                        </TableRow>
                      ))
                    ) : (
                      <TableRow>
                        <TableCell
                          colSpan={9}
                          className='text-muted-foreground h-24 text-center'
                        >
                          {loading ? t('Loading...') : t('No results found.')}
                        </TableCell>
                      </TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
              <div className='flex flex-col gap-2 text-sm sm:flex-row sm:items-center sm:justify-between'>
                <div className='text-muted-foreground'>
                  {t('Page {{page}} of {{totalPages}}, {{total}} items', {
                    page,
                    totalPages,
                    total,
                  })}
                </div>
                <div className='flex flex-wrap items-center gap-2'>
                  <Select
                    value={String(pageSize)}
                    onValueChange={(value) => {
                      setPageSize(Number(value) || 20)
                      setPage(1)
                    }}
                  >
                    <SelectTrigger className='h-8 w-[112px]'>
                      <SelectValue placeholder={t('Page Size')} />
                    </SelectTrigger>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {PAGE_SIZE_OPTIONS.map((size) => (
                          <SelectItem key={size} value={String(size)}>
                            {size} / {t('Page')}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setPage(1)}
                    disabled={loading || page <= 1}
                  >
                    {t('First page')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setPage((value) => Math.max(1, value - 1))}
                    disabled={loading || page <= 1}
                  >
                    {t('Previous page')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setPage((value) => Math.min(totalPages, value + 1))
                    }
                    disabled={loading || page >= totalPages}
                  >
                    {t('Next page')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => setPage(totalPages)}
                    disabled={loading || page >= totalPages}
                  >
                    {t('Last page')}
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function SummaryCard(props: {
  label: string
  value: string
  description?: string
}) {
  return (
    <Card>
      <CardContent className='p-4'>
        <div className='text-muted-foreground text-sm'>{props.label}</div>
        <div className='mt-2 break-words text-2xl font-semibold'>
          {props.value}
        </div>
        {props.description ? (
          <div className='text-muted-foreground mt-1 break-words text-xs'>
            {props.description}
          </div>
        ) : null}
      </CardContent>
    </Card>
  )
}
