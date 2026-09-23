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
import { Calendar as CalendarIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

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

type DatePickerProps = {
  selected: Date | undefined
  onSelect: (date: Date | undefined) => void
  placeholder?: string
  className?: string
  disableFuture?: boolean
  'aria-label'?: string
}

export function DatePicker({
  selected,
  onSelect,
  placeholder,
  className,
  disableFuture = true,
  'aria-label': ariaLabel,
}: DatePickerProps) {
  const { t, i18n } = useTranslation()
  const placeholderText = placeholder ?? t('Pick a date')
  const calendarLocale = resolveDayPickerLocale(i18n.language)
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            variant='outline'
            data-empty={!selected}
            aria-label={ariaLabel}
            className={cn(
              'data-[empty=true]:text-muted-foreground w-[240px] justify-start text-start font-normal',
              className
            )}
          />
        }
      >
        {selected ? (
          dayjs(selected).format('YYYY-MM-DD')
        ) : (
          <span>{placeholderText}</span>
        )}
        <CalendarIcon className='ms-auto h-4 w-4 opacity-50' />
      </PopoverTrigger>
      <PopoverContent className='w-auto p-0'>
        <Calendar
          mode='single'
          captionLayout='dropdown'
          selected={selected}
          onSelect={onSelect}
          locale={calendarLocale}
          disabled={(date: Date) =>
            date < new Date('1900-01-01') ||
            (disableFuture && date > new Date())
          }
        />
      </PopoverContent>
    </Popover>
  )
}
