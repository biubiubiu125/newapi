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
import { Loader2, Search, Info, ChevronDown } from 'lucide-react'
import { useState, useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { fetchUpstreamModels, updateChannel } from '../../api'
import {
  categorizeModels,
  categorizeModelsWithRedirect,
  channelsQueryKeys,
  collectRemovedUpstreamModels,
  normalizeModelName,
  parseModelsString,
} from '../../lib'
import { useChannels } from '../channels-provider'

type FetchModelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onModelsSelected?: (models: string[]) => void
  redirectModels?: string[]
  redirectSourceModels?: string[]
  customFetcher?: () => Promise<string[]>
  existingModelsOverride?: string[]
  channelName?: string | null
  canSaveModels?: boolean
}

export function FetchModelsDialog({
  open,
  onOpenChange,
  onModelsSelected,
  redirectModels = [],
  redirectSourceModels = [],
  customFetcher,
  existingModelsOverride,
  channelName,
  canSaveModels = true,
}: FetchModelsDialogProps) {
  const { t } = useTranslation()
  const { currentRow } = useChannels()
  const activeChannel = customFetcher ? null : currentRow
  const queryClient = useQueryClient()
  const [isFetching, setIsFetching] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [fetchedModels, setFetchedModels] = useState<string[]>([])
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [hasSuccessfulFetch, setHasSuccessfulFetch] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const fetchRequestGenerationRef = useRef(0)

  // Parse existing models
  const existingModels = useMemo(
    () =>
      existingModelsOverride ?? parseModelsString(activeChannel?.models || ''),
    [existingModelsOverride, activeChannel?.models]
  )

  // Categorize models with redirect models
  const modelCategories = useMemo(
    () => categorizeModelsWithRedirect(existingModels, redirectModels),
    [existingModels, redirectModels]
  )

  const { classificationSet, redirectOnlySet } = modelCategories

  const removedModels = useMemo(() => {
    if (!hasSuccessfulFetch) return []
    return collectRemovedUpstreamModels({
      existingModels,
      fetchedModels,
      redirectSourceModels,
      searchKeyword,
    })
  }, [
    existingModels,
    fetchedModels,
    hasSuccessfulFetch,
    redirectSourceModels,
    searchKeyword,
  ])

  useEffect(() => {
    if (open && (activeChannel || customFetcher)) {
      handleFetchModels()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, activeChannel?.id, customFetcher])

  useEffect(() => {
    return () => {
      fetchRequestGenerationRef.current += 1
    }
  }, [])

  const handleFetchModels = async () => {
    if (!activeChannel && !customFetcher) return

    const fetchGeneration = fetchRequestGenerationRef.current + 1
    fetchRequestGenerationRef.current = fetchGeneration
    const existingModelsSnapshot = existingModels
    const isCurrentFetch = () =>
      fetchRequestGenerationRef.current === fetchGeneration

    setIsFetching(true)
    setHasSuccessfulFetch(false)
    try {
      if (customFetcher) {
        const list = await customFetcher()
        if (!isCurrentFetch()) return
        setFetchedModels(list)
        setSelectedModels(existingModelsSnapshot)
        setHasSuccessfulFetch(true)
        toast.success(t('Fetched {{count}} models', { count: list.length }))
      } else if (activeChannel) {
        const response = await fetchUpstreamModels(activeChannel.id)
        if (!isCurrentFetch()) return
        if (response.success) {
          const list = Array.isArray(response.data) ? response.data : []
          setFetchedModels(list)
          setSelectedModels(existingModelsSnapshot)
          setHasSuccessfulFetch(true)
          toast.success(t('Fetched {{count}} models', { count: list.length }))
        } else {
          toast.error(response.message || t('Failed to fetch models'))
          setFetchedModels([])
          setSelectedModels([])
          setHasSuccessfulFetch(false)
        }
      }
    } catch (error: unknown) {
      if (!isCurrentFetch()) return
      toast.error(
        error instanceof Error ? error.message : t('Failed to fetch models')
      )
      setFetchedModels([])
      setSelectedModels([])
      setHasSuccessfulFetch(false)
    } finally {
      if (isCurrentFetch()) {
        setIsFetching(false)
      }
    }
  }

  const handleSave = async () => {
    if (!hasSuccessfulFetch) {
      toast.error(t('Please fetch models first'))
      return
    }

    // If onModelsSelected callback is provided, use it (form filling mode)
    if (onModelsSelected) {
      onModelsSelected(selectedModels)
      toast.success(t('Models filled to form'))
      onOpenChange(false)
      return
    }

    // Otherwise, directly save to API (standalone mode)
    if (!activeChannel) return
    if (!canSaveModels) {
      toast.error(t('No permission to perform this action'))
      return
    }
    setIsSaving(true)
    try {
      const modelsString = selectedModels.join(',')
      const response = await updateChannel(activeChannel.id, {
        models: modelsString,
      })
      if (response.success) {
        toast.success(t('Models updated successfully'))
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
        onOpenChange(false)
      } else {
        toast.error(response.message || t('Failed to update models'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to update models')
      )
    } finally {
      setIsSaving(false)
    }
  }

  const handleClose = () => {
    fetchRequestGenerationRef.current += 1
    setIsFetching(false)
    setFetchedModels([])
    setSelectedModels([])
    setHasSuccessfulFetch(false)
    setSearchKeyword('')
    onOpenChange(false)
  }

  // Filter models by search
  const filteredModels = useMemo(() => {
    if (!searchKeyword) return fetchedModels
    return fetchedModels.filter((model) =>
      model.toLowerCase().includes(searchKeyword.toLowerCase())
    )
  }, [fetchedModels, searchKeyword])

  const {
    newModels,
    existingFilteredModels,
    newModelsByCategory,
    existingModelsByCategory,
  } = useMemo(() => {
    const newModels: string[] = []
    const existingFilteredModels: string[] = []

    for (const model of filteredModels) {
      if (classificationSet.has(normalizeModelName(model))) {
        existingFilteredModels.push(model)
      } else {
        newModels.push(model)
      }
    }

    return {
      newModels,
      existingFilteredModels,
      newModelsByCategory: categorizeModels(newModels),
      existingModelsByCategory: categorizeModels(existingFilteredModels),
    }
  }, [classificationSet, filteredModels])

  // 厂商分类按 a-z 排序，Other 放最后，便于查找
  const getSortedCategoryEntries = (
    categories: Record<string, string[]>
  ): [string, string[]][] =>
    Object.entries(categories).sort(([a], [b]) => {
      if (a === 'Other') return 1
      if (b === 'Other') return -1
      return a.localeCompare(b, undefined, { sensitivity: 'base' })
    })

  const toggleModel = (model: string) => {
    setSelectedModels((prev) =>
      prev.includes(model) ? prev.filter((m) => m !== model) : [...prev, model]
    )
  }

  const toggleCategory = (categoryModels: string[], isChecked: boolean) => {
    setSelectedModels((prev) => {
      if (isChecked) {
        const newSelected = [...prev]
        categoryModels.forEach((model) => {
          if (!newSelected.includes(model)) {
            newSelected.push(model)
          }
        })
        return newSelected
      } else {
        return prev.filter((m) => !categoryModels.includes(m))
      }
    })
  }

  const isCategorySelected = (categoryModels: string[]) => {
    return categoryModels.every((m) => selectedModels.includes(m))
  }

  const renderModelCategory = (
    categoryName: string,
    categoryModels: string[]
  ) => {
    const allSelected = isCategorySelected(categoryModels)

    return (
      <Collapsible key={categoryName} defaultOpen>
        <CollapsibleTrigger className='hover:bg-muted/50 flex w-full items-center justify-between rounded-lg border p-3'>
          <div className='flex items-center gap-2'>
            <ChevronDown className='h-4 w-4' />
            <span className='font-medium'>
              {categoryName} ({categoryModels.length})
            </span>
          </div>
          <div className='flex items-center gap-2'>
            <span className='text-muted-foreground text-sm'>
              {categoryModels.filter((m) => selectedModels.includes(m)).length}{' '}
              / {categoryModels.length} selected
            </span>
            <Checkbox
              checked={allSelected}
              onCheckedChange={(checked) =>
                toggleCategory(categoryModels, !!checked)
              }
              onClick={(e) => e.stopPropagation()}
            />
          </div>
        </CollapsibleTrigger>
        <CollapsibleContent className='px-4 py-2'>
          <div className='grid grid-cols-2 gap-2'>
            {categoryModels.map((model) => (
              <div key={model} className='flex items-center space-x-2'>
                <Checkbox
                  id={model}
                  checked={selectedModels.includes(model)}
                  onCheckedChange={() => toggleModel(model)}
                />
                <Label
                  htmlFor={model}
                  className='flex cursor-pointer items-center gap-1.5 text-sm font-normal'
                >
                  <span>{model}</span>
                  {redirectOnlySet.has(normalizeModelName(model)) && (
                    <Tooltip>
                      <TooltipTrigger
                        render={<Info className='h-3.5 w-3.5 text-amber-500' />}
                      />
                      <TooltipContent>
                        {t('From model redirect, not yet added to models list')}
                      </TooltipContent>
                    </Tooltip>
                  )}
                </Label>
              </div>
            ))}
          </div>
        </CollapsibleContent>
      </Collapsible>
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className='max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Fetch Models')}</DialogTitle>
          <DialogDescription>
            {activeChannel ? (
              <>
                {t('Fetch available models for:')}{' '}
                <strong>{activeChannel.name}</strong>
              </>
            ) : channelName ? (
              <>
                {t('Fetch available models for:')}{' '}
                <strong>{channelName}</strong>
              </>
            ) : (
              t('Fetch available models from upstream')
            )}
          </DialogDescription>
        </DialogHeader>

        {!activeChannel && !customFetcher ? (
          <div className='text-muted-foreground py-8 text-center'>
            {t('No channel selected')}
          </div>
        ) : isFetching ? (
          <div className='flex items-center justify-center py-12'>
            <Loader2 className='text-muted-foreground h-8 w-8 animate-spin' />
          </div>
        ) : fetchedModels.length === 0 && removedModels.length === 0 ? (
          <div className='text-muted-foreground py-8 text-center'>
            <p>{t('No models fetched yet.')}</p>
            <Button
              className='mt-4'
              onClick={handleFetchModels}
              disabled={isFetching}
            >
              {t('Fetch Models')}
            </Button>
          </div>
        ) : (
          <>
            <div className='space-y-4'>
              {/* Search Bar */}
              <div className='relative'>
                <Search className='text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
                <Input
                  placeholder={t('Search models...')}
                  value={searchKeyword}
                  onChange={(e) => setSearchKeyword(e.target.value)}
                  className='pl-9'
                />
              </div>

              {/* Tabs for New vs Existing vs Removed */}
              <Tabs
                key={`${activeChannel?.id ?? 'custom'}-${
                  hasSuccessfulFetch ? 'fetched' : 'loading'
                }`}
                defaultValue={
                  newModels.length > 0
                    ? 'new'
                    : removedModels.length > 0
                      ? 'removed'
                      : 'existing'
                }
              >
                <TabsList
                  className={`grid w-full ${removedModels.length > 0 ? 'grid-cols-3' : 'grid-cols-2'}`}
                >
                  <TabsTrigger value='new' disabled={newModels.length === 0}>
                    {t('New Models ({{count}})', { count: newModels.length })}
                  </TabsTrigger>
                  <TabsTrigger
                    value='existing'
                    disabled={existingFilteredModels.length === 0}
                  >
                    {t('Existing Models ({{count}})', {
                      count: existingFilteredModels.length,
                    })}
                  </TabsTrigger>
                  {removedModels.length > 0 && (
                    <TabsTrigger value='removed'>
                      {t('Removed Models ({{count}})', {
                        count: removedModels.length,
                      })}
                    </TabsTrigger>
                  )}
                </TabsList>

                <TabsContent
                  value='new'
                  className='max-h-96 space-y-2 overflow-y-auto'
                >
                  {getSortedCategoryEntries(newModelsByCategory).map(
                    ([category, models]) =>
                      renderModelCategory(category, models)
                  )}
                </TabsContent>

                <TabsContent
                  value='existing'
                  className='max-h-96 space-y-2 overflow-y-auto'
                >
                  {getSortedCategoryEntries(existingModelsByCategory).map(
                    ([category, models]) =>
                      renderModelCategory(category, models)
                  )}
                </TabsContent>

                {removedModels.length > 0 && (
                  <TabsContent
                    value='removed'
                    className='max-h-96 space-y-2 overflow-y-auto'
                  >
                    <p className='text-muted-foreground text-xs'>
                      {t(
                        'These models are still in your selection but were not returned by the upstream listing. Entries that are only model_mapping source aliases are omitted. Toggle to adjust before saving.'
                      )}
                    </p>
                    {renderModelCategory(t('Removed'), removedModels)}
                  </TabsContent>
                )}
              </Tabs>

              {/* Selection Summary */}
              <div className='bg-muted/50 rounded-lg border p-3 text-sm'>
                {t('{{n}} model(s) selected', { n: selectedModels.length })}
              </div>
            </div>

            <DialogFooter>
              <Button
                variant='outline'
                onClick={handleClose}
                disabled={isSaving}
              >
                {t('Cancel')}
              </Button>
              <Button
                onClick={handleSave}
                disabled={isSaving || (!onModelsSelected && !canSaveModels)}
              >
                {isSaving && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
                {isSaving ? t('Saving...') : t('Save Models')}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
