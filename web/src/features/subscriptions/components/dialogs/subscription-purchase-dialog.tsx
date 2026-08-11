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
import { CalendarClock, Crown, Package } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { GroupBadge } from '@/components/group-badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Separator } from '@/components/ui/separator'
import { getPaymentIcon } from '@/features/wallet/lib'
import { isSafeHttpCheckoutUrl } from '@/features/wallet/lib/payment-url'
import type { PaymentInitiationResult } from '@/features/wallet/types'
import { formatQuota } from '@/lib/format'

import {
  paySubscriptionCreem,
  paySubscriptionEpay,
  paySubscriptionBEpusdt,
  paySubscriptionBalance,
  paySubscriptionStripe,
  paySubscriptionWaffoPancake,
} from '../../api'
import {
  formatCnyPrice,
  formatDuration,
  formatResetPeriod,
  splitGroupList,
} from '../../lib'
import type { PlanRecord, SubscriptionPayResponse } from '../../types'

interface PaymentMethod {
  type: string
  name?: string
  icon?: string
}

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  plan: PlanRecord | null
  enableStripe?: boolean
  enableCreem?: boolean
  enableWaffoPancake?: boolean
  enableOnlineTopUp?: boolean
  epayMethods?: PaymentMethod[]
  enableBEpusdt?: boolean
  bepusdtMethods?: PaymentMethod[]
  purchaseLimit?: number
  purchaseCount?: number
  onBalancePurchaseSuccess?: () => void | Promise<void>
  onPaymentStarted?: (payment?: PaymentInitiationResult | string) => void
}

