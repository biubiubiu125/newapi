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
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertTriangle,
  Ban,
  CheckCircle2,
  Eye,
  KeyRound,
  Network,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  UserRound,
} from 'lucide-react'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'
import {
  banRiskUser,
  createRiskWhitelist,
  deleteRiskWhitelist,
  disableRiskToken,
  getRiskActions,
  getRiskDetail,
  getRiskEvents,
  getRiskOverview,
  getRiskUsers,
  ignoreRiskEvent,
  markRiskEventViewed,
  resolveRiskEvent,
  scanRiskEvents,
  unbanRiskUser,
  type RiskAction,
  type RiskDetail,
  type RiskEvent,
  type RiskIP,
  type RiskLog,
  type RiskOrder,
  type RiskReferral,
  type RiskSignal,
  type RiskToken,
  type RiskUser,
  type RiskWhitelist,
} from './api'

type OverviewData = {
  window_hours: number
  signal_count: number
  open_event_count: number
  high_event_count: number
  disabled_users: number
  new_user_count: number
  signals: RiskSignal[]
}

type DetailKind = 'event' | 'user' | 'ip' | 'token' | 'order' | 'referral'

type DetailSelection = {
  kind: DetailKind
  title: string
  params: URLSearchParams
}

type WhitelistTarget = {
  target_type: string
  target_id: string
  label: string
}

const DEFAULT_WINDOW_HOURS = 24
const USER_STATUS_DISABLED = 2

function formatTime(timestamp?: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

function formatMoney(amount?: number, currency?: string) {
  const normalizedCurrency = (currency || 'CNY').toUpperCase()
  const value = Number(amount || 0)
  if (normalizedCurrency === 'CNY') return `\u00a5${value.toFixed(2)}`
  if (normalizedCurrency === 'USD') return `$${value.toFixed(2)}`
  if (normalizedCurrency === 'USDT') return `${value.toFixed(6)} USDT`
  return `${value.toFixed(2)} ${normalizedCurrency}`
}

function formatNumber(value?: number) {
  return Number(value || 0).toLocaleString()
}

function severityVariant(severity?: string): StatusVariant {
  switch (severity) {
    case 'high':
      return 'danger'
    case 'warning':
      return 'warning'
    case 'info':
      return 'info'
    case 'normal':
      return 'success'
    default:
      return 'neutral'
  }
}

function statusVariant(status?: string): StatusVariant {
  switch (status) {
    case 'open':
      return 'danger'
    case 'viewed':
      return 'warning'
    case 'resolved':
      return 'success'
    case 'ignored':
      return 'neutral'
    default:
      return 'neutral'
  }
}

function severityLabel(severity?: string) {
  switch (severity) {
    case 'high':
      return '高风险'
    case 'warning':
      return '预警'
    case 'info':
      return '提示'
    case 'normal':
      return '正常'
    default:
      return severity || '-'
  }
}

function eventStatusLabel(status?: string) {
  switch (status) {
    case 'open':
      return '待处理'
    case 'viewed':
      return '已查看'
    case 'resolved':
      return '已处理'
    case 'ignored':
      return '已忽略'
    default:
      return status || '-'
  }
}

function riskTypeLabel(type?: string) {
  switch (type) {
    case 'shared_ip':
      return '同 IP 多账号'
    case 'high_error_count':
    case 'errors':
      return '错误日志过多'
    case 'high_topup_activity':
    case 'topups':
      return '充值异常'
    case 'new_user_high_consume':
    case 'consume':
      return '新号高消耗'
    case 'token_rotation':
      return 'Token/IP 轮换'
    case 'payment_anomaly':
      return '支付/佣金异常'
    case 'referral_anomaly':
      return '推广返佣异常'
    case 'new_users':
      return '新注册用户'
    case 'disabled_users':
      return '禁用用户'
    case 'user':
    case 'user_detail':
      return '用户风险画像'
    case 'ip_detail':
      return 'IP 风险画像'
    case 'token_detail':
      return 'Token 风险画像'
    case 'order_detail':
      return '订单风险画像'
    default:
      return type || '风险详情'
  }
}

function targetTypeLabel(type?: string) {
  switch (type) {
    case 'user':
      return '用户'
    case 'ip':
      return 'IP'
    case 'token':
      return 'Token'
    case 'order':
      return '订单'
    case 'referral':
      return '推广'
    case 'rule':
      return '规则'
    default:
      return type || '-'
  }
}

function actionLabel(action?: string) {
  switch (action) {
    case 'viewed':
      return '查看'
    case 'resolved':
      return '处理完成'
    case 'ignored':
      return '忽略'
    case 'ban_user':
      return '封禁用户'
    case 'unban_user':
      return '解除封禁'
    case 'disable_token':
      return '禁用 Token'
    case 'whitelist':
      return '加入白名单'
    case 'remove_whitelist':
      return '移除白名单'
    case 'note':
      return '备注'
    default:
      return action || '-'
  }
}

function logTypeLabel(type?: number) {
  switch (type) {
    case 1:
      return '充值'
    case 2:
      return '消耗'
    case 3:
      return '管理'
    case 4:
      return '系统'
    case 5:
      return '错误'
    case 6:
      return '退款'
    default:
      return '未知'
  }
}

function orderTypeLabel(type?: string) {
  switch (type) {
    case 'subscription':
      return '订阅'
    case 'topup':
      return '充值'
    default:
      return type || '-'
  }
}

function orderStatusLabel(status?: string) {
  switch ((status || '').toLowerCase()) {
    case 'success':
      return '成功'
    case 'pending':
      return '待支付'
    case 'failed':
      return '失败'
    case 'expired':
      return '已过期'
    default:
      return status || '-'
  }
}

function orderStatusVariant(status?: string): StatusVariant {
  switch ((status || '').toLowerCase()) {
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

function referralStatusLabel(status?: string, error?: string) {
  if (error) return referralErrorLabel(error)
  switch (status) {
    case 'pending':
      return '待生成'
    case 'processing':
      return '处理中'
    case 'skipped':
      return '已跳过'
    case 'succeeded':
      return '已生成'
    case 'failed':
      return '失败'
    default:
      return status || '-'
  }
}

function referralErrorLabel(error?: string) {
  switch (error) {
    case 'fx_rate_missing':
      return '佣金汇率缺失'
    case 'missing_referral_snapshot':
      return '订单缺少推广快照'
    case 'zero_commission_amount':
      return '佣金金额为 0'
    case 'affiliate_not_eligible':
      return '推广员不可结算'
    case 'no_referral_binding':
      return '无有效邀请绑定'
    case 'commission_disabled':
      return '返佣未启用'
    default:
      return error || '-'
  }
}

function paymentMethodLabel(method?: string) {
  switch ((method || '').toLowerCase()) {
    case 'alipay':
      return '支付宝'
    case 'wxpay':
      return '微信支付'
    case 'usdt':
      return 'USDT'
    default:
      return method || '-'
  }
}

function userStatusLabel(status?: number) {
  return status === USER_STATUS_DISABLED ? '已禁用' : '正常'
}

function safeParams(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === '' || value === 0) return
    search.set(key, String(value))
  })
  return search
}

