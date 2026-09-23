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
import { enUS, fr, ja, ru, vi, zhCN, zhTW } from 'react-day-picker/locale'

import {
  normalizeInterfaceLanguage,
  type InterfaceLanguageCode,
} from '@/i18n/languages'

const calendarLocales: Record<InterfaceLanguageCode, typeof enUS> = {
  en: enUS,
  zhCN,
  zhTW,
  fr,
  ru,
  ja,
  vi,
}

const weekdayColumnMessageKeys = [
  'Sun',
  'Mon',
  'Tue',
  'Wed',
  'Thu',
  'Fri',
  'Sat',
] as const

export function resolveDayPickerLocale(language?: string | null): typeof enUS {
  return calendarLocales[normalizeInterfaceLanguage(language)]
}

export function resolveWeekStartsOn(language?: string | null): number {
  const start = resolveDayPickerLocale(language).options?.weekStartsOn
  if (typeof start !== 'number' || start < 0 || start > 6) return 0
  return start
}

export function weekdayColumnKeys(weekStartsOn: number): string[] {
  const start = normalizeWeekdayIndex(weekStartsOn)
  return [
    ...weekdayColumnMessageKeys.slice(start),
    ...weekdayColumnMessageKeys.slice(0, start),
  ]
}

export function monthGridLeadingDays(
  firstDayOfWeek: number,
  weekStartsOn: number
): number {
  return (
    normalizeWeekdayIndex(firstDayOfWeek) - normalizeWeekdayIndex(weekStartsOn) + 7
  ) % 7
}

function normalizeWeekdayIndex(day: number): number {
  if (!Number.isFinite(day)) return 0
  return ((Math.trunc(day) % 7) + 7) % 7
}
