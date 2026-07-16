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
import type {
  PreviewUpstreamDiffResponse,
  SyncLocale,
  SyncOverwritePayload,
  SyncSource,
  SyncUpstreamParams,
  SyncUpstreamResponse,
} from '../types'

export type UpstreamConflictSelection = Record<string, Set<string>>

export type UpstreamRowSelectionState = Record<string, boolean>

export type UpstreamConflictSubmitPayload = {
  overwrite: SyncOverwritePayload[]
  missing?: string[]
  skip_missing: boolean
}

export function getRowSelectionStateForRowIds(
  rowSelection: UpstreamRowSelectionState,
  rowIds: readonly string[],
): { checked: boolean; indeterminate: boolean } {
  if (rowIds.length === 0) {
    return { checked: false, indeterminate: false }
  }

  const selectedCount = rowIds.filter((id) => rowSelection[id]).length

  return {
    checked: selectedCount === rowIds.length,
    indeterminate: selectedCount > 0 && selectedCount < rowIds.length,
  }
}

export function buildRowSelectionForRowIds(
  rowSelection: UpstreamRowSelectionState,
  rowIds: readonly string[],
  checked: boolean,
): UpstreamRowSelectionState {
  const targetIds = new Set(rowIds)
  const nextSelection: UpstreamRowSelectionState = {}

  Object.entries(rowSelection).forEach(([id, selected]) => {
    if (selected && !targetIds.has(id)) {
      nextSelection[id] = true
    }
  })

  if (checked) {
    rowIds.forEach((id) => {
      nextSelection[id] = true
    })
  }

  return nextSelection
}

export function buildUpstreamConflictSubmitPayload(
  selections: UpstreamConflictSelection,
  syncMissing: boolean,
  missing?: readonly string[],
): UpstreamConflictSubmitPayload | null {
  const overwrite = Object.entries(selections)
    .map(([modelName, fields]) => ({
      model_name: modelName,
      fields: Array.from(fields),
    }))
    .filter((item) => item.fields.length > 0)

  if (overwrite.length === 0 && !syncMissing) {
    return null
  }

  const payload: UpstreamConflictSubmitPayload = {
    overwrite,
    skip_missing: !syncMissing,
  }

  if (Array.isArray(missing)) {
    payload.missing = [...missing]
  }

  return payload
}

type RefreshModelSyncQueries = () => Promise<void>

type SyncWizardFlowOptions = {
  locale: SyncLocale
  source: SyncSource
  previewUpstreamDiff: (params: {
    locale: SyncLocale
    source: SyncSource
  }) => Promise<PreviewUpstreamDiffResponse>
  syncUpstream: (params: SyncUpstreamParams) => Promise<SyncUpstreamResponse>
  refreshModelSyncQueries: RefreshModelSyncQueries
}

export type SyncWizardFlowResult =
  | {
      status: 'conflict'
      conflicts: NonNullable<PreviewUpstreamDiffResponse['data']>['conflicts']
      missing: string[]
    }
  | { status: 'synced'; data: SyncUpstreamResponse['data'] }
  | { status: 'preview_failed'; message?: string }
  | { status: 'sync_failed'; message?: string }
  | { status: 'refresh_failed'; error: unknown }

export async function runSyncWizardFlow({
  locale,
  source,
  previewUpstreamDiff,
  syncUpstream,
  refreshModelSyncQueries,
}: SyncWizardFlowOptions): Promise<SyncWizardFlowResult> {
  const previewRes = await previewUpstreamDiff({ locale, source })

  if (!previewRes.success) {
    return {
      status: 'preview_failed',
      message: previewRes.message || 'Failed to preview upstream diff',
    }
  }

  const conflicts = previewRes.data?.conflicts || []
  const missing = Array.isArray(previewRes.data?.missing)
    ? previewRes.data.missing
    : []

  if (conflicts.length > 0) {
    return { status: 'conflict', conflicts, missing }
  }

  const response = await syncUpstream({ locale, source, missing })
  if (!response.success) {
    return {
      status: 'sync_failed',
      message: response.message || 'Sync failed',
    }
  }

  try {
    await refreshModelSyncQueries()
  } catch (error) {
    return { status: 'refresh_failed', error }
  }

  return { status: 'synced', data: response.data }
}

type UpstreamConflictSubmitFlowOptions = {
  selections: UpstreamConflictSelection
  syncMissing: boolean
  missing: readonly string[]
  locale: SyncLocale
  source: SyncSource
  applyUpstreamOverwrite: (
    params: SyncUpstreamParams & { overwrite: SyncOverwritePayload[] },
  ) => Promise<SyncUpstreamResponse>
  refreshModelSyncQueries: RefreshModelSyncQueries
}

export type UpstreamConflictSubmitFlowResult =
  | { status: 'empty' }
  | {
      status: 'synced'
      hasOverwrite: boolean
      data: SyncUpstreamResponse['data']
    }
  | { status: 'sync_failed'; message?: string }
  | { status: 'refresh_failed'; error: unknown }

export async function runUpstreamConflictSubmitFlow({
  selections,
  syncMissing,
  missing,
  locale,
  source,
  applyUpstreamOverwrite,
  refreshModelSyncQueries,
}: UpstreamConflictSubmitFlowOptions): Promise<UpstreamConflictSubmitFlowResult> {
  const submitPayload = buildUpstreamConflictSubmitPayload(
    selections,
    syncMissing,
    missing,
  )

  if (!submitPayload) {
    return { status: 'empty' }
  }

  const response = await applyUpstreamOverwrite({
    ...submitPayload,
    locale,
    source,
  })

  if (!response.success) {
    return {
      status: 'sync_failed',
      message: response.message || 'Failed to apply overwrite.',
    }
  }

  try {
    await refreshModelSyncQueries()
  } catch (error) {
    return { status: 'refresh_failed', error }
  }

  return {
    status: 'synced',
    hasOverwrite: submitPayload.overwrite.length > 0,
    data: response.data,
  }
}