function riskSignalMatchesEvent(signal: RiskSignal, event: RiskEvent) {
  if (signal.event_key) return signal.event_key === event.event_key
  if (signal.type !== event.type) return false
  if (signal.target_type && signal.target_type !== event.target_type) return false
  if (signal.target_id && signal.target_id !== event.target_id) return false
  if (signal.ip && signal.ip !== event.ip) return false
  if (signal.user_id && signal.user_id !== event.user_id) return false
  if (signal.token_id && signal.token_id !== event.token_id) return false
  if (signal.trade_no && signal.trade_no !== event.trade_no) return false
  return true
}

function useRiskCenterData(windowHours: number, keyword: string) {
  const [overview, setOverview] = useState<OverviewData | null>(null)
  const [events, setEvents] = useState<RiskEvent[]>([])
  const [users, setUsers] = useState<RiskUser[]>([])
  const [actions, setActions] = useState<RiskAction[]>([])
  const [loading, setLoading] = useState(false)
  const keywordRef = useRef(keyword)

  useEffect(() => {
    keywordRef.current = keyword
  }, [keyword])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const currentKeyword = keywordRef.current.trim()
      const base = safeParams({ window_hours: windowHours })
      const eventParams = safeParams({
        window_hours: windowHours,
        page_size: 50,
        status: 'open',
        keyword: currentKeyword || undefined,
      })
      const userParams = safeParams({
        window_hours: windowHours,
        page_size: 50,
        keyword: currentKeyword || undefined,
      })
      const actionParams = safeParams({ page_size: 20 })
      const [overviewRes, eventsRes, usersRes, actionsRes] = await Promise.all([
        getRiskOverview(base),
        getRiskEvents(eventParams),
        getRiskUsers(userParams),
        getRiskActions(actionParams),
      ])
      if (!overviewRes.success) throw new Error(overviewRes.message)
      if (!eventsRes.success) throw new Error(eventsRes.message)
      if (!usersRes.success) throw new Error(usersRes.message)
      if (!actionsRes.success) throw new Error(actionsRes.message)
      setOverview(overviewRes.data)
      setEvents(eventsRes.data.items || [])
      setUsers(usersRes.data.items || [])
      setActions(actionsRes.data.items || [])
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '加载风控数据失败')
    } finally {
      setLoading(false)
    }
  }, [windowHours])

  useEffect(() => {
    void load()
  }, [load])

  return {
    overview,
    events,
    users,
    actions,
    loading,
    load,
  }
}

