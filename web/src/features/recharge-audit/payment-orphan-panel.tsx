import {
  ChevronLeft,
  ChevronRight,
  ChevronsLeft,
  ChevronsRight,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
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
  creditPaymentOrphan,
  resolvePaymentOrphan,
  type PaymentOrphanEvent,
  type PaymentOrphanStatusFilter,
} from './api'

type PendingAction = {
  event: PaymentOrphanEvent
  kind: 'credit' | 'refunded' | 'dismissed'
}

function isPaymentOrphanCreditEligible(event: PaymentOrphanEvent) {
  return Boolean(event.can_credit)
}

function paymentOrphanStatusVariant(status: string): StatusVariant {
  switch (status) {
    case 'credited':
      return 'success'
    case 'refunded':
    case 'dismissed':
      return 'neutral'
    case 'pending_review':
      return 'warning'
    default:
      return 'info'
  }
}

const STATUS_FILTERS: Array<{
  value: PaymentOrphanStatusFilter
  label: string
}> = [
  { value: 'pending_review', label: 'Pending review' },
  { value: 'credited', label: 'Credited' },
  { value: 'refunded', label: 'Refunded' },
  { value: 'dismissed', label: 'Dismissed' },
  { value: 'all', label: 'All statuses' },
]

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const PAGE_SIZE_SELECT_ITEMS = PAGE_SIZE_OPTIONS.map((pageSize) => ({
  value: String(pageSize),
  label: pageSize,
}))

