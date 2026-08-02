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

export type AnnouncementLike = string | {
  id?: unknown
  publishDate?: unknown
  content?: unknown
  extra?: unknown
  type?: unknown
  title?: unknown
  link?: unknown
}

function hashString(input: string): string {
  let hash = 0
  if (!input) return '0'

  for (let i = 0; i < input.length; i += 1) {
    const chr = input.charCodeAt(i)
    hash = (hash << 5) - hash + chr
    hash |= 0
  }

  return hash.toString(36)
}

export function getAnnouncementKey(
  item: AnnouncementLike | null | undefined
): string {
  if (!item) return ''
  if (typeof item === 'string') {
    return `hash:${hashString(item)}`
  }

  if (item.id !== undefined && item.id !== null) {
    return `id:${item.id}`
  }

  const publishDate =
    item.publishDate instanceof Date
      ? item.publishDate.toISOString()
      : typeof item.publishDate === 'string'
        ? item.publishDate
        : ''
  const toText = (value: unknown) =>
    typeof value === 'string' ? value.trim() : ''

  const fingerprint = JSON.stringify({
    publishDate,
    content: toText(item.content),
    extra: toText(item.extra),
    type: toText(item.type),
    title: toText(item.title),
    link: toText(item.link),
  })
  return `hash:${hashString(fingerprint)}`
}
