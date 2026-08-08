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
import { useQueryClient } from '@tanstack/react-query'
import type { ColumnDef, RowSelectionState } from '@tanstack/react-table'
import {
  Search,
  Info,
  MousePointerClick,
  ChevronLeft,
  ChevronRight,
} from 'lucide-react'
import { useEffect, useMemo, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTableView, useDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useIsMobile } from '@/hooks/use-mobile'

import { applyUpstreamOverwrite } from '../../api'
import {
  buildRowSelectionForRowIds,
  getRowSelectionStateForRowIds,
  refreshModelSyncQueries,
  runUpstreamConflictSubmitFlow,
  type UpstreamConflictSelection,
} from '../../lib'
import { useModels } from '../models-provider'

const FIELD_LABELS: Record<string, string> = {
  description: 'Description',
  icon: 'Icon',
  tags: 'Tags',
  vendor: 'Vendor',
  name_rule: 'Name Rule',
  status: 'Status',
  endpoints: 'Endpoints',
  quota_types: 'Quota Types',
  enable_groups: 'Enable Groups',
}

const PAGE_SIZE_OPTIONS = [5, 10, 20, 50] as const

const formatValue = (value: unknown) => {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'string') return value || '—'
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

type UpstreamConflictDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type ConflictFieldRow = {
  id: string
  modelName: string
  fieldKey: string
  fieldLabel: string
  localValue: unknown
  upstreamValue: unknown
}

function ValuePreview({ value }: { value: unknown }) {
  return (
    <pre className='bg-muted/70 text-muted-foreground max-h-32 overflow-auto rounded-md border px-2 py-1.5 font-mono text-xs break-words whitespace-pre-wrap'>
      {formatValue(value)}
    </pre>
  )
}

