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
import { useTranslation } from 'react-i18next'
import {
  BadgeDollarSign,
  FileText,
  RefreshCw,
  ShieldAlert,
  Users,
} from 'lucide-react'
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
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { cn } from '@/lib/utils'
import {
  getRiskDetail,
  getRiskOverview,
  getRiskUsers,
  type RiskDetail,
  type RiskLog,
  type RiskOrder,
  type RiskSignal,
  type RiskUser,
} from './api'

type DetailRequest = {
  title: string
  description: string
  params: URLSearchParams
}

type RuleCardConfig = {
  type: string
  title: string
  description: string
}

const RISK_RULES: RuleCardConfig[] = [
  {
    type: 'shared_ip',
    title: '同 IP 多账号',
    description: '同一 IP 在窗口内关联 5 个及以上账号，用于发现批量注册或共享账号风险。',
  },
  {
    type: 'high_error_count',
    title: '错误日志过多',
    description: '单用户在窗口内错误日志达到 20 次，用于发现撞库、异常调用或上游失败集中爆发。',
  },
  {
    type: 'high_topup_activity',
    title: '充值异常',
    description: '窗口内成功充值 5 笔以上或实付金额达到 1000，用于提示人工复核充值行为。',
  },
  {
    type: 'new_user_high_consume',
    title: '新号高消耗',
    description: '新注册账号在窗口内 API 消耗达到 1000000 额度，用于发现新号异常消耗风险。',
  },
]

