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
import { useQueryClient } from '@tanstack/react-query'
import { CheckCircle2, Clock3, RefreshCw } from 'lucide-react'
import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import { getSelf } from '@/lib/api'
import { subscribeSettingsRefresh } from '@/lib/settings-refresh'

import { getUserBillingHistory, isApiSuccess } from './api'
import { BillingHistoryDialog } from './components/dialogs/billing-history-dialog'
import { CreemConfirmDialog } from './components/dialogs/creem-confirm-dialog'
import { PaymentConfirmDialog } from './components/dialogs/payment-confirm-dialog'
import { RechargeFormCard } from './components/recharge-form-card'
import { SubscriptionPlansCard } from './components/subscription-plans-card'
import { WalletNoticeAlert } from './components/wallet-notice-alert'
import { WalletStatsCard } from './components/wallet-stats-card'
import { DEFAULT_DISCOUNT_RATE } from './constants'
import {
  useTopupInfo,
  usePayment,
  useRedemption,
  useCreemPayment,
  useWaffoPayment,
  useWaffoPancakePayment,
} from './hooks'
import {
  getDefaultPaymentType,
  getMinTopupAmount,
  isWaffoPancakePayment,
  formatPaymentCnyAmount,
  formatSiteCreditAmount,
  getPaymentMethodName,
} from './lib'
import type {
  UserWalletData,
  PaymentMethod,
  PresetAmount,
  CreemProduct,
  PaymentInitiationResult,
} from './types'

interface WalletProps {
  initialShowHistory?: boolean
  paymentReturn?: {
    show_history?: boolean
    pay?: 'success' | 'pending' | 'fail'
    payment_provider?: string
    order_type?: 'topup' | 'subscription'
    trade_no?: string
  }
}

interface PendingPaymentState {
  tradeNo?: string
  amount?: number
  payAmount?: number
  paymentMethod?: string
  paymentKind?: PaymentInitiationResult['paymentKind']
  title?: string
  status: 'waiting' | 'completed'
  dialogOpen: boolean
}

const PAYMENT_POLL_INTERVAL_MS = 3000

type PaymentSuccessMessageSource = {
  paymentKind?: PaymentInitiationResult['paymentKind']
  amount?: number
  title?: string
}