export function RiskCenter() {
  const { t } = useTranslation()
  const [windowHours, setWindowHours] = useState(DEFAULT_WINDOW_HOURS)
  const [keyword, setKeyword] = useState('')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [selection, setSelection] = useState<DetailSelection | null>(null)
  const [detail, setDetail] = useState<RiskDetail | null>(null)
  const [reason, setReason] = useState('')
  const [scanLoading, setScanLoading] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  const { overview, events, users, actions, loading, load } = useRiskCenterData(
    windowHours,
    keyword
  )

  const ipTokenEvents = useMemo(
    () =>
      events.filter(
        (event) =>
          event.target_type === 'ip' ||
          event.target_type === 'token' ||
          event.type === 'shared_ip' ||
          event.type === 'token_rotation'
      ),
    [events]
  )

  const openDetail = async (nextSelection: DetailSelection, markViewed = false) => {
    setSelection(nextSelection)
    setDetailOpen(true)
    setDetailLoading(true)
    setReason('')
    try {
      if (markViewed) {
        const eventId = Number(nextSelection.params.get('event_id') || 0)
        if (eventId > 0) {
          await markRiskEventViewed(eventId)
        }
      }
      const res = await getRiskDetail(nextSelection.params)
      if (!res.success) throw new Error(res.message)
      setDetail(res.data)
      if (markViewed) {
        void load()
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '加载风险详情失败')
    } finally {
      setDetailLoading(false)
    }
  }

  const openEvent = (event: RiskEvent) => {
    void openDetail(
      {
        kind: 'event',
        title: event.title || riskTypeLabel(event.type),
        params: safeParams({
          window_hours: windowHours,
          type: event.type,
          event_id: event.id,
          ip: event.ip,
          user_id: event.user_id,
          token_id: event.token_id,
          trade_no: event.trade_no,
        }),
      },
      true
    )
  }

  const openUser = (user: RiskUser) => {
    void openDetail({
      kind: 'user',
      title: `${user.username || `#${user.user_id}`} 的风险画像`,
      params: safeParams({
        window_hours: windowHours,
        type: 'user_detail',
        user_id: user.user_id,
      }),
    })
  }

  const openIP = (ip: string) => {
    void openDetail({
      kind: 'ip',
      title: `${ip} 的 IP 风险画像`,
      params: safeParams({
        window_hours: windowHours,
        type: 'ip_detail',
        ip,
      }),
    })
  }

  const openToken = (tokenId: number, tokenName?: string) => {
    void openDetail({
      kind: 'token',
      title: `${tokenName || `Token #${tokenId}`} 的风险画像`,
      params: safeParams({
        window_hours: windowHours,
        type: 'token_detail',
        token_id: tokenId,
      }),
    })
  }

  const openOrder = (tradeNo: string) => {
    void openDetail({
      kind: 'order',
      title: `${tradeNo} 的订单风险画像`,
      params: safeParams({
        window_hours: windowHours,
        type: 'order_detail',
        trade_no: tradeNo,
      }),
    })
  }

  const openReferral = (row: RiskReferral) => {
    void openDetail({
      kind: 'referral',
      title: `${row.inviter_username || `#${row.inviter_user_id}`} 的推广风险画像`,
      params: safeParams({
        window_hours: windowHours,
        type: 'referral_anomaly',
        user_id: row.inviter_user_id,
      }),
    })
  }

  const handleScan = async () => {
    setScanLoading(true)
    try {
      const res = await scanRiskEvents(safeParams({ window_hours: windowHours }))
      if (!res.success) throw new Error(res.message)
      toast.success(`扫描完成，发现 ${res.data.count} 个风险事件`)
      await load()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '扫描失败')
    } finally {
      setScanLoading(false)
    }
  }

  const handleProcessSignal = async (signal: RiskSignal) => {
    setScanLoading(true)
    try {
      const res = await scanRiskEvents(safeParams({ window_hours: windowHours }))
      if (!res.success) throw new Error(res.message)
      await load()
      const event = res.data.events.find((item) =>
        riskSignalMatchesEvent(signal, item)
      )
      if (!event) {
        toast.error('没有找到可处理的风险事件，请刷新后重试')
        return
      }
      openEvent(event)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '生成风险事件失败')
    } finally {
      setScanLoading(false)
    }
  }

  const currentEvent = detail?.event
  const primaryUser = detail?.users?.[0]
  const primaryToken = detail?.tokens?.[0]
  const primaryIP = detail?.ips?.[0]
  const primaryWhitelistTarget = getPrimaryWhitelistTarget(
    selection?.kind,
    detail
  )

  const refreshDetail = async () => {
    if (!selection) return
    await openDetail(selection)
  }

  const runAction = async (action: () => Promise<{ success: boolean; message?: string }>, successText: string) => {
    setActionLoading(true)
    try {
      const res = await action()
      if (!res.success) throw new Error(res.message)
      toast.success(successText)
      await Promise.all([load(), refreshDetail()])
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '操作失败')
    } finally {
      setActionLoading(false)
    }
  }

  const requireReason = () => {
    const text = reason.trim()
    if (!text) {
      toast.error('请输入处理原因')
      return ''
    }
    return text
  }

  const actionPayload = () => ({
    event_id: currentEvent?.id,
    reason: reason.trim(),
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        风险中心用于集中发现异常账号、IP、Token、订单和推广返佣问题，所有处置都保留人工操作记录。
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Input
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') void load()
          }}
          placeholder='搜索用户、IP、订单、Token'
          className='h-8 w-56'
        />
        <Input
          type='number'
          min={1}
          max={168}
          value={windowHours}
          onChange={(event) =>
            setWindowHours(Math.max(1, Number(event.target.value || 1)))
          }
          className='h-8 w-24'
        />
        <Button variant='outline' onClick={() => void load()} disabled={loading}>
          <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
          刷新
        </Button>
        <Button onClick={handleScan} disabled={scanLoading}>
          <ShieldAlert className={cn('size-4', scanLoading && 'animate-pulse')} />
          扫描风险
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <OverviewCards overview={overview} loading={loading} />

          <Tabs defaultValue='queue' className='space-y-3'>
            <TabsList className='w-full justify-start overflow-x-auto'>
              <TabsTrigger value='queue'>待处理队列</TabsTrigger>
              <TabsTrigger value='users'>用户排行</TabsTrigger>
              <TabsTrigger value='ip-token'>IP 与 Token</TabsTrigger>
              <TabsTrigger value='actions'>处置记录</TabsTrigger>
            </TabsList>

            <TabsContent value='queue' className='space-y-3'>
              <RiskSignals
                signals={overview?.signals || []}
                processing={scanLoading || detailLoading}
                onProcessSignal={handleProcessSignal}
              />
              <RiskEventQueue events={events} onOpenEvent={openEvent} />
            </TabsContent>

            <TabsContent value='users'>
              <RiskUserTable users={users} onOpenUser={openUser} />
            </TabsContent>

            <TabsContent value='ip-token'>
              <IPTokenPanel
                events={ipTokenEvents}
                users={users}
                onOpenEvent={openEvent}
                onOpenUser={openUser}
              />
            </TabsContent>

            <TabsContent value='actions'>
              <RiskActionList actions={actions} />
            </TabsContent>
          </Tabs>
        </div>

        <Sheet open={detailOpen} onOpenChange={setDetailOpen}>
          <SheetContent className='w-full overflow-y-auto sm:max-w-4xl'>
            <SheetHeader className='border-b'>
              <SheetTitle>{selection?.title || '风险详情'}</SheetTitle>
            </SheetHeader>
            {detailLoading ? (
              <div className='flex min-h-80 items-center justify-center text-sm text-muted-foreground'>
                正在加载风险详情...
              </div>
            ) : detail ? (
              <div className='space-y-4 p-4'>
                <RiskDetailSummary detail={detail} selection={selection} />

                <DetailGrid>
                  {detail.users.length > 0 && (
                    <DetailBlock title='关联用户'>
                      <RiskUserCards users={detail.users} onOpenUser={openUser} />
                    </DetailBlock>
                  )}

                  {detail.ips.length > 0 && (
                    <DetailBlock title='关联 IP'>
                      <RiskIPCards ips={detail.ips} onOpenIP={openIP} />
                    </DetailBlock>
                  )}

                  {detail.tokens.length > 0 && (
                    <DetailBlock title='关联 Token'>
                      <RiskTokenCards
                        tokens={detail.tokens}
                        onOpenToken={openToken}
                      />
                    </DetailBlock>
                  )}

                  {detail.orders.length > 0 && (
                    <DetailBlock title='关联订单'>
                      <RiskOrderCards orders={detail.orders} onOpenOrder={openOrder} />
                    </DetailBlock>
                  )}

                  {detail.referrals.length > 0 && (
                    <DetailBlock title='推广返佣'>
                      <RiskReferralCards
                        referrals={detail.referrals}
                        onOpenReferral={openReferral}
                      />
                    </DetailBlock>
                  )}

                  {detail.logs.length > 0 && (
                    <DetailBlock title='近期日志'>
                      <RiskLogCards logs={detail.logs} />
                    </DetailBlock>
                  )}
                </DetailGrid>

                <RiskActionPanel
                  reason={reason}
                  setReason={setReason}
                  actionLoading={actionLoading}
                  event={currentEvent}
                  user={primaryUser}
                  token={primaryToken}
                  ip={primaryIP}
                  whitelistTarget={primaryWhitelistTarget}
                  whitelists={detail.whitelists}
                  onResolve={() => {
                    const text = requireReason()
                    if (!text || !currentEvent) return
                    void runAction(
                      () => resolveRiskEvent(currentEvent.id, actionPayload()),
                      '风险事件已处理'
                    )
                  }}
                  onIgnore={() => {
                    const text = requireReason()
                    if (!text || !currentEvent) return
                    void runAction(
                      () => ignoreRiskEvent(currentEvent.id, actionPayload()),
                      '风险事件已忽略'
                    )
                  }}
                  onBanUser={() => {
                    const text = requireReason()
                    if (!text || !primaryUser) return
                    void runAction(
                      () =>
                        banRiskUser(primaryUser.user_id, {
                          ...actionPayload(),
                          user_id: primaryUser.user_id,
                        }),
                      '用户已封禁'
                    )
                  }}
                  onUnbanUser={() => {
                    const text = requireReason()
                    if (!text || !primaryUser) return
                    void runAction(
                      () =>
                        unbanRiskUser(primaryUser.user_id, {
                          ...actionPayload(),
                          user_id: primaryUser.user_id,
                        }),
                      '用户已恢复'
                    )
                  }}
                  onDisableToken={() => {
                    const text = requireReason()
                    if (!text || !primaryToken) return
                    void runAction(
                      () =>
                        disableRiskToken(primaryToken.token_id, {
                          ...actionPayload(),
                          token_id: primaryToken.token_id,
                        }),
                      'Token 已禁用'
                    )
                  }}
                  onWhitelist={() => {
                    const text = requireReason()
                    if (!text || !primaryWhitelistTarget) return
                    void runAction(
                      () =>
                        createRiskWhitelist({
                          ...actionPayload(),
                          target_type: primaryWhitelistTarget.target_type,
                          target_id: primaryWhitelistTarget.target_id,
                        }),
                      '已加入风控白名单'
                    )
                  }}
                  onDeleteWhitelist={(id) => {
                    const text = requireReason()
                    if (!text) return
                    void runAction(
                      () => deleteRiskWhitelist(id, { reason: text }),
                      '白名单已移除'
                    )
                  }}
                />
              </div>
            ) : (
              <div className='p-4 text-sm text-muted-foreground'>暂无数据</div>
            )}
          </SheetContent>
        </Sheet>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function OverviewCards(props: { overview: OverviewData | null; loading: boolean }) {
  const { overview, loading } = props
  const cards = [
    {
      title: '待处理事件',
      value: overview?.open_event_count ?? 0,
      desc: '包含待处理与已查看',
      icon: ShieldAlert,
      variant: 'danger' as StatusVariant,
    },
    {
      title: '高风险事件',
      value: overview?.high_event_count ?? 0,
      desc: '需要优先复核',
      icon: AlertTriangle,
      variant: 'warning' as StatusVariant,
    },
    {
      title: '禁用用户',
      value: overview?.disabled_users ?? 0,
      desc: '当前已禁用账号',
      icon: Ban,
      variant: 'neutral' as StatusVariant,
    },
    {
      title: '新注册用户',
      value: overview?.new_user_count ?? 0,
      desc: `${overview?.window_hours || DEFAULT_WINDOW_HOURS} 小时窗口`,
      icon: UserRound,
      variant: 'info' as StatusVariant,
    },
  ]
  return (
    <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
      {cards.map((card) => {
        const Icon = card.icon
        return (
          <Card key={card.title}>
            <CardContent className='flex items-center justify-between gap-3 p-4'>
              <div className='min-w-0'>
                <div className='text-muted-foreground text-sm'>{card.title}</div>
                <div className='mt-1 text-2xl font-semibold'>
                  {loading ? '-' : formatNumber(card.value)}
                </div>
                <div className='text-muted-foreground mt-1 text-xs'>
                  {card.desc}
                </div>
              </div>
              <div className='bg-muted flex size-10 items-center justify-center rounded-lg'>
                <Icon className='size-5' />
              </div>
            </CardContent>
          </Card>
        )
      })}
    </div>
  )
}

