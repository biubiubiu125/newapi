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
import { MODEL_FETCHABLE_TYPES } from '../constants'
import type { Channel } from '../types'
import { isChannelEnabled } from './channel-utils'

export function supportsChannelUpstreamModelUpdate(
  channel:
    | {
        type: number
        channel_info?: { is_multi_key?: boolean } | null
        is_draft_multi_key?: boolean
      }
    | null
    | undefined
): boolean {
  return Boolean(
    channel &&
    MODEL_FETCHABLE_TYPES.has(channel.type) &&
    channel.channel_info?.is_multi_key !== true &&
    channel.is_draft_multi_key !== true
  )
}

export function canUseChannelUpstreamUpdates(
  channel:
    | {
        type: number
        status?: number
        channel_info?: { is_multi_key?: boolean } | null
        is_draft_multi_key?: boolean
      }
    | null
    | undefined,
  upstreamUpdateMeta: { enabled?: boolean } | null | undefined
): boolean {
  return Boolean(
    channel &&
    supportsChannelUpstreamModelUpdate(channel) &&
    isChannelEnabled(channel as Channel) &&
    upstreamUpdateMeta?.enabled === true
  )
}

export function canFetchChannelUpstreamModels({
  canSensitiveWriteChannel = false,
}: {
  canSensitiveWriteChannel?: boolean
} = {}): boolean {
  return canSensitiveWriteChannel === true
}

export function shouldUseDraftFetchModels({
  isEditing,
  draftHasChanges,
  canFetchSavedModels,
}: {
  isEditing: boolean
  draftHasChanges: boolean
  canFetchSavedModels: boolean
}): boolean {
  if (!isEditing) return true
  return draftHasChanges || !canFetchSavedModels
}

export function normalizeModelList(models: unknown = []): string[] {
  let source: unknown[] = []
  if (Array.isArray(models)) {
    source = models
  } else if (typeof models === 'string') {
    source = models.split(',')
  }
  return [
    ...new Set(
      source.map((model) => String(model || '').trim()).filter(Boolean)
    ),
  ]
}

export function parseUpstreamUpdateMeta(settings: unknown): {
  enabled: boolean
  pendingAddModels: string[]
  pendingRemoveModels: string[]
} {
  let parsed: Record<string, unknown> | null = null
  if (settings && typeof settings === 'object' && !Array.isArray(settings)) {
    parsed = settings as Record<string, unknown>
  } else if (typeof settings === 'string') {
    try {
      parsed = JSON.parse(settings)
    } catch {
      parsed = null
    }
  }

  if (!parsed || typeof parsed !== 'object') {
    return { enabled: false, pendingAddModels: [], pendingRemoveModels: [] }
  }

  return {
    enabled: parsed.upstream_model_update_check_enabled === true,
    pendingAddModels: normalizeModelList(
      parsed.upstream_model_update_last_detected_models
    ),
    pendingRemoveModels: normalizeModelList(
      parsed.upstream_model_update_last_removed_models
    ),
  }
}