export function PaymentOrphanPanel(props: {
  events: PaymentOrphanEvent[]
  loading: boolean
  status: PaymentOrphanStatusFilter
  page: number
  pageSize: number
  total: number
  onStatusChange: (status: PaymentOrphanStatusFilter) => void
  onPageChange: (page: number) => void
  onPageSizeChange: (pageSize: number) => void
  onRefresh: () => void
}) {
  const { t } = useTranslation()
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null)
  const [note, setNote] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const totalPages = Math.max(1, Math.ceil(props.total / props.pageSize))
  const rangeStart =
    props.total === 0 ? 0 : (props.page - 1) * props.pageSize + 1
  const rangeEnd = Math.min(props.total, props.page * props.pageSize)
  const canPrevious = props.page > 1 && !props.loading
  const canNext = props.page < totalPages && !props.loading

  async function handleConfirm() {
    if (!pendingAction) return
    setSubmitting(true)
    try {
      const result =
        pendingAction.kind === 'credit'
          ? await creditPaymentOrphan(pendingAction.event.id)
          : await resolvePaymentOrphan(
              pendingAction.event.id,
              pendingAction.kind,
              note.trim()
            )
      if (!result.success) {
        toast.error(result.message || t('Operation failed'))
        return
      }
      toast.success(t('Payment orphan updated'))
      setPendingAction(null)
      setNote('')
      props.onRefresh()
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Operation failed')
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <Card>
        <CardContent className='space-y-3 p-4'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div>
              <div className='font-medium'>{t('Payment Orphans')}</div>
              <div className='text-muted-foreground text-sm'>
                {t('Stripe payments that need reconciliation')}
              </div>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              <Select
                items={STATUS_FILTERS}
                value={props.status}
                onValueChange={(value) =>
                  props.onStatusChange(value as PaymentOrphanStatusFilter)
                }
                disabled={props.loading}
              >
                <SelectTrigger className='h-8 w-[150px]'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {STATUS_FILTERS.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {t(item.label)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={props.onRefresh}
                disabled={props.loading}
              >
                {t('Refresh')}
              </Button>
            </div>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead>
                <tr className='border-b text-left'>
                  <th className='p-2'>{t('Reference')}</th>
                  <th className='p-2'>{t('Event')}</th>
                  <th className='p-2'>{t('Reason')}</th>
                  <th className='p-2'>{t('Status')}</th>
                  <th className='p-2'>{t('Actions')}</th>
                </tr>
              </thead>
              <tbody>
                {props.events.length === 0 ? (
                  <tr>
                    <td
                      className='text-muted-foreground p-4 text-center'
                      colSpan={5}
                    >
                      {props.loading
                        ? t('Loading...')
                        : t('No payment orphans.')}
                    </td>
                  </tr>
                ) : (
                  props.events.map((event) => {
                    const canCredit = isPaymentOrphanCreditEligible(event)
                    return (
                      <tr key={event.id} className='border-b last:border-0'>
                        <td className='p-2 align-top font-mono text-xs'>
                          <div>{event.reference_id || '-'}</div>
                          <div className='text-muted-foreground mt-1'>
                            {event.session_id || '-'}
                          </div>
                        </td>
                        <td className='p-2 align-top'>
                          <div>{event.provider}</div>
                          <div className='text-muted-foreground mt-1 text-xs'>
                            {event.event_type}
                          </div>
                        </td>
                        <td className='max-w-72 p-2 align-top'>
                          {event.reason || '-'}
                        </td>
                        <td className='p-2 align-top'>
                          <StatusBadge
                            label={event.status}
                            variant={paymentOrphanStatusVariant(event.status)}
                            copyable={false}
                          />
                        </td>
                        <td className='p-2 align-top'>
                          {event.status === 'pending_review' ? (
                            <div className='flex flex-wrap gap-2'>
                              {canCredit ? (
                                <Button
                                  type='button'
                                  size='sm'
                                  onClick={() => {
                                    setPendingAction({ event, kind: 'credit' })
                                    setNote('')
                                  }}
                                >
                                  {t('Credit payment')}
                                </Button>
                              ) : null}
                              <Button
                                type='button'
                                size='sm'
                                variant='outline'
                                onClick={() => {
                                  setPendingAction({ event, kind: 'refunded' })
                                  setNote('')
                                }}
                              >
                                {t('Mark refunded')}
                              </Button>
                              <Button
                                type='button'
                                size='sm'
                                variant='ghost'
                                onClick={() => {
                                  setPendingAction({ event, kind: 'dismissed' })
                                  setNote('')
                                }}
                              >
                                {t('Dismiss')}
                              </Button>
                            </div>
                          ) : (
                            <span className='text-muted-foreground'>-</span>
                          )}
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
          <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
            <div className='text-muted-foreground'>
              {t('Showing {{start}}-{{end}} of {{total}}', {
                start: rangeStart,
                end: rangeEnd,
                total: props.total,
              })}
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              <Select
                items={PAGE_SIZE_SELECT_ITEMS}
                value={String(props.pageSize)}
                onValueChange={(value) => props.onPageSizeChange(Number(value))}
                disabled={props.loading}
              >
                <SelectTrigger className='h-8 w-[84px]'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent side='top'>
                  <SelectGroup>
                    {PAGE_SIZE_OPTIONS.map((pageSize) => (
                      <SelectItem key={pageSize} value={String(pageSize)}>
                        {pageSize}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <div className='text-muted-foreground min-w-16 text-center tabular-nums'>
                {props.page}/{totalPages}
              </div>
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8'
                onClick={() => props.onPageChange(1)}
                disabled={!canPrevious}
              >
                <span className='sr-only'>{t('Go to first page')}</span>
                <ChevronsLeft className='size-4' />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8'
                onClick={() => props.onPageChange(props.page - 1)}
                disabled={!canPrevious}
              >
                <span className='sr-only'>{t('Go to previous page')}</span>
                <ChevronLeft className='size-4' />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8'
                onClick={() => props.onPageChange(props.page + 1)}
                disabled={!canNext}
              >
                <span className='sr-only'>{t('Go to next page')}</span>
                <ChevronRight className='size-4' />
              </Button>
              <Button
                type='button'
                variant='outline'
                size='icon'
                className='size-8'
                onClick={() => props.onPageChange(totalPages)}
                disabled={!canNext}
              >
                <span className='sr-only'>{t('Go to last page')}</span>
                <ChevronsRight className='size-4' />
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
      <ConfirmDialog
        open={pendingAction !== null}
        onOpenChange={(open) => {
          if (!open && !submitting) {
            setPendingAction(null)
            setNote('')
          }
        }}
        title={
          pendingAction?.kind === 'credit'
            ? t('Credit this Stripe payment?')
            : t('Resolve this payment orphan?')
        }
        desc={
          pendingAction?.kind === 'credit'
            ? t(
                'This creates the missing top-up or subscription and credits the matched Stripe customer exactly once.'
              )
            : t(
                'Record the external reconciliation result. This does not change user quota.'
              )
        }
        confirmText={t('Confirm')}
        destructive={pendingAction?.kind === 'dismissed'}
        isLoading={submitting}
        handleConfirm={() => {
          void handleConfirm()
        }}
      >
        {pendingAction?.kind !== 'credit' ? (
          <Input
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder={t('Resolution note')}
          />
        ) : null}
      </ConfirmDialog>
    </>
  )
}