function RiskSignals(props: {
  signals: RiskSignal[]
  processing: boolean
  onProcessSignal: (signal: RiskSignal) => void
}) {
  if (props.signals.length === 0) {
    return (
      <Card>
        <CardContent className='p-4 text-sm text-muted-foreground'>
          当前窗口没有新的实时信号。可以点击“扫描风险”生成待处理事件。
        </CardContent>
      </Card>
    )
  }
  return (
    <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
      {props.signals.slice(0, 8).map((signal, index) => (
        <Card key={`${signal.type}-${signal.ip || signal.user_id || index}`}>
          <CardContent className='p-4'>
            <div className='flex items-center justify-between gap-2'>
              <div className='font-medium'>{riskTypeLabel(signal.type)}</div>
              <StatusBadge
                label={severityLabel(signal.severity)}
                variant={severityVariant(signal.severity)}
                copyable={false}
              />
            </div>
            <div className='text-muted-foreground mt-2 line-clamp-2 text-sm'>
              {signal.message}
            </div>
            <div className='text-muted-foreground mt-3 flex items-center justify-between text-xs'>
              <span>命中 {formatNumber(signal.count)} 次</span>
              <span>{formatTime(signal.last_seen_at)}</span>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='mt-3 w-full'
              disabled={props.processing}
              onClick={() => props.onProcessSignal(signal)}
            >
              <ShieldCheck className='size-4' />
              处理
            </Button>
          </CardContent>
        </Card>
      ))}
    </div>
  )
}