export function Wallet(props: WalletProps) {
  const { t } = useTranslation()
  const [user, setUser] = useState<UserWalletData | null>(null)
  const [userLoading, setUserLoading] = useState(true)
  const [topupAmount, setTopupAmount] = useState(0)
  const [selectedPreset, setSelectedPreset] = useState<number | null>(null)
  const [selectedPaymentMethod, setSelectedPaymentMethod] =
    useState<PaymentMethod>()
  const [paymentLoading, setPaymentLoading] = useState<string | null>(null)
  const [confirmDialogOpen, setConfirmDialogOpen] = useState(false)
  const [billingDialogOpen, setBillingDialogOpen] = useState(false)
  const [redemptionCode, setRedemptionCode] = useState('')
  const [creemDialogOpen, setCreemDialogOpen] = useState(false)
  const [selectedCreemProduct, setSelectedCreemProduct] =
    useState<CreemProduct | null>(null)
  const [showSubscriptionPanel, setShowSubscriptionPanel] = useState(true)
  const [paymentRefreshKey, setPaymentRefreshKey] = useState(0)
  const [pendingPayment, setPendingPayment] =
    useState<PendingPaymentState | null>(null)
  const processedPaymentReturnKeyRef = useRef('')

  const queryClient = useQueryClient()
  const { status } = useStatus()
  const { currency } = useSystemConfig()
  const {
    topupInfo,
    presetAmounts,
    loading: topupLoading,
    refetch: refetchTopupInfo,
  } = useTopupInfo()

  // Calculate effective exchange rate - when display type is USD, use rate of 1
  const effectiveUsdExchangeRate = useMemo(() => {
    return currency?.quotaDisplayType === 'USD'
      ? 1
      : currency?.usdExchangeRate || 1
  }, [currency?.quotaDisplayType, currency?.usdExchangeRate])
  const {
    amount: paymentAmount,
    calculating,
    processing,
    calculatePaymentAmount,
    processPayment,
  } = usePayment()
  const { redeeming, redeemCode } = useRedemption()
  const { processing: creemProcessing, processCreemPayment } = useCreemPayment()
  const { processWaffoPayment } = useWaffoPayment()
  const { processing: pancakeProcessing, processWaffoPancakePayment } =
    useWaffoPancakePayment()

  // Fetch and refresh user data
  const fetchUser = useCallback(async () => {
    try {
      setUserLoading(true)
      const response = await getSelf()
      if (response.success && response.data) {
        setUser(response.data as UserWalletData)
      }
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to fetch user data:', error)
    } finally {
      setUserLoading(false)
    }
  }, [])

  const refreshPaymentData = useCallback(async () => {
    await Promise.all([
      fetchUser(),
      refetchTopupInfo(),
      queryClient.invalidateQueries({ queryKey: ['referral'] }),
      queryClient.invalidateQueries({ queryKey: ['referral-summary'] }),
      queryClient.invalidateQueries({ queryKey: ['referral-commissions'] }),
      queryClient.invalidateQueries({ queryKey: ['referral-withdrawals'] }),
      queryClient.invalidateQueries({ queryKey: ['user-subscriptions'] }),
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] }),
    ])
    setPaymentRefreshKey((key) => key + 1)
  }, [fetchUser, queryClient, refetchTopupInfo])

  const checkPaymentStatus = useCallback(
    async (tradeNo?: string): Promise<boolean> => {
      if (!tradeNo) return false

      const response = await getUserBillingHistory(1, 1, tradeNo)
      if (!isApiSuccess(response) || !response.data) return false

      const record = (response.data.items || []).find(
        (item) => item.trade_no === tradeNo
      )
      return record?.status === 'success'
    },
    []
  )

  const getPaymentSuccessMessage = useCallback(
    (payment?: PaymentSuccessMessageSource) => {
      if (
        payment?.paymentKind === 'topup' &&
        typeof payment.amount === 'number'
      ) {
        return t(
          'Payment completed. Top-up balance {{amount}} has been credited.',
          {
            amount: formatSiteCreditAmount(payment.amount),
          }
        )
      }

      if (payment?.paymentKind === 'subscription') {
        return t('Payment completed. {{title}} subscription is now active.', {
          title: payment.title || t('Subscription'),
        })
      }

      return t('Payment completed. Refreshing account data...')
    },
    [t]
  )

  const startPendingPayment = useCallback(
    (payment?: PaymentInitiationResult | string) => {
      const payload =
        typeof payment === 'string'
          ? ({ ok: true, tradeNo: payment } satisfies PaymentInitiationResult)
          : payment
      setPendingPayment({
        tradeNo: payload?.tradeNo,
        amount: payload?.amount,
        payAmount: payload?.payAmount,
        paymentMethod: payload?.paymentMethod,
        paymentKind: payload?.paymentKind,
        title: payload?.title,
        status: 'waiting',
        dialogOpen: true,
      })
    },
    []
  )

  const markPendingPaymentCompleted = useCallback((tradeNo: string) => {
    setPendingPayment((current) => {
      if (!current || current.tradeNo !== tradeNo) return current
      return { ...current, status: 'completed' }
    })
  }, [])

  useEffect(() => {
    fetchUser()
  }, [fetchUser])

  useEffect(() => {
    if (!pendingPayment || pendingPayment.status !== 'waiting') return
    if (!pendingPayment.tradeNo) return

    const pendingTradeNo = pendingPayment.tradeNo
    let canceled = false

    const poll = async () => {
      if (canceled) return

      const completed = await checkPaymentStatus(pendingTradeNo)
      if (canceled) return

      if (completed) {
        markPendingPaymentCompleted(pendingTradeNo)
        await refreshPaymentData()
        toast.success(getPaymentSuccessMessage(pendingPayment))
        setPendingPayment(null)
      }
    }

    void poll()
    const intervalId = window.setInterval(() => {
      void poll()
    }, PAYMENT_POLL_INTERVAL_MS)

    return () => {
      canceled = true
      window.clearInterval(intervalId)
    }
  }, [
    checkPaymentStatus,
    getPaymentSuccessMessage,
    markPendingPaymentCompleted,
    pendingPayment,
    refreshPaymentData,
    t,
  ])

  useEffect(() => {
    if (!pendingPayment || pendingPayment.status !== 'waiting') return
    const pendingTradeNo = pendingPayment.tradeNo

    const refreshOnReturn = () => {
      if (document.visibilityState === 'hidden') return
      void (async () => {
        await refreshPaymentData()
        if (pendingTradeNo) {
          const completed = await checkPaymentStatus(pendingTradeNo)
          if (completed) {
            markPendingPaymentCompleted(pendingTradeNo)
            toast.success(getPaymentSuccessMessage(pendingPayment))
            setPendingPayment(null)
          }
        }
      })()
    }

    window.addEventListener('focus', refreshOnReturn)
    document.addEventListener('visibilitychange', refreshOnReturn)
    return () => {
      window.removeEventListener('focus', refreshOnReturn)
      document.removeEventListener('visibilitychange', refreshOnReturn)
    }
  }, [
    checkPaymentStatus,
    getPaymentSuccessMessage,
    markPendingPaymentCompleted,
    pendingPayment,
    refreshPaymentData,
    t,
  ])

  useEffect(() => {
    if (props.initialShowHistory) {
      setBillingDialogOpen(true)
      window.history.replaceState({}, '', window.location.pathname)
    }
  }, [props.initialShowHistory])

  const returnedPay = props.paymentReturn?.pay
  const returnedShowHistory = props.paymentReturn?.show_history
  const returnedTradeNo = props.paymentReturn?.trade_no
  const returnedPaymentProvider = props.paymentReturn?.payment_provider
  const returnedOrderType = props.paymentReturn?.order_type

  useEffect(() => {
    if (!returnedPay && !returnedShowHistory) return

    const paymentReturnKey = [
      returnedPay || '',
      returnedShowHistory ? '1' : '0',
      returnedPaymentProvider || '',
      returnedOrderType || '',
      returnedTradeNo || '',
    ].join('|')
    if (processedPaymentReturnKeyRef.current === paymentReturnKey) return
    processedPaymentReturnKeyRef.current = paymentReturnKey

    setBillingDialogOpen(true)
    void fetchUser()
    void refetchTopupInfo()
    setPaymentRefreshKey((key) => key + 1)

    if (returnedPay === 'success') {
      toast.success(
        getPaymentSuccessMessage({
          paymentKind: returnedOrderType,
        })
      )
    } else if (returnedPay === 'pending') {
      toast.info(t('Payment returned. Waiting for payment confirmation...'))
    } else if (returnedPay === 'fail') {
      toast.error(
        t('Payment verification failed. Please check your order history.')
      )
    }

    window.history.replaceState({}, '', window.location.pathname)
  }, [
    fetchUser,
    refetchTopupInfo,
    returnedPay,
    returnedPaymentProvider,
    returnedOrderType,
    returnedShowHistory,
    returnedTradeNo,
    getPaymentSuccessMessage,
    t,
  ])

  // Initialize topup amount when topup info is loaded
  useEffect(() => {
    if (topupInfo && topupAmount === 0) {
      const minTopup = getMinTopupAmount(topupInfo)
      setTopupAmount(minTopup)

      // Calculate initial payment amount with default payment type
      const defaultPaymentType = getDefaultPaymentType(topupInfo)
      calculatePaymentAmount(minTopup, defaultPaymentType)
    }
  }, [topupInfo, topupAmount, calculatePaymentAmount])

  // Get current payment type (selected or default)
  const getCurrentPaymentType = useCallback(() => {
    return selectedPaymentMethod?.type || getDefaultPaymentType(topupInfo)
  }, [selectedPaymentMethod, topupInfo])

  // Handle preset selection
  const handleSelectPreset = (preset: PresetAmount) => {
    setTopupAmount(preset.value)
    setSelectedPreset(preset.value)
    calculatePaymentAmount(preset.value, getCurrentPaymentType())
  }

  // Handle topup amount change
  const handleTopupAmountChange = (amount: number) => {
    setTopupAmount(amount)
    setSelectedPreset(null)
    calculatePaymentAmount(amount, getCurrentPaymentType())
  }

  // Handle payment method selection
  const handlePaymentMethodSelect = async (method: PaymentMethod) => {
    setSelectedPaymentMethod(method)
    setPaymentLoading(method.type)

    try {
      // Validate minimum topup
      const minTopup = method.min_topup || getMinTopupAmount(topupInfo)
      if (topupAmount < minTopup) {
        return
      }

      // Calculate payment amount and show confirmation dialog
      await calculatePaymentAmount(topupAmount, method.type)
      setConfirmDialogOpen(true)
    } finally {
      setPaymentLoading(null)
    }
  }

  // Handle payment confirmation
  const handlePaymentConfirm = async () => {
    if (!selectedPaymentMethod) return

    const isPancake = isWaffoPancakePayment(selectedPaymentMethod.type)
    const paymentResult = isPancake
      ? await processWaffoPancakePayment(topupAmount)
      : await processPayment(topupAmount, selectedPaymentMethod.type)

    if (paymentResult.ok) {
      setConfirmDialogOpen(false)
      startPendingPayment(paymentResult)
      await refreshPaymentData()
    }
  }

  // Handle redemption
  const handleRedeem = async () => {
    if (!redemptionCode) return

    const success = await redeemCode(redemptionCode)
    if (success) {
      setRedemptionCode('')
      await fetchUser()
    }
  }

  // Handle Creem product selection
  const handleCreemProductSelect = (product: CreemProduct) => {
    setSelectedCreemProduct(product)
    setCreemDialogOpen(true)
  }

  // Handle Creem payment confirmation
  const handleCreemConfirm = async () => {
    if (!selectedCreemProduct) return

    const paymentResult = await processCreemPayment(
      selectedCreemProduct.productId
    )
    if (paymentResult.ok) {
      setCreemDialogOpen(false)
      setSelectedCreemProduct(null)
      startPendingPayment({
        ...paymentResult,
        amount: selectedCreemProduct.quota,
        payAmount: selectedCreemProduct.price,
        paymentMethod: selectedCreemProduct.currency,
        paymentKind: 'topup',
        title: selectedCreemProduct.name,
      })
      await refreshPaymentData()
    }
  }

  const handleWaffoMethodSelect = async (_method: unknown, index: number) => {
    const loadingKey = `waffo-${index}`
    setPaymentLoading(loadingKey)

    try {
      const paymentResult = await processWaffoPayment(topupAmount, index)
      if (paymentResult.ok) {
        startPendingPayment(paymentResult)
        await refreshPaymentData()
      }
    } finally {
      setPaymentLoading(null)
    }
  }

  const handleManualPaymentRefresh = async () => {
    await refreshPaymentData()
    if (!pendingPayment?.tradeNo) return

    const completed = await checkPaymentStatus(pendingPayment.tradeNo)
    if (completed) {
      markPendingPaymentCompleted(pendingPayment.tradeNo)
      setPendingPayment(null)
      toast.success(getPaymentSuccessMessage(pendingPayment))
    } else {
      toast.info(t('Payment is still pending. Please check again later.'))
    }
  }

  const handlePendingPaymentOpenChange = (open: boolean) => {
    setPendingPayment((current) => {
      if (!current) return current
      if (!open && current.status === 'completed') return null
      return { ...current, dialogOpen: open }
    })
  }

  const pendingPaymentRows = [
    pendingPayment?.paymentKind
      ? {
          label: t('Order Type'),
          value:
            pendingPayment.paymentKind === 'subscription'
              ? t('Subscription')
              : t('Top-up'),
        }
      : null,
    pendingPayment?.title
      ? {
          label: t('Product'),
          value: pendingPayment.title,
        }
      : null,
    typeof pendingPayment?.amount === 'number'
      ? {
          label:
            pendingPayment.paymentKind === 'subscription'
              ? t('Amount Due')
              : t('Topup Amount'),
          value:
            pendingPayment.paymentKind === 'subscription'
              ? `¥${pendingPayment.amount.toFixed(2)}`
              : formatSiteCreditAmount(pendingPayment.amount),
        }
      : null,
    pendingPayment?.paymentKind === 'topup' &&
    typeof pendingPayment?.payAmount === 'number'
      ? {
          label: t('You Pay'),
          value: formatPaymentCnyAmount(pendingPayment.payAmount),
        }
      : null,
    pendingPayment?.paymentMethod
      ? {
          label: t('Payment Method'),
          value: getPaymentMethodName(pendingPayment.paymentMethod, t),
        }
      : null,
  ].filter(Boolean) as Array<{ label: string; value: string }>

  // Get discount rate for current topup amount
  const getDiscountRate = useCallback(() => {
    return topupInfo?.discount?.[topupAmount] || DEFAULT_DISCOUNT_RATE
  }, [topupInfo, topupAmount])

  const handleSubscriptionAvailabilityChange = useCallback(
    (available: boolean) => {
      setShowSubscriptionPanel(available)
    },
    []
  )

  const refreshWalletPricing = useCallback(async () => {
    const [, latestTopupInfo] = await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['status'] }),
      refetchTopupInfo(),
    ])
    const paymentType =
      selectedPaymentMethod?.type ||
      getDefaultPaymentType(latestTopupInfo || topupInfo)
    calculatePaymentAmount(topupAmount, paymentType)
  }, [
    calculatePaymentAmount,
    queryClient,
    refetchTopupInfo,
    selectedPaymentMethod?.type,
    topupInfo,
    topupAmount,
  ])

  useEffect(() => {
    return subscribeSettingsRefresh((payload) => {
      const keys = payload.keys || []
      const shouldRefresh =
        keys.length === 0 ||
        keys.some((key) =>
          [
            'Price',
            'USDExchangeRate',
            'QuotaPerUnit',
            'DisplayInCurrencyEnabled',
            'general_setting.quota_display_type',
            'general_setting.custom_currency_symbol',
            'general_setting.custom_currency_exchange_rate',
            'payment_setting.wallet_notice',
            'TopUpLink',
          ].includes(key)
        )

      if (shouldRefresh) {
        void refreshWalletPricing()
      }
    })
  }, [refreshWalletPricing])

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Wallet')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
            <WalletStatsCard user={user} loading={userLoading} />

            <WalletNoticeAlert
              notice={topupInfo?.wallet_notice}
              topupLink={topupInfo?.topup_link}
            />

            <div
              className={
                showSubscriptionPanel
                  ? 'grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(360px,0.95fr)] xl:items-start'
                  : 'grid gap-4'
              }
            >
              <div id='wallet-add-funds' className='scroll-mt-4'>
                <RechargeFormCard
                  topupInfo={topupInfo}
                  presetAmounts={presetAmounts}
                  selectedPreset={selectedPreset}
                  onSelectPreset={handleSelectPreset}
                  topupAmount={topupAmount}
                  onTopupAmountChange={handleTopupAmountChange}
                  paymentAmount={paymentAmount}
                  calculating={calculating}
                  onPaymentMethodSelect={handlePaymentMethodSelect}
                  paymentLoading={paymentLoading}
                  redemptionCode={redemptionCode}
                  onRedemptionCodeChange={setRedemptionCode}
                  onRedeem={handleRedeem}
                  redeeming={redeeming}
                  topupLink={topupInfo?.topup_link}
                  loading={topupLoading}
                  priceRatio={(status?.price as number) || 1}
                  usdExchangeRate={effectiveUsdExchangeRate}
                  onOpenBilling={() => setBillingDialogOpen(true)}
                  creemProducts={topupInfo?.creem_products}
                  enableCreemTopup={topupInfo?.enable_creem_topup}
                  onCreemProductSelect={handleCreemProductSelect}
                  enableWaffoTopup={topupInfo?.enable_waffo_topup}
                  waffoPayMethods={topupInfo?.waffo_pay_methods}
                  waffoMinTopup={topupInfo?.waffo_min_topup}
                  onWaffoMethodSelect={handleWaffoMethodSelect}
                  enableWaffoPancakeTopup={
                    topupInfo?.enable_waffo_pancake_topup
                  }
                />
              </div>

              <SubscriptionPlansCard
                topupInfo={topupInfo}
                onAvailabilityChange={handleSubscriptionAvailabilityChange}
                refreshKey={paymentRefreshKey}
                onBalancePurchaseSuccess={refreshPaymentData}
                onPaymentStarted={startPendingPayment}
              />
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PaymentConfirmDialog
        open={confirmDialogOpen}
        onOpenChange={setConfirmDialogOpen}
        onConfirm={handlePaymentConfirm}
        topupAmount={topupAmount}
        paymentAmount={paymentAmount}
        paymentMethod={selectedPaymentMethod}
        calculating={calculating}
        processing={processing || pancakeProcessing}
        discountRate={getDiscountRate()}
        usdExchangeRate={effectiveUsdExchangeRate}
      />

      <BillingHistoryDialog
        open={billingDialogOpen}
        onOpenChange={setBillingDialogOpen}
        refreshKey={paymentRefreshKey}
      />

      <CreemConfirmDialog
        open={creemDialogOpen}
        onOpenChange={setCreemDialogOpen}
        onConfirm={handleCreemConfirm}
        product={selectedCreemProduct}
        processing={creemProcessing}
      />

      <AlertDialog
        open={Boolean(pendingPayment?.dialogOpen)}
        onOpenChange={handlePendingPaymentOpenChange}
      >
        <AlertDialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
          <AlertDialogHeader>
            <AlertDialogMedia
              className={
                pendingPayment?.status === 'completed'
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
                  : 'bg-muted text-muted-foreground'
              }
            >
              {pendingPayment?.status === 'completed' ? (
                <CheckCircle2 className='h-6 w-6' />
              ) : (
                <Clock3 className='h-6 w-6' />
              )}
            </AlertDialogMedia>
            <AlertDialogTitle className='text-xl font-semibold'>
              {pendingPayment?.status === 'completed'
                ? t('Payment completed')
                : t('Waiting for payment confirmation')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingPayment?.status === 'completed'
                ? t('Account data has been refreshed.')
                : t(
                    'The payment page has opened. If payment is confirmed, this page will refresh automatically.'
                  )}
            </AlertDialogDescription>
          </AlertDialogHeader>

          <div className='space-y-3 py-3 sm:space-y-4 sm:py-4'>
            <div className='bg-muted/50 rounded-lg p-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Order Number')}
              </div>
              <div className='mt-1 text-sm font-medium break-all'>
                {pendingPayment?.tradeNo || t('Pending payment order')}
              </div>
            </div>

            {pendingPaymentRows.length > 0 && (
              <div className='bg-muted/50 divide-y rounded-lg p-3'>
                {pendingPaymentRows.map((row) => (
                  <div
                    key={row.label}
                    className='flex items-center justify-between gap-3 py-2 first:pt-0 last:pb-0'
                  >
                    <span className='text-muted-foreground text-xs'>
                      {row.label}
                    </span>
                    <span className='max-w-[220px] truncate text-right text-sm font-medium'>
                      {row.value}
                    </span>
                  </div>
                ))}
              </div>
            )}

            {pendingPayment?.status === 'waiting' && (
              <div className='text-muted-foreground text-sm'>
                {t(
                  'You can close this dialog. We will keep checking in the background and refresh your account after the payment is confirmed.'
                )}
              </div>
            )}
          </div>

          <AlertDialogFooter className='grid grid-cols-2 gap-2 sm:flex'>
            <AlertDialogCancel>{t('Close')}</AlertDialogCancel>
            <AlertDialogAction onClick={handleManualPaymentRefresh}>
              <RefreshCw className='mr-2 h-4 w-4' />
              {t('Refresh')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
