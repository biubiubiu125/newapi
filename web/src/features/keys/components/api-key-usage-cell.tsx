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
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import type { ApiKey, ApiKeyUsageStats } from '../types'

type ApiKeyUsageCellProps = {
  apiKey: ApiKey
  usage?: ApiKeyUsageStats
  isLoading?: boolean
  isError?: boolean
}

export function ApiKeyUsageCell({
  apiKey,
  usage,
  isLoading,
  isError,
}: ApiKeyUsageCellProps) {
  if (isLoading) {
    return <span className='text-muted-foreground text-xs'>加载中</span>
  }
  if (isError) {
    return <span className='text-destructive text-xs'>获取失败</span>
  }
  if (!usage) {
    return <span className='text-muted-foreground text-xs'>暂无数据</span>
  }

  return (
    <div className='min-w-[180px] space-y-0.5 text-xs leading-5'>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>今日</span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.today_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>本月</span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.month_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>
          {usage.reset_at
            ? `自 ${formatTimestampToDate(usage.reset_at).slice(0, 10)} 起累计`
            : '累计'}
        </span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.cumulative_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>最后使用</span>
        <span className='font-mono tabular-nums'>
          {usage.last_used_at ? formatTimestampToDate(usage.last_used_at) : '-'}
        </span>
      </div>
      {!apiKey.unlimited_quota ? (
        <div className='flex justify-between gap-3'>
          <span className='text-muted-foreground'>剩余额度</span>
          <span className='font-mono tabular-nums'>
            {formatQuota(apiKey.remain_quota)}
          </span>
        </div>
      ) : null}
    </div>
  )
}