function RiskEventQueue(props: {
  events: RiskEvent[]
  onOpenEvent: (event: RiskEvent) => void
}) {
  if (props.events.length === 0) {
    return <EmptyPanel text='暂无待处理风险事件。' />
  }
  return (
    <Card>
      <CardContent className='p-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>风险事件</TableHead>
              <TableHead>对象</TableHead>
              <TableHead>命中</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>最近发现</TableHead>
              <TableHead className='text-right'>操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.events.map((event) => (
              <TableRow key={event.id} className='cursor-pointer' onClick={() => props.onOpenEvent(event)}>
                <TableCell>
                  <div className='flex min-w-0 flex-col gap-1'>
                    <div className='flex items-center gap-2'>
                      <StatusBadge
                        label={severityLabel(event.severity)}
                        variant={severityVariant(event.severity)}
                        copyable={false}
                      />
                      <span className='font-medium'>{event.title || riskTypeLabel(event.type)}</span>
                    </div>
                    <div className='text-muted-foreground line-clamp-1 text-xs'>
                      {event.summary || event.event_key}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <div className='text-sm'>{targetTypeLabel(event.target_type)}</div>
                  <div className='text-muted-foreground max-w-52 truncate text-xs'>
                    {event.username || event.ip || event.token_name || event.trade_no || event.target_id}
                  </div>
                </TableCell>
                <TableCell>{formatNumber(event.hit_count)}</TableCell>
                <TableCell>
                  <StatusBadge
                    label={eventStatusLabel(event.status)}
                    variant={statusVariant(event.status)}
                    copyable={false}
                  />
                </TableCell>
                <TableCell>{formatTime(event.last_seen_at)}</TableCell>
                <TableCell className='text-right'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={(clickEvent) => {
                      clickEvent.stopPropagation()
                      props.onOpenEvent(event)
                    }}
                  >
                    处理
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function RiskUserTable(props: {
  users: RiskUser[]
  onOpenUser: (user: RiskUser) => void
}) {
  if (props.users.length === 0) {
    return <EmptyPanel text='暂无用户风险排行数据。' />
  }
  return (
    <Card>
      <CardContent className='p-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>用户</TableHead>
              <TableHead>风险</TableHead>
              <TableHead>充值</TableHead>
              <TableHead>错误</TableHead>
              <TableHead>IP / Token</TableHead>
              <TableHead>状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.users.map((user) => (
              <TableRow key={user.user_id} className='cursor-pointer' onClick={() => props.onOpenUser(user)}>
                <TableCell>
                  <div className='font-medium'>{user.username || `#${user.user_id}`}</div>
                  <div className='text-muted-foreground text-xs'>
                    ID {user.user_id} · {user.email || '-'}
                  </div>
                </TableCell>
                <TableCell>
                  <StatusBadge
                    label={severityLabel(user.severity)}
                    variant={severityVariant(user.severity)}
                    copyable={false}
                  />
                </TableCell>
                <TableCell>
                  <div>{formatMoney(user.topup_paid_amount, 'CNY')}</div>
                  <div className='text-muted-foreground text-xs'>
                    {formatNumber(user.topup_count)} 笔
                  </div>
                </TableCell>
                <TableCell>{formatNumber(user.error_count)}</TableCell>
                <TableCell>
                  {formatNumber(user.unique_ip_count)} / {formatNumber(user.token_count)}
                </TableCell>
                <TableCell>
                  <StatusBadge
                    label={userStatusLabel(user.status)}
                    variant={user.status === USER_STATUS_DISABLED ? 'danger' : 'success'}
                    copyable={false}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function IPTokenPanel(props: {
  events: RiskEvent[]
  users: RiskUser[]
  onOpenEvent: (event: RiskEvent) => void
  onOpenUser: (user: RiskUser) => void
}) {
  const topIPUsers = props.users.filter((user) => user.unique_ip_count > 1)
  return (
    <div className='grid gap-3 xl:grid-cols-2'>
      <Card>
        <CardContent className='space-y-3 p-4'>
          <div className='flex items-center gap-2 font-medium'>
            <Network className='size-4' />
            IP / Token 风险事件
          </div>
          {props.events.length === 0 ? (
            <EmptyText text='暂无 IP 或 Token 风险事件。' />
          ) : (
            <div className='space-y-2'>
              {props.events.map((event) => (
                <button
                  key={event.id}
                  type='button'
                  className='hover:bg-muted flex w-full items-center justify-between gap-3 rounded-lg border p-3 text-left'
                  onClick={() => props.onOpenEvent(event)}
                >
                  <div className='min-w-0'>
                    <div className='font-medium'>{event.title || riskTypeLabel(event.type)}</div>
                    <div className='text-muted-foreground truncate text-xs'>
                      {event.ip || event.token_name || event.target_id}
                    </div>
                  </div>
                  <StatusBadge
                    label={severityLabel(event.severity)}
                    variant={severityVariant(event.severity)}
                    copyable={false}
                  />
                </button>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardContent className='space-y-3 p-4'>
          <div className='flex items-center gap-2 font-medium'>
            <KeyRound className='size-4' />
            多 IP / Token 用户
          </div>
          {topIPUsers.length === 0 ? (
            <EmptyText text='暂无明显多 IP 或多 Token 用户。' />
          ) : (
            <div className='space-y-2'>
              {topIPUsers.slice(0, 12).map((user) => (
                <div
                  key={user.user_id}
                  className='flex items-center justify-between rounded-lg border p-3'
                >
                  <button
                    type='button'
                    className='min-w-0 text-left hover:underline'
                    onClick={() => props.onOpenUser(user)}
                  >
                    <div className='font-medium'>{user.username}</div>
                    <div className='text-muted-foreground text-xs'>
                      {formatNumber(user.unique_ip_count)} 个 IP ·{' '}
                      {formatNumber(user.token_count)} 个 Token
                    </div>
                  </button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => props.onOpenUser(user)}
                  >
                    查看画像
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function RiskActionList(props: { actions: RiskAction[] }) {
  if (props.actions.length === 0) {
    return <EmptyPanel text='暂无人工处置记录。' />
  }
  return (
    <Card>
      <CardContent className='p-0'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>动作</TableHead>
              <TableHead>对象</TableHead>
              <TableHead>处理人</TableHead>
              <TableHead>原因</TableHead>
              <TableHead>时间</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.actions.map((action) => (
              <TableRow key={action.id}>
                <TableCell>{actionLabel(action.action)}</TableCell>
                <TableCell>
                  <div>{targetTypeLabel(action.target_type)}</div>
                  <div className='text-muted-foreground max-w-52 truncate text-xs'>
                    {action.target_id || action.ip || action.user_id || '-'}
                  </div>
                </TableCell>
                <TableCell>{action.operator_name || `#${action.operator_user_id}`}</TableCell>
                <TableCell className='max-w-80 truncate'>{action.reason || '-'}</TableCell>
                <TableCell>{formatTime(action.created_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}

function RiskDetailSummary(props: {
  detail: RiskDetail
  selection: DetailSelection | null
}) {
  const { detail, selection } = props
  const event = detail.event
  return (
    <Card>
      <CardContent className='space-y-3 p-4'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <div className='text-base font-medium'>
              {event?.title || selection?.title || riskTypeLabel(detail.type)}
            </div>
            <div className='text-muted-foreground mt-1 text-sm'>
              {event?.summary || `${detail.window_hours} 小时窗口内的关联风险数据`}
            </div>
          </div>
          <div className='flex items-center gap-2'>
            {event && (
              <>
                <StatusBadge
                  label={severityLabel(event.severity)}
                  variant={severityVariant(event.severity)}
                  copyable={false}
                />
                <StatusBadge
                  label={eventStatusLabel(event.status)}
                  variant={statusVariant(event.status)}
                  copyable={false}
                />
              </>
            )}
          </div>
        </div>
        <div className='grid gap-2 text-sm md:grid-cols-4'>
          <InfoItem label='详情类型' value={riskTypeLabel(detail.type)} />
          <InfoItem label='窗口' value={`${detail.window_hours} 小时`} />
          <InfoItem label='对象' value={event?.target_id || detail.ip || detail.trade_no || '-'} />
          <InfoItem label='最近发现' value={formatTime(event?.last_seen_at)} />
        </div>
      </CardContent>
    </Card>
  )
}

function RiskActionPanel(props: {
  reason: string
  setReason: (value: string) => void
  actionLoading: boolean
  event?: RiskEvent
  user?: RiskUser
  token?: RiskToken
  ip?: RiskIP
  whitelistTarget?: WhitelistTarget | null
  whitelists: RiskWhitelist[]
  onResolve: () => void
  onIgnore: () => void
  onBanUser: () => void
  onUnbanUser: () => void
  onDisableToken: () => void
  onWhitelist: () => void
  onDeleteWhitelist: (id: number) => void
}) {
  return (
    <Card>
      <CardContent className='space-y-3 p-4'>
        <div className='flex items-center gap-2 font-medium'>
          <ShieldCheck className='size-4' />
          人工处置
        </div>
        <Textarea
          value={props.reason}
          onChange={(event) => props.setReason(event.target.value)}
          placeholder='填写处理原因，所有人工动作都会记录到风控处置日志。'
        />
        <div className='flex flex-wrap gap-2'>
          {props.event && (
            <>
              <Button
                variant='outline'
                disabled={props.actionLoading}
                onClick={props.onResolve}
              >
                <CheckCircle2 className='size-4' />
                标记已处理
              </Button>
              <Button
                variant='outline'
                disabled={props.actionLoading}
                onClick={props.onIgnore}
              >
                <Eye className='size-4' />
                忽略事件
              </Button>
            </>
          )}
          {props.user && props.user.status !== USER_STATUS_DISABLED && (
            <Button
              variant='destructive'
              disabled={props.actionLoading}
              onClick={props.onBanUser}
            >
              <Ban className='size-4' />
              封禁用户
            </Button>
          )}
          {props.user && props.user.status === USER_STATUS_DISABLED && (
            <Button
              variant='outline'
              disabled={props.actionLoading}
              onClick={props.onUnbanUser}
            >
              <UserRound className='size-4' />
              解除封禁
            </Button>
          )}
          {props.token && props.token.token_id > 0 && (
            <Button
              variant='outline'
              disabled={props.actionLoading}
              onClick={props.onDisableToken}
            >
              <KeyRound className='size-4' />
              禁用 Token
            </Button>
          )}
          {props.whitelistTarget && (
            <Button
              variant='outline'
              disabled={props.actionLoading}
              onClick={props.onWhitelist}
            >
              <ShieldCheck className='size-4' />
              加入白名单
            </Button>
          )}
        </div>
        {props.whitelists.length > 0 && (
          <div className='space-y-2'>
            <div className='text-sm font-medium'>当前白名单</div>
            {props.whitelists.map((item) => (
              <div
                key={item.id}
                className='flex items-center justify-between gap-2 rounded-lg border p-2 text-sm'
              >
                <div className='min-w-0'>
                  <div className='font-medium'>
                    {targetTypeLabel(item.target_type)} · {item.target_id}
                  </div>
                  <div className='text-muted-foreground truncate text-xs'>
                    {item.reason || '-'} · {item.operator_name || '-'}
                  </div>
                </div>
                <Button
                  size='sm'
                  variant='outline'
                  disabled={props.actionLoading}
                  onClick={() => props.onDeleteWhitelist(item.id)}
                >
                  移除
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function DetailGrid(props: { children: ReactNode }) {
  return <div className='grid gap-4 xl:grid-cols-2'>{props.children}</div>
}

function DetailBlock(props: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardContent className='space-y-3 p-4'>
        <div className='font-medium'>{props.title}</div>
        {props.children}
      </CardContent>
    </Card>
  )
}

function RiskUserCards(props: {
  users: RiskUser[]
  onOpenUser: (user: RiskUser) => void
}) {
  return (
    <div className='space-y-2'>
      {props.users.map((user) => (
        <button
          key={user.user_id}
          type='button'
          className='hover:bg-muted w-full rounded-lg border p-3 text-left'
          onClick={() => props.onOpenUser(user)}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>{user.username || `#${user.user_id}`}</div>
            <StatusBadge
              label={userStatusLabel(user.status)}
              variant={user.status === USER_STATUS_DISABLED ? 'danger' : 'success'}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            ID {user.user_id} · 充值 {formatMoney(user.topup_paid_amount, 'CNY')} · 错误{' '}
            {formatNumber(user.error_count)}
          </div>
        </button>
      ))}
    </div>
  )
}

function RiskIPCards(props: { ips: RiskIP[]; onOpenIP: (ip: string) => void }) {
  return (
    <div className='space-y-2'>
      {props.ips.map((ip) => (
        <button
          key={ip.ip}
          type='button'
          className='hover:bg-muted w-full rounded-lg border p-3 text-left'
          onClick={() => props.onOpenIP(ip.ip)}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>{ip.ip || '-'}</div>
            <StatusBadge
              label={ip.whitelisted ? '白名单' : `${formatNumber(ip.user_count)} 用户`}
              variant={ip.whitelisted ? 'success' : 'warning'}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            Token {formatNumber(ip.token_count)} · 请求 {formatNumber(ip.request_count)} · 错误{' '}
            {formatNumber(ip.error_count)}
          </div>
        </button>
      ))}
    </div>
  )
}

function RiskTokenCards(props: {
  tokens: RiskToken[]
  onOpenToken: (tokenId: number, tokenName?: string) => void
}) {
  return (
    <div className='space-y-2'>
      {props.tokens.map((token) => (
        <button
          key={token.token_id}
          type='button'
          className='hover:bg-muted w-full rounded-lg border p-3 text-left'
          onClick={() => props.onOpenToken(token.token_id, token.token_name)}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>{token.token_name || `Token #${token.token_id}`}</div>
            <StatusBadge
              label={token.status === 1 ? '启用' : '禁用'}
              variant={token.status === 1 ? 'success' : 'danger'}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {token.username || `#${token.user_id}`} · IP {formatNumber(token.unique_ip_count)} · 错误{' '}
            {formatNumber(token.error_count)}
          </div>
        </button>
      ))}
    </div>
  )
}

function RiskOrderCards(props: {
  orders: RiskOrder[]
  onOpenOrder: (tradeNo: string) => void
}) {
  return (
    <div className='space-y-2'>
      {props.orders.map((order) => (
        <button
          key={`${order.order_type}-${order.trade_no}`}
          type='button'
          className='hover:bg-muted w-full rounded-lg border p-3 text-left'
          onClick={() => props.onOpenOrder(order.trade_no)}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>{order.trade_no}</div>
            <StatusBadge
              label={orderStatusLabel(order.status)}
              variant={orderStatusVariant(order.status)}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {orderTypeLabel(order.order_type)} · {order.payment_provider || '-'} ·{' '}
            {paymentMethodLabel(order.payment_method)} ·{' '}
            {formatMoney(order.paid_amount, order.paid_currency)}
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            佣金：{referralStatusLabel(order.referral_commission_status, order.referral_commission_error)}
          </div>
        </button>
      ))}
    </div>
  )
}

function RiskReferralCards(props: {
  referrals: RiskReferral[]
  onOpenReferral: (row: RiskReferral) => void
}) {
  return (
    <div className='space-y-2'>
      {props.referrals.map((row) => (
        <button
          key={`${row.affiliate_id}-${row.inviter_user_id}`}
          type='button'
          className='hover:bg-muted w-full rounded-lg border p-3 text-left'
          onClick={() => props.onOpenReferral(row)}
        >
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>
              {row.inviter_username || `#${row.inviter_user_id}`}
            </div>
            <StatusBadge
              label={severityLabel(row.severity)}
              variant={severityVariant(row.severity)}
              copyable={false}
            />
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            邀请 {formatNumber(row.invitee_count)} · 佣金{' '}
            {formatMoney(row.commission_amount, 'CNY')} · 提现{' '}
            {formatMoney(row.withdrawal_amount, 'CNY')}
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>{row.reason || '-'}</div>
        </button>
      ))}
    </div>
  )
}

function RiskLogCards(props: { logs: RiskLog[] }) {
  return (
    <div className='space-y-2'>
      {props.logs.slice(0, 12).map((log) => (
        <div key={log.id} className='rounded-lg border p-3'>
          <div className='flex items-center justify-between gap-2'>
            <div className='font-medium'>{logTypeLabel(log.type)}</div>
            <div className='text-muted-foreground text-xs'>{formatTime(log.created_at)}</div>
          </div>
          <div className='mt-1 line-clamp-2 text-sm'>{log.content || '-'}</div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {log.username || `#${log.user_id}`} · IP {log.ip || '-'} · Token{' '}
            {log.token_name || log.token_id || '-'} · 模型 {log.model_name || '-'}
          </div>
        </div>
      ))}
    </div>
  )
}

function InfoItem(props: { label: string; value: ReactNode }) {
  return (
    <div className='bg-muted/50 rounded-lg p-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 truncate text-sm font-medium'>{props.value}</div>
    </div>
  )
}

function EmptyPanel(props: { text: string }) {
  return (
    <Card>
      <CardContent className='p-6 text-center text-sm text-muted-foreground'>
        {props.text}
      </CardContent>
    </Card>
  )
}

function EmptyText(props: { text: string }) {
  return <div className='text-sm text-muted-foreground'>{props.text}</div>
}

function getPrimaryWhitelistTarget(
  kind: DetailKind | undefined,
  detail: RiskDetail | null
): WhitelistTarget | null {
  if (!detail) return null
  if (detail.event?.target_type && detail.event.target_id) {
    return {
      target_type: detail.event.target_type,
      target_id: detail.event.target_id,
      label: detail.event.target_id,
    }
  }
  if (kind === 'user' && detail.users[0]) {
    return {
      target_type: 'user',
      target_id: String(detail.users[0].user_id),
      label: detail.users[0].username,
    }
  }
  if (kind === 'ip' && detail.ip) {
    return { target_type: 'ip', target_id: detail.ip, label: detail.ip }
  }
  if (kind === 'token' && detail.token_id) {
    return {
      target_type: 'token',
      target_id: String(detail.token_id),
      label: String(detail.token_id),
    }
  }
  if (kind === 'order' && detail.trade_no) {
    return {
      target_type: 'order',
      target_id: detail.trade_no,
      label: detail.trade_no,
    }
  }
  if (kind === 'referral' && detail.users[0]) {
    return {
      target_type: 'referral',
      target_id: String(detail.users[0].user_id),
      label: detail.users[0].username,
    }
  }
  return null
}
