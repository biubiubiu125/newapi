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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge } from '@/components/status-badge'
import { getRechargeAudit, getRechargeAuditSummary } from './api'
import type { RechargeAuditOrder, RechargeAuditSummary } from './api'

function formatMoney(value: number, currency = 'CNY') {
  return `${currency === 'CNY' ? '¥' : currency + ' '}${Number(value || 0).toFixed(2)}`
}

function formatTime(timestamp: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
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

export function RechargeAudit() {
  const { t } = useTranslation()
  const [summary, setSummary] = useState<RechargeAuditSummary | null>(null)
  const [orders, setOrders] = useState<RechargeAuditOrder[]>([])
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [provider, setProvider] = useState('')
  const [loading, setLoading] = useState(false)

  const params = useMemo(() => {
    const p = new URLSearchParams({ p: '1', page_size: '20' })
    if (keyword.trim()) p.set('keyword', keyword.trim())
    if (status.trim()) p.set('status', status.trim())
    if (provider.trim()) p.set('provider', provider.trim())
    return p
  }, [keyword, status, provider])

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

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Recharge Audit')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Review recharge orders, payment channels, and financial anomalies')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid gap-3 md:grid-cols-4'>
            <SummaryCard
              label={t('Successful Revenue')}
              value={formatMoney(totals?.paid_amount || 0)}
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
              <div className='grid gap-2 md:grid-cols-[minmax(0,1fr)_160px_160px_auto]'>
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
                  placeholder={t('Provider')}
                />
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
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Provider')}</TableHead>
                      <TableHead>{t('Payment')}</TableHead>
                      <TableHead>{t('Credit')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Referral')}</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {orders.map((order) => (
                      <TableRow key={order.id}>
                        <TableCell className='font-mono text-xs'>
                          {order.trade_no}
                        </TableCell>
                        <TableCell>{order.username || order.user_id}</TableCell>
                        <TableCell>{order.payment_provider || '-'}</TableCell>
                        <TableCell>
                          {formatMoney(
                            order.paid_amount || order.money,
                            order.paid_currency || 'CNY'
                          )}
                        </TableCell>
                        <TableCell>${order.amount}</TableCell>
                        <TableCell>
                          <StatusBadge
                            label={t(order.status)}
                            variant={statusVariant(order.status)}
                            copyable={false}
                          />
                        </TableCell>
                        <TableCell>
                          {order.referral_commission_status || '-'}
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

function SummaryCard(props: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className='p-4'>
        <div className='text-muted-foreground text-sm'>{props.label}</div>
        <div className='mt-2 text-2xl font-semibold'>{props.value}</div>
      </CardContent>
    </Card>
  )
}