function formatTime(timestamp?: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

function formatMoney(amount?: number, currency?: string) {
  const normalizedCurrency = (currency || 'CNY').toUpperCase()
  const value = Number(amount || 0)
  if (normalizedCurrency === 'CNY') return `¥${value.toFixed(2)}`
  if (normalizedCurrency === 'USD') return `$${value.toFixed(2)}`
  if (normalizedCurrency === 'USDT') return `${value.toFixed(6)} USDT`
  return `${value.toFixed(2)} ${normalizedCurrency}`
}

function severityVariant(severity: string): StatusVariant {
  switch (severity) {
    case 'high':
      return 'danger'
    case 'warning':
      return 'warning'
    case 'info':
      return 'info'
    default:
      return 'neutral'
  }
}

function severityLabel(severity: string) {
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

function riskTypeLabel(type: string) {
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
      return '消耗异常'
    case 'new_users':
      return '新注册用户'
    case 'disabled_users':
      return '禁用用户'
    case 'user':
      return '用户风险详情'
    default:
      return type || '风险详情'
  }
}

function logTypeLabel(type: number) {
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

function orderStatusLabel(status: string) {
  switch (status) {
    case 'success':
    case 'SUCCESS':
      return '成功'
    case 'pending':
    case 'PENDING':
      return '待支付'
    case 'failed':
    case 'FAILED':
      return '失败'
    case 'expired':
    case 'EXPIRED':
      return '已过期'
    default:
      return status || '-'
  }
}

function referralErrorLabel(error: string) {
  switch (error) {
    case 'fx_rate_missing':
      return '佣金结算汇率缺失'
    case 'missing_referral_snapshot':
      return '订单缺少推广快照'
    case 'zero_commission_amount':
      return '佣金金额为 0'
    case 'affiliate_not_eligible':
      return '推广员当前不可结算'
    case 'no_referral_binding':
      return '订单没有有效邀请绑定'
    case 'commission_disabled':
      return '返佣功能未启用'
    default:
      return error || '-'
  }
}

function commissionStatusLabel(status: string, error: string) {
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

function orderIssueVariant(order: RiskOrder): StatusVariant {
  if (order.status === 'success') return 'success'
  if (order.status === 'failed' || order.status === 'expired') return 'danger'
  return 'warning'
}

function buildDetailParams(
  type: string,
  windowHours: string,
  extras: Record<string, string | number | undefined> = {}
) {
  const params = new URLSearchParams({
    type,
    window_hours: windowHours || '24',
  })
  Object.entries(extras).forEach(([key, value]) => {
    if (value !== undefined && value !== '') params.set(key, String(value))
  })
  return params
}

function signalKey(signal: RiskSignal) {
  return `${signal.type}-${signal.ip || signal.user_id || signal.last_seen_at || signal.message}`
}

function openAdminPath(path: string) {
  window.location.assign(path)
}

function rechargeAuditPath(keyword: string | number) {
  return `/recharge-audit?keyword=${encodeURIComponent(String(keyword))}`
}

function usageLogsPath(username: string) {
  return `/usage-logs/common?username=${encodeURIComponent(username)}`
}

function usersPath(keyword: string | number) {
  return `/users?keyword=${encodeURIComponent(String(keyword))}`
}

export function RiskCenter() {
  const { t } = useTranslation()
  const [signals, setSignals] = useState<RiskSignal[]>([])
  const [users, setUsers] = useState<RiskUser[]>([])
  const [signalCount, setSignalCount] = useState(0)
  const [disabledUsers, setDisabledUsers] = useState(0)
  const [newUsers, setNewUsers] = useState(0)
  const [keyword, setKeyword] = useState('')
  const [windowHours, setWindowHours] = useState('24')
  const [loading, setLoading] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailTitle, setDetailTitle] = useState('')
  const [detailDescription, setDetailDescription] = useState('')
  const [detail, setDetail] = useState<RiskDetail | null>(null)
  const [lastDetailRequest, setLastDetailRequest] = useState<DetailRequest | null>(null)

  const params = useMemo(() => {
    const next = new URLSearchParams({
      p: '1',
      page_size: '20',
      window_hours: windowHours || '24',
    })
    if (keyword.trim()) next.set('keyword', keyword.trim())
    return next
  }, [keyword, windowHours])

  const load = async () => {
    setLoading(true)
    try {
      const [overviewRes, userRes] = await Promise.all([
        getRiskOverview(params),
        getRiskUsers(params),
      ])
      if (overviewRes.success) {
        setSignals(overviewRes.data?.signals || [])
        setSignalCount(overviewRes.data?.signal_count || 0)
        setDisabledUsers(overviewRes.data?.disabled_users || 0)
        setNewUsers(overviewRes.data?.new_user_count || 0)
      }
      if (userRes.success) setUsers(userRes.data?.items || [])
    } finally {
      setLoading(false)
    }
  }

  async function openDetail(request: DetailRequest) {
    setLastDetailRequest(request)
    setDetailTitle(request.title)
    setDetailDescription(request.description)
    setDetailOpen(true)
    setDetailLoading(true)
    setDetail(null)
    try {
      const res = await getRiskDetail(request.params)
      if (res.success) setDetail(res.data)
    } finally {
      setDetailLoading(false)
    }
  }

  function openSignal(signal: RiskSignal) {
    void openDetail({
      title: signal.message || riskTypeLabel(signal.type),
      description: `${riskTypeLabel(signal.type)} · ${signal.username || signal.ip || signal.user_id || '-'} · ${signal.count} 次`,
      params: buildDetailParams(signal.type, windowHours, {
        ip: signal.ip,
        user_id: signal.user_id,
      }),
    })
  }

  function openRule(rule: RuleCardConfig) {
    const matched = signals.find((signal) => signal.type === rule.type)
    if (matched) {
      openSignal(matched)
      return
    }
    void openDetail({
      title: rule.title,
      description: rule.description,
      params: buildDetailParams(rule.type, windowHours),
    })
  }

  function openSummary(type: string, title: string, description: string) {
    void openDetail({
      title,
      description,
      params: buildDetailParams(type, windowHours),
    })
  }

  function openUser(user: RiskUser, type = 'user', title = '用户风险详情') {
    void openDetail({
      title: `${title}：${user.username}`,
      description: `用户 ID ${user.user_id} · 最近 ${windowHours || 24} 小时`,
      params: buildDetailParams(type, windowHours, { user_id: user.user_id }),
    })
  }

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        风控中心用于集中发现需要人工复核的账号、充值和调用异常；点击风险信号、指标或用户字段，会在弹窗中展示关联账号、订单和最近日志。
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid gap-3 md:grid-cols-4'>
            <SummaryCard
              label='风险信号'
              value={String(signalCount)}
              icon={<ShieldAlert className='size-4' />}
              onClick={() => {
                if (signals[0]) {
                  openSignal(signals[0])
                  return
                }
                openSummary('shared_ip', '风险信号', '当前窗口内暂无可展开的风险信号')
              }}
            />
            <SummaryCard
              label='新注册用户'
              value={String(newUsers)}
              icon={<Users className='size-4' />}
              onClick={() =>
                openSummary(
                  'new_users',
                  '新注册用户',
                  `最近 ${windowHours || 24} 小时注册的用户`
                )
              }
            />
            <SummaryCard
              label={t('Disabled Users')}
              value={String(disabledUsers)}
              icon={<Users className='size-4' />}
              onClick={() =>
                openSummary('disabled_users', '禁用用户', '当前被禁用的用户账号')
              }
            />
            <SummaryCard
              label='时间窗口'
              value={`${windowHours || 24}h`}
            />
          </div>

          <Card>
            <CardContent className='space-y-4 p-4'>
              <div className='grid gap-3 lg:grid-cols-4'>
                {RISK_RULES.map((rule) => (
                  <RuleCard
                    key={rule.type}
                    title={rule.title}
                    description={rule.description}
                    active={signals.some((signal) => signal.type === rule.type)}
                    onClick={() => openRule(rule)}
                  />
                ))}
              </div>

              <div className='grid gap-2 md:grid-cols-[minmax(0,1fr)_140px_auto]'>
                <Input
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder={t('Search user, email, or user ID')}
                />
                <Input
                  type='number'
                  min={1}
                  max={720}
                  value={windowHours}
                  onChange={(event) => setWindowHours(event.target.value)}
                  placeholder={t('Hours')}
                />
                <Button onClick={load} disabled={loading}>
                  <RefreshCw className={cn('size-4', loading && 'animate-spin')} />
                  {loading ? t('Loading...') : t('Refresh')}
                </Button>
              </div>

              {signals.length > 0 ? (
                <div className='grid gap-3 lg:grid-cols-2'>
                  {signals.slice(0, 8).map((signal) => (
                    <button
                      key={signalKey(signal)}
                      type='button'
                      className='rounded-md border p-3 text-left transition hover:border-primary hover:bg-muted/30'
                      onClick={() => openSignal(signal)}
                    >
                      <div className='flex items-center justify-between gap-2'>
                        <div className='font-medium'>{signal.message}</div>
                        <StatusBadge
                          label={severityLabel(signal.severity)}
                          variant={severityVariant(signal.severity)}
                          copyable={false}
                        />
                      </div>
                      <div className='text-muted-foreground mt-2 text-sm'>
                        {signal.username || signal.ip || signal.user_id || '-'} ·{' '}
                        {t('Count')}: {signal.count}
                        {signal.amount ? ` · ${signal.amount}` : ''}
                      </div>
                      <div className='text-muted-foreground mt-1 text-xs'>
                        {formatTime(signal.first_seen_at)} -{' '}
                        {formatTime(signal.last_seen_at)} · 点击查看详情
                      </div>
                    </button>
                  ))}
                </div>
              ) : (
                <div className='text-muted-foreground rounded-md border border-dashed p-4 text-sm'>
                  当前窗口内暂无风险信号。
                </div>
              )}

              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Risk')}</TableHead>
                      <TableHead>充值</TableHead>
                      <TableHead>错误数</TableHead>
                      <TableHead>消耗</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {users.map((user) => (
                      <TableRow key={user.user_id}>
                        <TableCell>
                          <button
                            type='button'
                            className='text-left hover:underline'
                            onClick={() => openUser(user)}
                          >
                            <div className='font-medium'>{user.username}</div>
                            <div className='text-muted-foreground text-xs'>
                              ID {user.user_id}
                            </div>
                          </button>
                        </TableCell>
                        <TableCell>
                          <button
                            type='button'
                            className='hover:underline'
                            onClick={() =>
                              openUser(
                                user,
                                user.status === 1 ? 'user' : 'disabled_users',
                                user.status === 1 ? '用户状态' : '禁用用户'
                              )
                            }
                          >
                            {user.status === 1 ? t('Enabled') : t('Disabled')}
                          </button>
                        </TableCell>
                        <TableCell>
                          <button
                            type='button'
                            onClick={() => openUser(user)}
                            className='inline-flex'
                          >
                            <StatusBadge
                              label={severityLabel(user.severity)}
                              variant={severityVariant(user.severity)}
                              copyable={false}
                            />
                          </button>
                        </TableCell>
                        <TableCell>
                          <MetricButton
                            primary={formatMoney(user.topup_paid_amount, 'CNY')}
                            secondary={`${user.topup_count} 笔订单`}
                            disabled={user.topup_count <= 0}
                            onClick={() =>
                              openUser(user, 'topups', '充值订单')
                            }
                          />
                        </TableCell>
                        <TableCell>
                          <MetricButton
                            primary={String(user.error_count)}
                            disabled={user.error_count <= 0}
                            onClick={() =>
                              openUser(user, 'errors', '错误日志')
                            }
                          />
                        </TableCell>
                        <TableCell>
                          <MetricButton
                            primary={String(user.consume_count)}
                            secondary={`${user.consume_quota} 额度`}
                            disabled={
                              user.consume_count <= 0 &&
                              user.consume_quota <= 0
                            }
                            onClick={() =>
                              openUser(user, 'consume', 'API 消耗记录')
                            }
                          />
                        </TableCell>
                        <TableCell>{formatTime(user.created_at)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>

          <RiskDetailDialog
            open={detailOpen}
            onOpenChange={(open) => {
              setDetailOpen(open)
              if (!open) setDetail(null)
            }}
            title={detailTitle}
            description={detailDescription}
            detail={detail}
            loading={detailLoading}
            onUserSelect={(user) => openUser(user)}
            onRefresh={() => {
              if (lastDetailRequest) void openDetail(lastDetailRequest)
            }}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function SummaryCard(props: {
  label: string
  value: string
  icon?: ReactNode
  onClick?: () => void
}) {
  const content = (
    <CardContent className='p-4'>
      <div className='text-muted-foreground flex items-center gap-2 text-sm'>
        {props.icon}
        {props.label}
      </div>
      <div className='mt-2 text-2xl font-semibold'>{props.value}</div>
    </CardContent>
  )
  if (!props.onClick) return <Card>{content}</Card>
  return (
    <button
      type='button'
      className='text-left'
      onClick={props.onClick}
      aria-label={`${props.label} ${props.value}`}
    >
      <Card className='h-full transition hover:border-primary hover:bg-muted/30'>
        {content}
      </Card>
    </button>
  )
}

function RuleCard(props: {
  title: string
  description: string
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type='button'
      className={cn(
        'rounded-md border p-3 text-left transition hover:border-primary hover:bg-muted/30',
        props.active && 'border-warning/50 bg-warning/5'
      )}
      onClick={props.onClick}
    >
      <div className='text-sm font-medium'>{props.title}</div>
      <div className='text-muted-foreground mt-1 text-xs leading-5'>
        {props.description}
      </div>
    </button>
  )
}

function MetricButton(props: {
  primary: string
  secondary?: string
  disabled?: boolean
  onClick: () => void
}) {
  if (props.disabled) {
    return (
      <div>
        <div>{props.primary}</div>
        {props.secondary ? (
          <div className='text-muted-foreground text-xs'>{props.secondary}</div>
        ) : null}
      </div>
    )
  }
  return (
    <button
      type='button'
      className='text-left hover:underline'
      onClick={props.onClick}
    >
      <div>{props.primary}</div>
      {props.secondary ? (
        <div className='text-muted-foreground text-xs'>{props.secondary}</div>
      ) : null}
    </button>
  )
}

function RiskDetailDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description: string
  detail: RiskDetail | null
  loading: boolean
  onUserSelect: (user: RiskUser) => void
  onRefresh: () => void
}) {
  if (!props.open) return null
  return (
    <div
      className='fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-4'
      role='dialog'
      aria-modal='true'
      aria-labelledby='risk-detail-title'
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) props.onOpenChange(false)
      }}
    >
      <div className='bg-background w-full max-w-5xl rounded-lg border shadow-lg'>
        <div className='flex items-start justify-between gap-4 border-b p-4'>
          <div>
            <h2 id='risk-detail-title' className='text-lg font-semibold'>
              {props.title || '风险详情'}
            </h2>
            <div className='text-muted-foreground mt-1 text-sm'>
              {props.description ||
                (props.detail
                  ? `${riskTypeLabel(props.detail.type)} · ${props.detail.window_hours}h`
                  : '加载风险详情')}
            </div>
          </div>
          <Button
            type='button'
            size='sm'
            variant='ghost'
            onClick={() => props.onOpenChange(false)}
          >
            关闭
          </Button>
        </div>
        <div className='max-h-[68vh] overflow-y-auto p-4'>
          {props.loading ? (
            <div className='text-muted-foreground py-8 text-center text-sm'>
              加载中...
            </div>
          ) : props.detail ? (
            <RiskDetailContent
              detail={props.detail}
              onUserSelect={props.onUserSelect}
            />
          ) : (
            <EmptyDetail />
          )}
        </div>
        <div className='flex justify-end gap-2 border-t p-4'>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
            disabled={props.loading}
          >
            关闭
          </Button>
          <Button
            type='button'
            onClick={props.onRefresh}
            disabled={props.loading}
          >
            刷新详情
          </Button>
        </div>
      </div>
    </div>
  )
}

function RiskDetailContent(props: {
  detail: RiskDetail
  onUserSelect: (user: RiskUser) => void
}) {
  return (
    <div className='space-y-4'>
      <DetailActionBar detail={props.detail} />
      <div className='grid gap-3 md:grid-cols-3'>
        <DialogMetric title='关联用户' value={props.detail.users.length} />
        <DialogMetric title='订单管理记录' value={props.detail.orders.length} />
        <DialogMetric title='调用日志记录' value={props.detail.logs.length} />
      </div>
      <div className='grid gap-4 xl:grid-cols-3'>
        <DetailBlock title='关联用户'>
          {props.detail.users.length === 0 ? (
            <EmptyDetail />
          ) : (
            <div className='space-y-2'>
              {props.detail.users.slice(0, 20).map((user) => (
                <button
                  key={user.user_id}
                  type='button'
                  className='flex w-full items-center justify-between gap-3 rounded-md border bg-background px-3 py-2 text-left text-sm hover:border-primary'
                  onClick={() => props.onUserSelect(user)}
                >
                  <span>
                    <span className='font-medium'>{user.username}</span>
                    <span className='text-muted-foreground ml-2'>
                      ID {user.user_id}
                    </span>
                  </span>
                  <StatusBadge
                    label={severityLabel(user.severity)}
                    variant={severityVariant(user.severity)}
                    copyable={false}
                  />
                </button>
              ))}
            </div>
          )}
        </DetailBlock>

        <DetailBlock title='订单管理记录'>
          {props.detail.orders.length === 0 ? (
            <EmptyDetail />
          ) : (
            <div className='space-y-2'>
              {props.detail.orders.slice(0, 15).map((order) => (
                <RiskOrderItem
                  key={`${order.order_type}-${order.trade_no}`}
                  order={order}
                />
              ))}
            </div>
          )}
        </DetailBlock>

        <DetailBlock title='调用日志记录'>
          {props.detail.logs.length === 0 ? (
            <EmptyDetail />
          ) : (
            <div className='space-y-2'>
              {props.detail.logs.slice(0, 15).map((log) => (
                <RiskLogItem key={log.id} log={log} />
              ))}
            </div>
          )}
        </DetailBlock>
      </div>
    </div>
  )
}

function DialogMetric(props: { title: string; value: number }) {
  return (
    <div className='rounded-md border bg-muted/20 p-3'>
      <div className='text-muted-foreground text-xs'>{props.title}</div>
      <div className='mt-1 text-lg font-semibold'>{props.value}</div>
    </div>
  )
}

function DetailActionBar(props: { detail: RiskDetail }) {
  const primaryUser = props.detail.users[0]
  const firstOrder = props.detail.orders[0]
  const firstLog = props.detail.logs.find((log) => log.username)
  const username = primaryUser?.username || firstOrder?.username || firstLog?.username
  const userID = primaryUser?.user_id || firstOrder?.user_id || firstLog?.user_id

  if (!username && !userID && !firstOrder?.trade_no) {
    return null
  }

  return (
    <div className='rounded-md border bg-muted/20 p-3'>
      <div className='text-sm font-medium'>后台联动入口</div>
      <div className='text-muted-foreground mt-1 text-xs'>
        弹窗内先展示关联数据；需要追溯完整列表时，可跳转到对应管理员模块并自动带上当前用户或订单关键字。
      </div>
      <div className='mt-3 flex flex-wrap gap-2'>
        {firstOrder?.trade_no || username || userID ? (
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() =>
              openAdminPath(rechargeAuditPath(firstOrder?.trade_no || username || userID || ''))
            }
          >
            查看订单管理
          </Button>
        ) : null}
        {username ? (
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() => openAdminPath(usageLogsPath(username))}
          >
            查看调用日志
          </Button>
        ) : null}
        {username || userID ? (
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() => openAdminPath(usersPath(username || userID || ''))}
          >
            查看用户列表
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function DetailBlock(props: { title: string; children: ReactNode }) {
  return (
    <div>
      <div className='mb-2 text-sm font-medium'>{props.title}</div>
      {props.children}
    </div>
  )
}

function EmptyDetail() {
  return <div className='text-muted-foreground text-sm'>暂无数据</div>
}

function RiskOrderItem(props: { order: RiskOrder }) {
  const path = rechargeAuditPath(props.order.trade_no)
  return (
    <div className='rounded-md border bg-background p-3 text-sm'>
      <div className='flex items-center justify-between gap-2'>
        <button
          type='button'
          className='font-medium hover:underline'
          onClick={() => openAdminPath(path)}
        >
          {props.order.trade_no}
        </button>
        <StatusBadge
          label={orderStatusLabel(props.order.status)}
          variant={orderIssueVariant(props.order)}
          copyable={false}
        />
      </div>
      <div className='text-muted-foreground mt-1 text-xs'>
        {props.order.username || `#${props.order.user_id}`} ·{' '}
        {props.order.order_type === 'subscription' ? '订阅' : '充值'} ·{' '}
        {props.order.payment_provider || '-'} ·{' '}
        {props.order.payment_method || '-'} ·{' '}
        {formatMoney(props.order.paid_amount, props.order.paid_currency)}
      </div>
      <div className='text-muted-foreground mt-1 text-xs'>
        佣金：
        {commissionStatusLabel(
          props.order.referral_commission_status,
          props.order.referral_commission_error
        )}
      </div>
      <div className='text-muted-foreground mt-1 flex items-center gap-1 text-xs'>
        <BadgeDollarSign className='size-3' />
        {formatTime(props.order.created_at)} · 来自订单管理
      </div>
    </div>
  )
}

function RiskLogItem(props: { log: RiskLog }) {
  const path = usageLogsPath(props.log.username || '')
  return (
    <div className='rounded-md border bg-background p-3 text-sm'>
      <div className='flex items-center justify-between gap-2'>
        <button
          type='button'
          className='font-medium hover:underline'
          onClick={() => openAdminPath(path)}
        >
          {logTypeLabel(props.log.type)}
        </button>
        <span className='text-muted-foreground text-xs'>
          {formatTime(props.log.created_at)}
        </span>
      </div>
      <div className='mt-1 line-clamp-2'>{props.log.content || '-'}</div>
      <div className='text-muted-foreground mt-1 flex items-center gap-1 text-xs'>
        <FileText className='size-3' />
        {props.log.username || `#${props.log.user_id}`} · IP{' '}
        {props.log.ip || '-'} · quota {props.log.quota} · 来自调用日志
      </div>
    </div>
  )
}
