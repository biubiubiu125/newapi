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
along with this program. If you did not receive a copy of the GNU Affero
General Public License along with this program, see
<https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { CalendarDays } from 'lucide-react'
import { useMemo, useState } from 'react'
import type { DateRange } from 'react-day-picker'
import { useTranslation } from 'react-i18next'

import { TimeInput } from '@/components/time-input'
import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { resolveDayPickerLocale } from '@/lib/calendar-locale'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

interface CompactDateTimeRangePickerProps {
  start?: Date
  end?: Date
  onChange: (range: { start?: Date; end?: Date }) => void
  className?: string
}

function timeFromDate(date?: Date, fallback = '00:00'): string {
  if (!date) return fallback
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function withTime(date: Date | undefined, time: string): Date | undefined {
  if (!date) return undefined
  const [hours, minutes] = time.split(':').map(Number)
  const next = new Date(date)
  next.setHours(hours || 0, minutes || 0, 0, 0)
  return next
}

export function CompactDateTimeRangePicker({
  start,
  end,
  onChange,
  className,
}: CompactDateTimeRangePickerProps) {
  const { t, i18n } = useTranslation()
  const calendarLocale = resolveDayPickerLocale(i18n.language)
  const [open, setOpen] = useState(false)
  const [draftStart, setDraftStart] = useState<Date | undefined>(start)
  const [draftEnd, setDraftEnd] = useState<Date | undefined>(end)
  const [startTime, setStartTime] = useState(timeFromDate(start))
  const [endTime, setEndTime] = useState(timeFromDate(end, '23:59'))

  const label = useMemo(() => {
    if (!start && !end) return t('Date Range')
    const startText = start ? dayjs(start).format('YYYY-MM-DD HH:mm') : '-'
    const endText = end ? dayjs(end).format('YYYY-MM-DD HH:mm') : '-'
    return `${startText} ~ ${endText}`
  }, [end, start, t])

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) {
      setDraftStart(start)
      setDraftEnd(end)
      setStartTime(timeFromDate(start))
      setEndTime(timeFromDate(end, '23:59'))
    }
    setOpen(nextOpen)
  }

  const applyDraft = () => {
    onChange({
      start: withTime(draftStart, startTime),
      end: withTime(draftEnd, endTime),
    })
    setOpen(false)
  }

  const applyPreset = (kind: 'today' | '7d' | 'week' | '30d' | 'month') => {
    const now = dayjs()
    const presets = {
      today: {
        start: now.startOf('day').toDate(),
        end: now.endOf('day').toDate(),
      },
      '7d': {
        start: now.subtract(6, 'day').startOf('day').toDate(),
        end: now.endOf('day').toDate(),
      },
      week: {
        start: now.startOf('week').toDate(),
        end: now.endOf('week').toDate(),
      },
      '30d': {
        start: now.subtract(29, 'day').startOf('day').toDate(),
        end: now.endOf('day').toDate(),
      },
      month: {
        start: now.startOf('month').toDate(),
        end: now.endOf('month').toDate(),
      },
    }
    const range = presets[kind]
    onChange(range)
    setOpen(false)
  }

  const handleRangeSelect = (range: DateRange | undefined) => {
    setDraftStart(range?.from)
    setDraftEnd(range?.to)
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            className={cn(
              'w-full justify-start gap-2 px-2.5 text-sm leading-5 font-normal tabular-nums',
              !start && !end && 'text-muted-foreground',
              className
            )}
          />
        }
      >
        <CalendarDays className='text-muted-foreground size-4 shrink-0' />
        <span className='truncate'>{label}</span>
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[min(520px,calc(100vw-2rem))] p-3'
      >
        <div className='space-y-3'>
          <Calendar
            mode='range'
            selected={{ from: draftStart, to: draftEnd }}
            onSelect={handleRangeSelect}
            locale={calendarLocale}
            captionLayout='label'
          />

          <div className='grid gap-2 sm:grid-cols-[1fr_auto_1fr] sm:items-end'>
            <div className='space-y-1.5'>
              <div className='text-muted-foreground text-xs'>
                {t('Start Time')}
              </div>
              <TimeInput
                value={startTime}
                onChange={setStartTime}
                disabled={!draftStart}
                aria-label={t('Start Time')}
              />
            </div>
            <span className='text-muted-foreground hidden pb-2 text-xs sm:block'>
              ~
            </span>
            <div className='space-y-1.5'>
              <div className='text-muted-foreground text-xs'>
                {t('End Time')}
              </div>
              <TimeInput
                value={endTime}
                onChange={setEndTime}
                disabled={!draftEnd}
                aria-label={t('End Time')}
              />
            </div>
          </div>

          <div className='flex flex-wrap gap-1.5'>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => applyPreset('today')}
            >
              {t('Today')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => applyPreset('7d')}
            >
              {t('7 Days')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => applyPreset('week')}
            >
              {t('This week')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => applyPreset('30d')}
            >
              {t('30 Days')}
            </Button>
            <Button
              type='button'
              variant='secondary'
              size='sm'
              className='h-7 flex-1 px-2 text-xs'
              onClick={() => applyPreset('month')}
            >
              {t('This calendar month')}
            </Button>
          </div>

          <div className='flex justify-end'>
            <Button size='sm' className='h-8' onClick={applyDraft}>
              {t('Confirm')}
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}
