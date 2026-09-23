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

import { cn } from '@/lib/utils'

const HOURS = Array.from({ length: 24 }, (_, hour) =>
  String(hour).padStart(2, '0')
)
const MINUTES = Array.from({ length: 60 }, (_, minute) =>
  String(minute).padStart(2, '0')
)

interface TimeInputProps {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  className?: string
  'aria-label'?: string
}

function parseTime(value: string): { hours: string; minutes: string } {
  const match = /^(\d{1,2}):(\d{2})/.exec(value.trim())
  if (!match) {
    return { hours: '00', minutes: '00' }
  }
  const hours = Math.min(23, Math.max(0, Number(match[1])))
  const minutes = Math.min(59, Math.max(0, Number(match[2])))
  return {
    hours: String(hours).padStart(2, '0'),
    minutes: String(minutes).padStart(2, '0'),
  }
}

const selectClassName =
  'border-input bg-background h-8 rounded-lg border px-1.5 text-sm tabular-nums outline-none focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-50'

export function TimeInput({
  value,
  onChange,
  disabled,
  className,
  'aria-label': ariaLabel,
}: TimeInputProps) {
  const { t } = useTranslation()
  const { hours, minutes } = parseTime(value)

  const emit = (nextHours: string, nextMinutes: string) => {
    onChange(`${nextHours}:${nextMinutes}`)
  }

  return (
    <div
      data-slot='time-input'
      role='group'
      aria-label={ariaLabel}
      className={cn('flex items-center gap-1', className)}
    >
      <select
        aria-label={t('Hour')}
        className={selectClassName}
        disabled={disabled}
        value={hours}
        onChange={(event) => emit(event.target.value, minutes)}
      >
        {HOURS.map((hour) => (
          <option key={hour} value={hour}>
            {hour}
          </option>
        ))}
      </select>
      <span className='text-muted-foreground text-sm'>:</span>
      <select
        aria-label={t('Minute')}
        className={selectClassName}
        disabled={disabled}
        value={minutes}
        onChange={(event) => emit(hours, event.target.value)}
      >
        {MINUTES.map((minute) => (
          <option key={minute} value={minute}>
            {minute}
          </option>
        ))}
      </select>
    </div>
  )
}
