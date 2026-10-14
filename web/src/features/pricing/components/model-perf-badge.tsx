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
import { memo, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import type { SuccessRatePoint } from '@/features/performance-metrics/types'
import {
  formatLatency,
  formatThroughput,
  getSuccessRateDotClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = {
  window_start?: number
  window_end?: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_series?: SuccessRatePoint[]
}

export interface ModelPerfBadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  perf: ModelPerfBadgeData | undefined
}

export const ModelPerfBadge = memo(function ModelPerfBadge(
  props: ModelPerfBadgeProps
) {
  const { t } = useTranslation()
  const perf = props.perf
  const successRate = perf?.success_rate
  const hasSuccessRate =
    successRate != null &&
    Number.isFinite(successRate) &&
    successRate >= 0 &&
    successRate <= 100
  const latencyText = formatLatency(perf?.avg_latency_ms ?? Number.NaN)
  const rawThroughput = formatThroughput(perf?.avg_tps ?? Number.NaN)
  const throughputText =
    rawThroughput === '—' ? '—t/s' : rawThroughput.replace(' ', '')
  const samples = useMemo(() => {
    return (perf?.recent_success_series ?? [])
      .filter(
        (point) =>
          Number.isFinite(point.success_rate) &&
          point.success_rate >= 0 &&
          point.success_rate <= 100
      )
      .slice(-24)
  }, [perf?.recent_success_series])

  return (
    <div
      aria-label={t('Performance metrics for the last 24 hours')}
      className={cn(
        'flex w-full min-w-0 items-center justify-between gap-3',
        props.className
      )}
    >
      <dl className='flex min-w-0 items-start gap-5 text-xs tabular-nums'>
        <div className='w-24 shrink-0'>
          <dt
            title={t(
              'Success rate excludes business rejections and includes the current partial hour.'
            )}
            className='text-muted-foreground flex items-center justify-between gap-1 text-[11px] leading-4'
          >
            <span>{t('Status')}</span>
            <span className='font-mono'>
              {hasSuccessRate ? `${successRate.toFixed(2)}%` : '—'}
            </span>
          </dt>
          <dd
            role='img'
            aria-label={t(
              samples.length > 0
                ? 'Recent success-rate samples'
                : 'No performance data available'
            )}
            title={t(
              samples.length > 0
                ? 'Recent success-rate samples'
                : 'No performance data available'
            )}
            className='mt-1 flex h-3 w-24 items-center gap-px'
          >
            {samples.map((point, index) => (
              <span
                key={`${point.ts}-${index}`}
                aria-hidden
                className={cn(
                  'h-full w-[3px] shrink-0 rounded-xs',
                  getSuccessRateDotClass(point.success_rate)
                )}
              />
            ))}
          </dd>
        </div>
        <div title={t('Average latency')} className='shrink-0'>
          <dt className='text-muted-foreground text-[11px] leading-4'>
            {t('Latency short')}
          </dt>
          <dd className='mt-1 font-mono whitespace-nowrap'>
            {latencyText === '—' ? '—s' : latencyText}
          </dd>
        </div>
        <div title={t('Throughput')} className='shrink-0'>
          <dt className='text-muted-foreground text-[11px] leading-4'>
            {t('Throughput short')}
          </dt>
          <dd className='mt-1 font-mono whitespace-nowrap'>{throughputText}</dd>
        </div>
      </dl>
      {props.children}
    </div>
  )
})
