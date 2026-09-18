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
import { useTranslation } from 'react-i18next'

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
  const { t } = useTranslation()
  if (isLoading) {
    return <span className='text-muted-foreground text-xs'>{t('Loading')}</span>
  }
  if (isError) {
    return (
      <span className='text-destructive text-xs'>{t('Failed to load')}</span>
    )
  }
  if (!usage) {
    return <span className='text-muted-foreground text-xs'>{t('No data')}</span>
  }

  return (
    <div className='min-w-[180px] space-y-0.5 text-xs leading-5'>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>{t('Today')}</span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.today_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>{t('This Month')}</span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.month_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>
          {usage.reset_at
            ? t('Cumulative since {{date}}', {
                date: formatTimestampToDate(usage.reset_at).slice(0, 10),
              })
            : t('Cumulative')}
        </span>
        <span className='font-mono tabular-nums'>
          {formatQuota(usage.cumulative_quota)}
        </span>
      </div>
      <div className='flex justify-between gap-3'>
        <span className='text-muted-foreground'>{t('Last used')}</span>
        <span className='font-mono tabular-nums'>
          {usage.last_used_at ? formatTimestampToDate(usage.last_used_at) : '-'}
        </span>
      </div>
      {!apiKey.unlimited_quota ? (
        <div className='flex justify-between gap-3'>
          <span className='text-muted-foreground'>{t('Remaining quota')}</span>
          <span className='font-mono tabular-nums'>
            {formatQuota(apiKey.remain_quota)}
          </span>
        </div>
      ) : null}
    </div>
  )
}
