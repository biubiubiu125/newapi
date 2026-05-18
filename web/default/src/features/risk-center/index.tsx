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
import {
  getRiskOverview,
  getRiskUsers,
  type RiskSignal,
  type RiskUser,
} from './api'

function formatTime(timestamp?: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

function severityVariant(severity: string) {
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

export function RiskCenter() {
  const { t } = useTranslation()
  const [signals, setSignals] = useState<RiskSignal[]>([])
  const [users, setUsers] = useState<RiskUser[]>([])
  const [signalCount, setSignalCount] = useState(0)
  const [disabledUsers, setDisabledUsers] = useState(0)
  const [keyword, setKeyword] = useState('')
  const [windowHours, setWindowHours] = useState('24')
  const [loading, setLoading] = useState(false)

  const params = useMemo(() => {
    const p = new URLSearchParams({
      p: '1',
      page_size: '20',
      window_hours: windowHours || '24',
    })
    if (keyword.trim()) p.set('keyword', keyword.trim())
    return p
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
      }
      if (userRes.success) {
        setUsers(userRes.data?.items || [])
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Risk Center')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('Review account, recharge, and usage risk signals')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='space-y-4'>
          <div className='grid gap-3 md:grid-cols-3'>
            <SummaryCard
              label={t('Risk Signals')}
              value={String(signalCount)}
            />
            <SummaryCard
              label={t('Disabled Users')}
              value={String(disabledUsers)}
            />
            <SummaryCard label={t('Window')} value={`${windowHours || 24}h`} />
          </div>

          <Card>
            <CardContent className='space-y-4 p-4'>
              <div className='grid gap-2 md:grid-cols-[minmax(0,1fr)_140px_auto]'>
                <Input
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  placeholder={t('Search user, email, or user ID')}
                />
                <Input
                  type='number'
                  min={1}
                  max={720}
                  value={windowHours}
                  onChange={(e) => setWindowHours(e.target.value)}
                  placeholder={t('Hours')}
                />
                <Button onClick={load} disabled={loading}>
                  {loading ? t('Loading...') : t('Refresh')}
                </Button>
              </div>

              <div className='grid gap-3 lg:grid-cols-2'>
                {signals.slice(0, 8).map((signal) => (
                  <div
                    key={`${signal.type}-${signal.ip || signal.user_id || signal.last_seen_at}`}
                    className='rounded-md border p-3'
                  >
                    <div className='flex items-center justify-between gap-2'>
                      <div className='font-medium'>{signal.message}</div>
                      <StatusBadge
                        label={t(signal.severity)}
                        variant={severityVariant(signal.severity)}
                        copyable={false}
                      />
                    </div>
                    <div className='text-muted-foreground mt-2 text-sm'>
                      {signal.username || signal.ip || signal.user_id || '-'} ·{' '}
                      {t('Count')}: {signal.count}
                    </div>
                    <div className='text-muted-foreground mt-1 text-xs'>
                      {formatTime(signal.first_seen_at)} -{' '}
                      {formatTime(signal.last_seen_at)}
                    </div>
                  </div>
                ))}
              </div>

              <div className='overflow-x-auto rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('User')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Risk')}</TableHead>
                      <TableHead>{t('Recharge')}</TableHead>
                      <TableHead>{t('Errors')}</TableHead>
                      <TableHead>{t('Consumption')}</TableHead>
                      <TableHead>{t('Created At')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {users.map((user) => (
                      <TableRow key={user.user_id}>
                        <TableCell>
                          <div className='font-medium'>{user.username}</div>
                          <div className='text-muted-foreground text-xs'>
                            ID {user.user_id}
                          </div>
                        </TableCell>
                        <TableCell>
                          {user.status === 1 ? t('Enabled') : t('Disabled')}
                        </TableCell>
                        <TableCell>
                          <StatusBadge
                            label={t(user.severity)}
                            variant={severityVariant(user.severity)}
                            copyable={false}
                          />
                        </TableCell>
                        <TableCell>
                          ¥{Number(user.topup_paid_amount || 0).toFixed(2)}
                          <div className='text-muted-foreground text-xs'>
                            {user.topup_count} {t('orders')}
                          </div>
                        </TableCell>
                        <TableCell>{user.error_count}</TableCell>
                        <TableCell>
                          {user.consume_count}
                          <div className='text-muted-foreground text-xs'>
                            {user.consume_quota}
                          </div>
                        </TableCell>
                        <TableCell>{formatTime(user.created_at)}</TableCell>
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
