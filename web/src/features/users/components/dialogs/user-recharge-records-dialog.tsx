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
import { ChevronLeft, ChevronRight, ExternalLink, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  sideDrawerContentClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import {
  getRechargeAudit,
  type RechargeAuditOrder,
} from '@/features/recharge-audit/api'
import { formatSiteCreditAmount } from '@/features/wallet/lib'
import { formatQuota, formatTimestamp } from '@/lib/format'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: { id: number; username?: string } | null
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

function paymentProviderLabel(provider: string) {
  switch ((provider || '').toLowerCase()) {
    case 'bepusdt':
      return 'BEpusdt'
    default:
      return provider || '-'
  }
}

function orderPaidCurrency(order: RechargeAuditOrder) {
  const provider = (order.payment_provider || '').toLowerCase()
  if (provider === 'epay' || provider === 'bepusdt') return 'CNY'
  return order.paid_currency || 'CNY'
}

function formatMoney(value: number, currency = 'CNY') {
  const normalizedCurrency = (currency || 'CNY').toUpperCase()
  const amount = Number(value || 0)
  if (normalizedCurrency === 'CNY') return `\u00a5${amount.toFixed(2)}`
  if (normalizedCurrency === 'USD') return `$${amount.toFixed(2)}`
  return `${normalizedCurrency} ${amount.toFixed(2)}`
}

function formatPaidAmount(order: RechargeAuditOrder) {
  return formatMoney(order.paid_amount || order.money, orderPaidCurrency(order))
}

function formatDelivery(order: RechargeAuditOrder, t: (key: string) => string) {
  if (order.order_type === 'subscription') {
    const parts: string[] = []
    if (order.product_name) parts.push(order.product_name)
    if (order.credit_quota > 0) {
      parts.push(formatQuota(order.credit_quota))
    }
    return parts.length > 0 ? parts.join(' / ') : t('Subscription Purchase')
  }
  return formatSiteCreditAmount(order.credit_amount ?? order.amount)
}

export function UserRechargeRecordsDialog(props: Props) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [recordsState, setRecordsState] = useState<{
    key: string
    records: RechargeAuditOrder[]
    total: number
  }>({ key: '', records: [], total: 0 })
  const [page, setPage] = useState(1)
  const latestRequestKeyRef = useRef('')
  const pageSize = 10

  const currentRequestKey = props.user?.id ? `${props.user.id}:${page}` : ''
  const records =
    recordsState.key === currentRequestKey ? recordsState.records : []
  const total = recordsState.key === currentRequestKey ? recordsState.total : 0
  const isLoadingCurrent =
    loading ||
    (props.open &&
      Boolean(props.user?.id) &&
      recordsState.key !== currentRequestKey)

  const totalPages = useMemo(
    () => Math.max(1, Math.ceil(total / pageSize)),
    [total]
  )

  const loadData = useCallback(async () => {
    const userId = props.user?.id
    if (!userId) return
    const requestKey = `${userId}:${page}`
    latestRequestKeyRef.current = requestKey
    setLoading(true)
    try {
      const params = new URLSearchParams({
        p: String(page),
        page_size: String(pageSize),
        user_id: String(userId),
      })
      const res = await getRechargeAudit(params)
      if (latestRequestKeyRef.current !== requestKey) return
      if (res.success) {
        setRecordsState({
          key: requestKey,
          records: res.data?.items || [],
          total: res.data?.total || 0,
        })
      } else {
        setRecordsState((state) =>
          state.key === requestKey
            ? state
            : { key: requestKey, records: [], total: 0 }
        )
        toast.error(res.message || t('Loading failed'))
      }
    } catch {
      if (latestRequestKeyRef.current === requestKey) {
        setRecordsState((state) =>
          state.key === requestKey
            ? state
            : { key: requestKey, records: [], total: 0 }
        )
        toast.error(t('Loading failed'))
      }
    } finally {
      if (latestRequestKeyRef.current === requestKey) {
        setLoading(false)
      }
    }
  }, [page, props.user?.id, t])

  useEffect(() => {
    if (props.open && props.user?.id) {
      if (page !== 1) {
        latestRequestKeyRef.current = `${props.user.id}:reset`
        setPage(1)
        return
      }
      loadData()
    }
  }, [props.open, props.user?.id, page, loadData])

  const openOrderManagement = () => {
    if (!props.user?.id) return
    window.location.href = `/recharge-audit?user_id=${props.user.id}`
  }

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-4xl')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <div className='flex items-start justify-between gap-3 pr-8'>
            <div className='min-w-0'>
              <SheetTitle>{t('充值记录')}</SheetTitle>
              <SheetDescription>
                {props.user?.username || '-'} (ID: {props.user?.id || '-'})
              </SheetDescription>
            </div>
            <div className='flex shrink-0 gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={openOrderManagement}
                disabled={!props.user?.id}
              >
                <ExternalLink className='mr-1 h-4 w-4' />
                {t('Order Management')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                onClick={loadData}
                disabled={isLoadingCurrent || !props.user?.id}
              >
                <RefreshCw className='mr-1 h-4 w-4' />
                {t('Refresh')}
              </Button>
            </div>
          </div>
        </SheetHeader>

        <div className={sideDrawerFormClassName('gap-4')}>
          <div className='rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>{t('Order')}</TableHead>
                  <TableHead>{t('Order Type')}</TableHead>
                  <TableHead>{t('Payment Gateway')}</TableHead>
                  <TableHead>{t('Paid Amount')}</TableHead>
                  <TableHead>{t('Delivery')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoadingCurrent ? (
                  <TableRow>
                    <TableCell colSpan={8} className='py-8 text-center'>
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                ) : records.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground py-8 text-center'
                    >
                      {t('No recharge records')}
                    </TableCell>
                  </TableRow>
                ) : (
                  records.map((record) => (
                    <TableRow key={`${record.order_type}-${record.id}`}>
                      <TableCell>
                        <TableId value={record.id} />
                      </TableCell>
                      <TableCell className='max-w-48'>
                        <div className='truncate font-mono text-xs'>
                          {record.trade_no}
                        </div>
                        {record.product_name ? (
                          <div className='text-muted-foreground mt-1 truncate text-xs'>
                            {record.product_name}
                          </div>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        {orderTypeLabel(record.order_type, t)}
                      </TableCell>
                      <TableCell>
                        <div>
                          {paymentProviderLabel(record.payment_provider)}
                        </div>
                        <div className='text-muted-foreground text-xs'>
                          {record.payment_method || '-'}
                        </div>
                      </TableCell>
                      <TableCell>{formatPaidAmount(record)}</TableCell>
                      <TableCell>{formatDelivery(record, t)}</TableCell>
                      <TableCell>
                        <StatusBadge
                          label={t(record.status || '-')}
                          variant={statusVariant(record.status)}
                          copyable={false}
                        />
                      </TableCell>
                      <TableCell>
                        {formatTimestamp(record.create_time)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          <div className='flex items-center justify-between gap-3'>
            <div className='text-muted-foreground text-sm'>
              {t('Total')}: {total}
            </div>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setPage((value) => Math.max(1, value - 1))}
                disabled={isLoadingCurrent || page <= 1}
              >
                <ChevronLeft className='h-4 w-4' />
              </Button>
              <span className='text-sm tabular-nums'>
                {page}/{totalPages}
              </span>
              <Button
                variant='outline'
                size='sm'
                onClick={() =>
                  setPage((value) => Math.min(totalPages, value + 1))
                }
                disabled={isLoadingCurrent || page >= totalPages}
              >
                <ChevronRight className='h-4 w-4' />
              </Button>
            </div>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
