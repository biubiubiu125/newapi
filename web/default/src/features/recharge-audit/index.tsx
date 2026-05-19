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
import { useEffect, useMemo, useState } from 'react'
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

function formatPaidAmountCNY(order: RechargeAuditOrder, t: (key: string) => string) {
  if (order.paid_cny_fx_missing) {
    return t('Missing referral FX rate')
  }
  return formatMoney(order.paid_amount_cny || 0, 'CNY')
}

function formatOriginalPaidAmount(order: RechargeAuditOrder) {
  return formatMoney(
    order.paid_amount || order.money,
    order.paid_currency || 'CNY'
  )
}

function shouldShowOriginalPaid(order: RechargeAuditOrder) {
  const currency = (order.paid_currency || 'CNY').toUpperCase()
  return currency !== 'CNY' || order.paid_cny_fx_missing
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

function formatOrderBenefit(order: RechargeAuditOrder, t: (key: string) => string) {
  if (order.order_type === 'subscription') {
    if (order.credit_quota > 0) {
      return `${t('Subscription Quota')}: ${formatQuota(order.credit_quota)}`
    }
    return t('Subscription Rights')
  }
  return formatSiteCreditAmount(order.credit_amount ?? order.amount)
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

export function RechargeAudit() {
  const { t } = useTranslation()
  const [summary, setSummary] = useState<RechargeAuditSummary | null>(null)
  const [orders, setOrders] = useState<RechargeAuditOrder[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [provider, setProvider] = useState('')
  const [orderType, setOrderType] = useState('all')
  const [loading, setLoading] = useState(false)

  const params = useMemo(() => {
    const p = new URLSearchParams({ p: '1', page_size: '20' })
    if (keyword.trim()) p.set('keyword', keyword.trim())
    if (status.trim()) p.set('status', status.trim())
    if (provider.trim()) p.set('provider', provider.trim())
    if (orderType !== 'all') p.set('order_type', orderType)
    return p
  }, [keyword, status, provider, orderType])

  const load = async () => {
    setLoading(true)
    try {
      const [summaryRes, orderRes] = await Promise.all([
        getRechargeAuditSummary(params),
        getRechargeAudit(params),
      ])
      if (summaryRes.success) setSummary(summaryRes.data)
      if (orderRes.success) setOrders(orderRes.data?.items || [])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  const totals = summary?.totals
  const paidRevenueText = formatMoney(totals?.paid_amount_cny || 0, 'CNY')
  const paidRevenueDetail = formatMoneyBreakdown(summary?.by_currency)
  const siteCreditRevenueText = formatSiteCreditAmount(
    totals?.credit_amount || 0
  )

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
              <div className='grid gap-2 md:grid-cols-[minmax(0,1fr)_160px_160px_180px_auto]'>
                <Input
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  placeholder={t('Search order, user, or user ID')}
                />
                <Input
                  value={status}
                  onChange={(e) => setStatus(e.target.value)}
                  placeholder={t('Status')}
                />
                <Input
                  value={provider}
                  onChange={(e) => setProvider(e.target.value)}
                  placeholder={t('Payment Gateway')}
                />
                <Select
                  value={orderType}
                  onValueChange={(value) => setOrderType(value || 'all')}
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
                <Button onClick={load} disabled={loading}>
                  {loading ? t('Loading...') : t('Refresh')}
                </Button>
              </div>

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
                      <TableHead>{t('Paid Amount CNY')}</TableHead>
                      <TableHead>{t('Benefit')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Referral Status')}</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {orders.map((order) => (
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
                        <TableCell>{order.username || order.user_id}</TableCell>
                        <TableCell>{order.payment_provider || '-'}</TableCell>
                        <TableCell>
                          <div className='font-medium'>
                            {formatPaidAmountCNY(order, t)}
                          </div>
                          {shouldShowOriginalPaid(order) ? (
                            <div className='text-muted-foreground text-xs'>
                              {t('Original paid')}: {formatOriginalPaidAmount(order)}
                              {!order.paid_cny_fx_missing && order.paid_cny_fx_rate > 0
                                ? ` · ${t('FX Rate')}: ${order.paid_cny_fx_rate}`
                                : ''}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell>{formatOrderBenefit(order, t)}</TableCell>
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
                    ))}
                  </TableBody>
                </Table>
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