export function UpstreamConflictDialog({
  open,
  onOpenChange,
}: UpstreamConflictDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const {
    upstreamConflicts = [],
    setUpstreamConflicts,
    upstreamMissing,
    setUpstreamMissing,
    syncWizardOptions,
  } = useModels()
  const isMobile = useIsMobile()
  const [search, setSearch] = useState('')
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({})
  const [syncMissing, setSyncMissing] = useState(false)
  const [pageSize, setPageSize] = useState(10)
  const [pageIndex, setPageIndex] = useState(0)

  useEffect(() => {
    if (open) {
      setRowSelection({})
      setSearch('')
      setPageIndex(0)
      setSyncMissing(upstreamMissing.length > 0)
    } else {
      setSyncMissing(false)
    }
  }, [open, upstreamConflicts, upstreamMissing.length])

  const conflictRows = useMemo<ConflictFieldRow[]>(() => {
    return upstreamConflicts.flatMap((conflict) => {
      if (!conflict.fields?.length) {
        return []
      }

      return conflict.fields.map((field) => ({
        id: `${conflict.model_name}-${field.field}`,
        modelName: conflict.model_name,
        fieldKey: field.field,
        fieldLabel: FIELD_LABELS[field.field] || field.field,
        localValue: field.local,
        upstreamValue: field.upstream,
      }))
    })
  }, [upstreamConflicts])

  const totalModels = upstreamConflicts.length
  const totalFields = conflictRows.length
  const normalizedSearch = search.trim().toLowerCase()

  const { matchingModelNames, visibleRowIds } = useMemo(() => {
    if (!normalizedSearch) {
      return { matchingModelNames: null, visibleRowIds: null }
    }

    const modelMatches = new Set<string>()
    upstreamConflicts.forEach((conflict) => {
      const modelMatch = conflict.model_name
        ?.toLowerCase()
        .includes(normalizedSearch)

      if (modelMatch) {
        modelMatches.add(conflict.model_name)
        return
      }

      const fieldMatch = conflict.fields?.some((field) => {
        const label = FIELD_LABELS[field.field] || field.field
        return (
          label.toLowerCase().includes(normalizedSearch) ||
          field.field.toLowerCase().includes(normalizedSearch)
        )
      })

      if (fieldMatch) {
        modelMatches.add(conflict.model_name)
      }
    })

    const rowIdSet = new Set<string>()
    conflictRows.forEach((row) => {
      if (
        modelMatches.has(row.modelName) ||
        row.fieldLabel.toLowerCase().includes(normalizedSearch) ||
        row.fieldKey.toLowerCase().includes(normalizedSearch)
      ) {
        rowIdSet.add(row.id)
      }
    })

    return { matchingModelNames: modelMatches, visibleRowIds: rowIdSet }
  }, [normalizedSearch, upstreamConflicts, conflictRows])

  const filteredConflictRows = useMemo(() => {
    return visibleRowIds
      ? conflictRows.filter((row) => visibleRowIds.has(row.id))
      : conflictRows
  }, [conflictRows, visibleRowIds])

  const totalFilteredFields = filteredConflictRows.length
  const totalPages =
    totalFilteredFields === 0 ? 1 : Math.ceil(totalFilteredFields / pageSize)

  useEffect(() => {
    setPageIndex((prev) => Math.min(prev, Math.max(0, totalPages - 1)))
  }, [totalPages])

  const pageStart = pageIndex * pageSize
  const paginatedConflictRows = useMemo(() => {
    return filteredConflictRows.slice(pageStart, pageStart + pageSize)
  }, [filteredConflictRows, pageSize, pageStart])
  const paginatedRowIds = useMemo(
    () => paginatedConflictRows.map((row) => row.id),
    [paginatedConflictRows]
  )
  const pageSelectionState = useMemo(
    () => getRowSelectionStateForRowIds(rowSelection, paginatedRowIds),
    [rowSelection, paginatedRowIds]
  )

  const columns = useMemo<ColumnDef<ConflictFieldRow>[]>(() => {
    const modelColumn: ColumnDef<ConflictFieldRow> = {
      accessorKey: 'modelName',
      header: 'Model',
      cell: ({ row }) => (
        <div className='flex items-start gap-3'>
          {isMobile ? (
            <Checkbox
              checked={row.getIsSelected()}
              onCheckedChange={(value) => row.toggleSelected(!!value)}
              aria-label='Select row'
            />
          ) : null}
          <div className='space-y-1'>
            <p className='leading-none font-medium'>{row.original.modelName}</p>
            <span className='text-muted-foreground font-mono text-xs'>
              {row.original.fieldKey}
            </span>
            {isMobile ? (
              <StatusBadge
                label={row.original.fieldLabel}
                variant='neutral'
                size='sm'
                copyable={false}
              />
            ) : null}
          </div>
        </div>
      ),
      size: 220,
    }

    const diffColumn: ColumnDef<ConflictFieldRow> = {
      id: 'actions',
      header: 'Diff',
      enableSorting: false,
      enableHiding: false,
      cell: ({ row }) => (
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant='ghost'
                size='sm'
                className={isMobile ? 'h-7 w-7 p-0' : 'h-7 gap-2 px-2 text-xs'}
              />
            }
          >
            <MousePointerClick className='h-3.5 w-3.5' />
            {!isMobile && 'View diff'}
          </PopoverTrigger>
          <PopoverContent className='w-[min(90vw,24rem)] space-y-4 text-sm'>
            <div>
              <StatusBadge
                label='Local'
                variant='neutral'
                size='sm'
                copyable={false}
                className='mb-1'
              />
              <pre className='bg-muted rounded-md p-2 text-xs'>
                {formatValue(row.original.localValue)}
              </pre>
            </div>
            <div>
              <StatusBadge
                label='Upstream'
                variant='info'
                size='sm'
                copyable={false}
                className='mb-1'
              />
              <pre className='bg-muted rounded-md p-2 text-xs'>
                {formatValue(row.original.upstreamValue)}
              </pre>
            </div>
          </PopoverContent>
        </Popover>
      ),
      size: isMobile ? 50 : 140,
    }

    if (isMobile) {
      return [modelColumn, diffColumn]
    }

    const selectionColumn: ColumnDef<ConflictFieldRow> = {
      id: 'select',
      header: () => (
        <Checkbox
          checked={pageSelectionState.checked}
          indeterminate={pageSelectionState.indeterminate}
          onCheckedChange={(value) =>
            setRowSelection((currentSelection) =>
              buildRowSelectionForRowIds(
                currentSelection,
                paginatedRowIds,
                value === true
              )
            )
          }
          aria-label='Select all'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label='Select row'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    }

    return [
      selectionColumn,
      modelColumn,
      {
        accessorKey: 'fieldLabel',
        header: 'Field',
        cell: ({ row }) => (
          <StatusBadge
            label={row.original.fieldLabel}
            variant='neutral'
            size='sm'
            copyable={false}
          />
        ),
        enableSorting: false,
        size: 160,
      },
      {
        accessorKey: 'localValue',
        header: 'Local Value',
        cell: ({ row }) => <ValuePreview value={row.original.localValue} />,
      },
      {
        accessorKey: 'upstreamValue',
        header: 'Upstream Value',
        cell: ({ row }) => <ValuePreview value={row.original.upstreamValue} />,
      },
    ]
  }, [
    isMobile,
    pageSelectionState.checked,
    pageSelectionState.indeterminate,
    paginatedRowIds,
  ])

  const { table } = useDataTable({
    data: conflictRows,
    columns,
    rowSelection,
    enableRowSelection: true,
    onRowSelectionChange: setRowSelection,
    getRowId: (row) => row.id,
    withFilteredRowModel: false,
    withPaginationRowModel: false,
    withSortedRowModel: false,
    withFacetedRowModel: false,
  })

  const totalSelectedFields = table.getSelectedRowModel().rows.length
  const hasSelection = totalSelectedFields > 0
  const missingCount = upstreamMissing.length
  const canApply = hasSelection || syncMissing
  const paginatedRowIdSet = useMemo(
    () => new Set(paginatedRowIds),
    [paginatedRowIds]
  )
  const paginatedRows = table
    .getRowModel()
    .rows.filter((row) => paginatedRowIdSet.has(row.id))
  const displayStart = totalFilteredFields === 0 ? 0 : pageStart + 1
  const displayEnd =
    totalFilteredFields === 0
      ? 0
      : Math.min(pageStart + pageSize, totalFilteredFields)
  const currentPageDisplay = totalFilteredFields === 0 ? 0 : pageIndex + 1
  const totalPagesDisplay = totalFilteredFields === 0 ? 0 : totalPages

  const visibleModelCount = matchingModelNames?.size ?? totalModels
  const visibleFieldCount = totalFilteredFields
  const hasConflicts = totalFields > 0
  const showSearchEmptyState = hasConflicts && totalFilteredFields === 0

  const clearSelections = useCallback(() => {
    setRowSelection({})
  }, [])

  const handleApplyOverwrite = async () => {
    const selectedRows = table.getSelectedRowModel().rows
    const groupedSelections = selectedRows.reduce<UpstreamConflictSelection>(
      (acc, row) => {
        const key = row.original.modelName
        if (!acc[key]) {
          acc[key] = new Set()
        }
        acc[key].add(row.original.fieldKey)
        return acc
      },
      {}
    )

    setIsSubmitting(true)
    try {
      const result = await runUpstreamConflictSubmitFlow({
        selections: groupedSelections,
        syncMissing,
        missing: upstreamMissing,
        locale: syncWizardOptions.locale,
        source: syncWizardOptions.source,
        applyUpstreamOverwrite,
        refreshModelSyncQueries: () => refreshModelSyncQueries(queryClient),
      })

      if (result.status === 'empty') {
        toast.warning(
          t(
            'Select at least one field to overwrite or enable missing model sync.'
          )
        )
        return
      }

      if (result.status === 'synced') {
        const {
          created_models = 0,
          created_vendors = 0,
          updated_models = 0,
        } = result.data || {}
        toast.success(
          result.hasOverwrite
            ? t(
                'Sync completed! Created {{created}} models, updated {{updated}}, and added {{vendors}} vendors.',
                {
                  created: created_models,
                  updated: updated_models,
                  vendors: created_vendors,
                }
              )
            : t(
                'Missing upstream models were synced successfully. Created {{created}} models and added {{vendors}} vendors.',
                {
                  created: created_models,
                  vendors: created_vendors,
                }
              )
        )
        setUpstreamConflicts([])
        setUpstreamMissing([])
        onOpenChange(false)
        return
      }

      if (result.status === 'refresh_failed') {
        toast.error(
          t(
            'Sync completed, but refreshing page data failed. Please refresh manually to view the latest results.'
          )
        )
        setUpstreamConflicts([])
        setUpstreamMissing([])
        onOpenChange(false)
        return
      }

      toast.error(result.message || t('Failed to apply overwrite.'))
    } catch (error: unknown) {
      toast.error((error as Error)?.message || t('Failed to apply overwrite.'))
    } finally {
      setIsSubmitting(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          setUpstreamConflicts([])
          setUpstreamMissing([])
        }
        onOpenChange(nextOpen)
      }}
      title={t('Resolve Conflicts')}
      description={t(
        'Select upstream fields to overwrite. Missing models can be synced in the same request.'
      )}
      contentClassName='w-full sm:max-w-5xl'
      contentHeight='min(72vh, 720px)'
      bodyClassName='flex flex-col gap-4'
      initialFocus={!isMobile}
      footerClassName='sm:justify-between'
      footer={
        <div className='flex w-full flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
          <div className='flex flex-1 flex-col gap-2'>
            {missingCount > 0 ? (
              <div className='flex items-start gap-2 text-xs'>
                <Checkbox
                  checked={syncMissing}
                  onCheckedChange={(value) => setSyncMissing(value === true)}
                  aria-label={t('Sync missing upstream models')}
                  className='mt-0.5'
                />
                <div className='space-y-0.5'>
                  <div className='font-medium'>
                    {t('Sync {{count}} missing upstream model{{suffix}}', {
                      count: missingCount,
                      suffix: missingCount === 1 ? '' : 's',
                    })}
                  </div>
                  <div className='text-muted-foreground'>
                    {t('Disable this to apply conflict overwrites only.')}
                  </div>
                </div>
              </div>
            ) : null}
            <div className='text-muted-foreground flex items-start gap-2 text-xs'>
              <Info className='h-4 w-4 flex-shrink-0' />
              <span>
                {t(
                  'Only selected fields will be overwritten. Unselected fields keep their local values.'
                )}
              </span>
            </div>
          </div>
          <div className='flex flex-col gap-2 sm:flex-row sm:justify-end'>
            <Button
              variant='outline'
              onClick={() => {
                setUpstreamConflicts([])
                setUpstreamMissing([])
                onOpenChange(false)
              }}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleApplyOverwrite}
              disabled={isSubmitting || !canApply}
            >
              {isSubmitting ? t('Applying...') : t('Apply Overwrite')}
            </Button>
          </div>
        </div>
      }
    >
      <div className='flex min-h-0 flex-1 flex-col gap-4'>
        {!hasConflicts ? (
          <div className='text-muted-foreground flex flex-1 items-center justify-center rounded-md border border-dashed p-8 text-center text-sm'>
            {t('No conflict entries available.')}
          </div>
        ) : (
          <div className='flex min-h-0 flex-1 flex-col gap-4 overflow-hidden'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
              <div className='space-y-1'>
                <div className='text-sm font-medium'>
                  {visibleModelCount} {t('model')}
                  {visibleModelCount === 1 ? '' : 's'} {t('with conflicts')}
                </div>
                <div className='text-muted-foreground text-xs'>
                  {visibleFieldCount} {t('field')}
                  {visibleFieldCount === 1 ? '' : 's'} {t('showing •')}{' '}
                  {totalSelectedFields} {t('selected')}
                </div>
              </div>
              <div className='flex w-full flex-col gap-2 sm:w-auto sm:flex-row'>
                <div className='relative flex-1'>
                  <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
                  <Input
                    value={search}
                    onChange={(event) => {
                      setSearch(event.target.value)
                      setPageIndex(0)
                    }}
                    placeholder={t('Search models or fields...')}
                    className='pl-9'
                    aria-label={t('Search conflicting models or fields')}
                  />
                </div>
                <Button
                  variant='ghost'
                  size='sm'
                  onClick={clearSelections}
                  disabled={!hasSelection}
                >
                  {t('Clear selection')}
                </Button>
              </div>
            </div>

            {showSearchEmptyState ? (
              <div className='text-muted-foreground flex flex-1 items-center justify-center rounded-md border border-dashed p-8 text-center text-sm'>
                {t('No conflicts match your search.')}
              </div>
            ) : (
              <div className='flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border'>
                <div className='flex-1 overflow-auto'>
                  <DataTableView
                    table={table}
                    rows={paginatedRows}
                    containerClassName='border-0'
                    tableContainerClassName={
                      isMobile ? 'min-w-full' : 'min-w-[720px]'
                    }
                  />
                </div>

                <div className='bg-muted/40 flex flex-col gap-2 border-t px-2 py-1.5 text-sm sm:flex-row sm:items-center sm:justify-between sm:gap-3 sm:px-3 sm:py-2'>
                  <div className='text-muted-foreground text-xs'>
                    {t('Showing')} {displayStart}-{displayEnd} {t('of')}{' '}
                    {visibleFieldCount} {t('field')}
                    {visibleFieldCount === 1 ? '' : 's'}
                  </div>
                  <div className='flex items-center justify-between gap-2 sm:flex-wrap sm:gap-3'>
                    <div className='flex items-center gap-1.5 text-xs sm:gap-2'>
                      <span className='hidden sm:inline'>
                        {t('Rows per page')}
                      </span>
                      <Select
                        items={PAGE_SIZE_OPTIONS.map((size) => ({
                          value: String(size),
                          label: size,
                        }))}
                        value={String(pageSize)}
                        onValueChange={(value) => {
                          setPageSize(Number(value))
                          setPageIndex(0)
                        }}
                      >
                        <SelectTrigger className='h-8 w-[70px] text-xs sm:h-8 sm:w-[72px]'>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent alignItemWithTrigger={false}>
                          <SelectGroup>
                            {PAGE_SIZE_OPTIONS.map((size) => (
                              <SelectItem key={size} value={String(size)}>
                                {size}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className='flex items-center gap-1'>
                      <Button
                        variant='outline'
                        size='icon'
                        className='h-7 w-7 sm:h-8 sm:w-8'
                        onClick={() =>
                          setPageIndex((prev) => Math.max(0, prev - 1))
                        }
                        disabled={pageIndex === 0}
                        aria-label={t('Previous page')}
                      >
                        <ChevronLeft className='h-3.5 w-3.5 sm:h-4 sm:w-4' />
                      </Button>
                      <span className='text-xs font-medium'>
                        {t('Page {{current}} of {{total}}', {
                          current: currentPageDisplay,
                          total: totalPagesDisplay,
                        })}
                      </span>
                      <Button
                        variant='outline'
                        size='icon'
                        className='h-7 w-7 sm:h-8 sm:w-8'
                        onClick={() =>
                          setPageIndex((prev) =>
                            Math.min(totalPages - 1, prev + 1)
                          )
                        }
                        disabled={
                          pageIndex >= totalPages - 1 ||
                          totalFilteredFields === 0
                        }
                        aria-label={t('Next page')}
                      >
                        <ChevronRight className='h-3.5 w-3.5 sm:h-4 sm:w-4' />
                      </Button>
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </Dialog>
  )
}