export function SubscriptionPurchaseDialog(props: Props) {
  const { t } = useTranslation()
  const [paying, setPaying] = useState(false)

  const plan = props.plan?.plan
  if (!plan) return null

  const stripePriceId = plan.stripe_price_id?.trim() || ''
  const creemProductId = plan.creem_product_id?.trim() || ''
  const waffoPancakeProductId = plan.waffo_pancake_product_id?.trim() || ''
  const hasStripe = props.enableStripe && stripePriceId !== ''
  const hasCreem = props.enableCreem && creemProductId !== ''
  const hasWaffoPancake =
    props.enableWaffoPancake && waffoPancakeProductId !== ''
  const hasEpay =
    props.enableOnlineTopUp && (props.epayMethods || []).length > 0
  const hasBEpusdt =
    props.enableBEpusdt && (props.bepusdtMethods || []).length > 0
  const allowBalancePay = plan.allow_balance_pay !== false
  const hasAnyPayment =
    allowBalancePay ||
    hasStripe ||
    hasCreem ||
    hasWaffoPancake ||
    hasEpay ||
    hasBEpusdt
  const totalAmount = Number(plan.total_amount || 0)
  const price = formatCnyPrice(plan.price_amount || 0)
  const grantGroups = splitGroupList(plan.grant_groups)
  const limitReached =
    (props.purchaseLimit || 0) > 0 &&
    (props.purchaseCount || 0) >= (props.purchaseLimit || 0)

  const getTradeNo = (res?: SubscriptionPayResponse): string | undefined =>
    res?.trade_no || res?.order_id || res?.data?.trade_no || res?.data?.order_id

  const getPaymentStartedPayload = (
    res: SubscriptionPayResponse,
    paymentMethod: string
  ): PaymentInitiationResult => ({
    ok: true,
    tradeNo: getTradeNo(res),
    amount: plan.price_amount || 0,
    paymentMethod,
    paymentKind: 'subscription',
    title: plan.title,
  })

  const handlePayBalance = async () => {
    if (!allowBalancePay) {
      toast.error(t('This plan does not allow balance redemption'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionBalance({ plan_id: plan.id })
      if (res.success || res.message === 'success') {
        toast.success(t('Subscription purchased successfully'))
        props.onOpenChange(false)
        await props.onBalancePurchaseSuccess?.()
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayStripe = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionStripe({ plan_id: plan.id })
      const checkoutUrl = res.data?.pay_link?.trim() || ''
      if (res.message === 'success' && isSafeHttpCheckoutUrl(checkoutUrl)) {
        props.onOpenChange(false)
        props.onPaymentStarted?.(getPaymentStartedPayload(res, 'stripe'))
        toast.success(t('Redirecting to payment page...'))
        window.location.assign(checkoutUrl)
      } else if (res.message === 'success') {
        toast.error(t('Invalid payment redirect URL'))
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayCreem = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionCreem({ plan_id: plan.id })
      const checkoutUrl = res.data?.checkout_url?.trim() || ''
      if (res.message === 'success' && isSafeHttpCheckoutUrl(checkoutUrl)) {
        props.onOpenChange(false)
        props.onPaymentStarted?.(getPaymentStartedPayload(res, 'creem'))
        toast.success(t('Redirecting to payment page...'))
        window.location.assign(checkoutUrl)
      } else if (res.message === 'success') {
        toast.error(t('Invalid payment redirect URL'))
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  // In-tab redirect (not window.open) — user-gesture context is lost
  // across the await, so a popup would be blocked. Same as the wallet hook.
  const handlePayWaffoPancake = async () => {
    setPaying(true)
    try {
      const res = await paySubscriptionWaffoPancake({ plan_id: plan.id })
      const checkoutUrl = res.data?.checkout_url?.trim() || ''
      if (res.message === 'success' && isSafeHttpCheckoutUrl(checkoutUrl)) {
        toast.success(t('Redirecting to payment page...'))
        props.onOpenChange(false)
        props.onPaymentStarted?.(getPaymentStartedPayload(res, 'waffo_pancake'))
        window.location.assign(checkoutUrl)
      } else if (res.message === 'success') {
        toast.error(t('Invalid payment redirect URL'))
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayEpay = async (paymentMethod: string) => {
    if (!paymentMethod) {
      toast.error(t('Please select a payment method'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionEpay({
        plan_id: plan.id,
        payment_method: paymentMethod,
      })
      const paymentUrl = res.url?.trim() || ''
      if (res.message === 'success' && isSafeHttpCheckoutUrl(paymentUrl)) {
        const form = document.createElement('form')
        form.action = paymentUrl
        form.method = 'POST'
        Object.entries(res.data || {}).forEach(([key, value]) => {
          const input = document.createElement('input')
          input.type = 'hidden'
          input.name = key
          input.value = String(value)
          form.appendChild(input)
        })
        document.body.appendChild(form)
        props.onOpenChange(false)
        props.onPaymentStarted?.(getPaymentStartedPayload(res, paymentMethod))
        toast.success(t('Payment initiated'))
        form.submit()
        document.body.removeChild(form)
      } else if (res.message === 'success') {
        toast.error(t('Invalid payment redirect URL'))
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  const handlePayBEpusdt = async (paymentMethod: string) => {
    if (!paymentMethod) {
      toast.error(t('Please select a payment method'))
      return
    }
    setPaying(true)
    try {
      const res = await paySubscriptionBEpusdt({
        plan_id: plan.id,
        payment_method: paymentMethod,
      })
      const paymentUrl = res.data?.payment_url?.trim() || ''
      if (
        (res.success || res.message === 'success') &&
        isSafeHttpCheckoutUrl(paymentUrl)
      ) {
        props.onOpenChange(false)
        props.onPaymentStarted?.(getPaymentStartedPayload(res, paymentMethod))
        toast.success(t('Redirecting to payment page...'))
        window.location.assign(paymentUrl)
      } else if (res.success || res.message === 'success') {
        toast.error(t('Invalid payment redirect URL'))
      } else {
        toast.error(
          res.message && res.message !== 'success'
            ? res.message
            : t('Payment request failed')
        )
      }
    } catch {
      toast.error(t('Payment request failed'))
    } finally {
      setPaying(false)
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <Crown className='h-5 w-5' />
            {t('Purchase Subscription')}
          </DialogTitle>
        </DialogHeader>

        <div className='space-y-3 sm:space-y-4'>
          <div className='bg-muted/50 space-y-2.5 rounded-lg border p-3 sm:space-y-3 sm:p-4'>
            <div className='flex justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Plan Name')}
              </span>
              <span className='max-w-[200px] truncate text-sm font-medium'>
                {plan.title}
              </span>
            </div>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Validity Period')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <CalendarClock className='h-3.5 w-3.5' />
                {formatDuration(plan, t)}
              </span>
            </div>
            {formatResetPeriod(plan, t) !== t('No Reset') && (
              <div className='flex justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Reset Period')}
                </span>
                <span className='text-sm'>{formatResetPeriod(plan, t)}</span>
              </div>
            )}
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Received amount')}
              </span>
              <span className='flex items-center gap-1 text-sm'>
                <Package className='h-3.5 w-3.5' />
                {totalAmount > 0 ? formatQuota(totalAmount) : t('Unlimited')}
              </span>
            </div>
            {plan.upgrade_group && (
              <div className='flex items-center justify-between'>
                <span className='text-muted-foreground text-sm'>
                  {t('Upgrade Group')}
                </span>
                <GroupBadge group={plan.upgrade_group} />
              </div>
            )}
            {grantGroups.length > 0 && (
              <div className='flex items-start justify-between gap-3'>
                <span className='text-muted-foreground text-sm'>
                  {t('Granted Groups')}
                </span>
                <div className='flex max-w-[220px] flex-wrap justify-end gap-1'>
                  {grantGroups.map((group) => (
                    <GroupBadge key={group} group={group} />
                  ))}
                </div>
              </div>
            )}
            <Separator />
            <div className='flex items-center justify-between'>
              <span className='text-sm font-medium'>{t('Amount Due')}</span>
              <span className='text-primary text-lg font-bold'>{price}</span>
            </div>
          </div>

          {limitReached && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Purchase limit reached')} ({props.purchaseCount}/
                {props.purchaseLimit})
              </AlertDescription>
            </Alert>
          )}

          {hasAnyPayment ? (
            <div className='space-y-3'>
              <p className='text-muted-foreground text-xs'>
                {t('Select payment method')}
              </p>
              {allowBalancePay && (
                <Button
                  variant='outline'
                  className='w-full'
                  onClick={handlePayBalance}
                  disabled={paying || limitReached}
                >
                  {t('Pay with Balance')}
                </Button>
              )}
              {(hasStripe || hasCreem || hasWaffoPancake) && (
                <div className='grid grid-cols-2 gap-2 sm:flex'>
                  {hasStripe && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayStripe}
                      disabled={paying || limitReached}
                    >
                      Stripe
                    </Button>
                  )}
                  {hasCreem && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayCreem}
                      disabled={paying || limitReached}
                    >
                      Creem
                    </Button>
                  )}
                  {hasWaffoPancake && (
                    <Button
                      variant='outline'
                      className='flex-1'
                      onClick={handlePayWaffoPancake}
                      disabled={paying || limitReached}
                    >
                      Waffo Pancake
                    </Button>
                  )}
                </div>
              )}
              {hasEpay && (
                <div className='flex flex-wrap gap-2'>
                  {(props.epayMethods || []).map((method) => (
                    <Button
                      key={`epay-${method.type}`}
                      variant='outline'
                      onClick={() => void handlePayEpay(method.type)}
                      disabled={paying || limitReached}
                      className='h-9 shrink-0 justify-center gap-2 rounded-lg px-3 text-center sm:min-w-36'
                    >
                      {getPaymentIcon(
                        method.type,
                        'h-4 w-4 shrink-0',
                        method.icon,
                        method.name
                      )}
                      <span className='whitespace-nowrap'>
                        {method.name || method.type}
                      </span>
                    </Button>
                  ))}
                </div>
              )}
              {hasBEpusdt && (
                <div className='flex flex-wrap gap-2'>
                  {(props.bepusdtMethods || []).map((method) => (
                    <Button
                      key={`bepusdt-${method.type}`}
                      variant='outline'
                      onClick={() => void handlePayBEpusdt(method.type)}
                      disabled={paying || limitReached}
                      className='h-9 shrink-0 justify-center gap-2 rounded-lg px-3 text-center sm:min-w-36'
                    >
                      {getPaymentIcon(
                        method.type,
                        'h-4 w-4 shrink-0',
                        method.icon,
                        method.name
                      )}
                      <span className='whitespace-nowrap'>
                        {method.name || method.type}
                      </span>
                    </Button>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <Alert>
              <AlertDescription>
                {t(
                  'Online payment is not enabled. Please contact the administrator.'
                )}
              </AlertDescription>
            </Alert>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
