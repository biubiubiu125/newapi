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
import { memo, useEffect, useMemo, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useUpdateOption } from '../hooks/use-update-option'
import { normalizeJsonString, validateJsonString } from '../models/utils'

const OPTION_KEY = 'ProviderPriceOverrides'

type ProviderPriceRow = {
  id: string
  group_name: string
  model_name: string
  input_price: string
  output_price: string
  cache_input_price: string
  cache_create_price: string
  cache_create_price_1h: string
  image_output_price: string
  enabled: boolean
  note: string
  sort_order: string
}

type ProviderPriceExportSectionProps = {
  defaultValue: string
}

function createRow(index: number): ProviderPriceRow {
  return {
    id: `provider-price-${Date.now()}-${index}`,
    group_name: '',
    model_name: '',
    input_price: '',
    output_price: '',
    cache_input_price: '',
    cache_create_price: '',
    cache_create_price_1h: '',
    image_output_price: '',
    enabled: true,
    note: '',
    sort_order: String(index),
  }
}

function parseInitialRows(value: string): ProviderPriceRow[] {
  try {
    const normalized = normalizeJsonString(value, '[]')
    const parsed = JSON.parse(normalized) as unknown
    if (!Array.isArray(parsed)) {
      return []
    }
    return parsed.map((item, index) => ({
      id:
        typeof item?.id === 'string' && item.id.trim()
          ? item.id.trim()
          : `provider-price-${index}`,
      group_name:
        typeof item?.group_name === 'string' ? item.group_name : '',
      model_name:
        typeof item?.model_name === 'string' ? item.model_name : '',
      input_price:
        item?.input_price == null ? '' : String(item.input_price),
      output_price:
        item?.output_price == null ? '' : String(item.output_price),
      cache_input_price:
        item?.cache_input_price == null ? '' : String(item.cache_input_price),
      cache_create_price:
        item?.cache_create_price == null ? '' : String(item.cache_create_price),
      cache_create_price_1h:
        item?.cache_create_price_1h == null
          ? ''
          : String(item.cache_create_price_1h),
      image_output_price:
        item?.image_output_price == null ? '' : String(item.image_output_price),
      enabled: item?.enabled !== false,
      note: typeof item?.note === 'string' ? item.note : '',
      sort_order:
        item?.sort_order == null ? String(index) : String(item.sort_order),
    }))
  } catch {
    return []
  }
}

function parsePrice(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return null
  const parsed = Number(trimmed)
  return Number.isFinite(parsed) ? parsed : null
}

function rowsToPayload(rows: ProviderPriceRow[]) {
  return rows
    .map((row, index) => ({
      id: row.id.trim() || `provider-price-${index + 1}`,
      group_name: row.group_name.trim(),
      model_name: row.model_name.trim(),
      input_price: parsePrice(row.input_price),
      output_price: parsePrice(row.output_price),
      cache_input_price: parsePrice(row.cache_input_price),
      cache_create_price: parsePrice(row.cache_create_price),
      cache_create_price_1h: parsePrice(row.cache_create_price_1h),
      image_output_price: parsePrice(row.image_output_price),
      enabled: row.enabled,
      note: row.note.trim(),
      sort_order: Number.isFinite(Number(row.sort_order))
        ? Number(row.sort_order)
        : index,
    }))
    .filter((row) => row.group_name && row.model_name)
}

function validateRows(rows: ProviderPriceRow[]) {
  for (const row of rows) {
    if (!row.group_name.trim() || !row.model_name.trim()) {
      continue
    }
    const hasAnyPrice = [
      row.input_price,
      row.output_price,
      row.cache_input_price,
      row.cache_create_price,
      row.cache_create_price_1h,
      row.image_output_price,
    ].some((value) => {
      const parsed = parsePrice(value)
      return parsed != null && parsed > 0
    })
    if (!hasAnyPrice) {
      return false
    }
  }
  return true
}

export const ProviderPriceExportSection = memo(
  function ProviderPriceExportSection({
    defaultValue,
  }: ProviderPriceExportSectionProps) {
    const { t } = useTranslation()
    const updateOption = useUpdateOption()
    const [rows, setRows] = useState<ProviderPriceRow[]>([])

    useEffect(() => {
      setRows(parseInitialRows(defaultValue))
    }, [defaultValue])

    const canSave = useMemo(() => validateRows(rows), [rows])

    const updateRow = (
      rowId: string,
      key: keyof ProviderPriceRow,
      value: string | boolean
    ) => {
      setRows((prev) =>
        prev.map((row) => (row.id === rowId ? { ...row, [key]: value } : row))
      )
    }

    const addRow = () => {
      setRows((prev) => [...prev, createRow(prev.length)])
    }

    const removeRow = (rowId: string) => {
      setRows((prev) => prev.filter((row) => row.id !== rowId))
    }

    const save = async () => {
      if (!canSave) {
        return
      }
      const payload = JSON.stringify(rowsToPayload(rows))
      const validation = validateJsonString(payload, {
        type: 'array',
        fallback: '[]',
      })
      if (!validation.valid) {
        return
      }
      await updateOption.mutateAsync({
        key: OPTION_KEY,
        value: payload,
      })
    }

    return (
      <div className='space-y-4'>
        <Alert>
          <AlertDescription className='space-y-1 text-sm'>
            <div>
              {t(
                'This export is only for public price display and does not affect real billing.'
              )}
            </div>
            <div>
              {t(
                'Amounts are exported in CNY per 1M tokens. Other sites can read them from `/api/provider/pricing`.'
              )}
            </div>
          </AlertDescription>
        </Alert>

        <div className='flex flex-wrap items-center gap-2'>
          <Button variant='outline' size='sm' onClick={addRow}>
            <Plus className='mr-2 h-4 w-4' />
            {t('Add')}
          </Button>
          <Button
            size='sm'
            onClick={save}
            disabled={updateOption.isPending || !canSave}
          >
            {updateOption.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </div>

        {!canSave && (
          <div className='text-sm text-red-500'>
            {t(
              'Each configured row must include a group name, model name, and at least one price greater than 0.'
            )}
          </div>
        )}

        <div className='rounded-lg border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Enabled')}</TableHead>
                <TableHead>{t('Group')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('Input price')}</TableHead>
                <TableHead>{t('Output price')}</TableHead>
                <TableHead>{t('Cache read')}</TableHead>
                <TableHead>{t('Cache create')}</TableHead>
                <TableHead>{t('Cache create (1h)')}</TableHead>
                <TableHead>{t('Image output')}</TableHead>
                <TableHead>{t('Note')}</TableHead>
                <TableHead>{t('Sort')}</TableHead>
                <TableHead>{t('Action')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={12} className='text-center text-muted-foreground'>
                    {t('No public provider price rows yet.')}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => (
                  <TableRow key={row.id}>
                    <TableCell>
                      <Switch
                        checked={row.enabled}
                        onCheckedChange={(checked) =>
                          updateRow(row.id, 'enabled', checked)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.group_name}
                        onChange={(e) =>
                          updateRow(row.id, 'group_name', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.model_name}
                        onChange={(e) =>
                          updateRow(row.id, 'model_name', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.input_price}
                        onChange={(e) =>
                          updateRow(row.id, 'input_price', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.output_price}
                        onChange={(e) =>
                          updateRow(row.id, 'output_price', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.cache_input_price}
                        onChange={(e) =>
                          updateRow(row.id, 'cache_input_price', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.cache_create_price}
                        onChange={(e) =>
                          updateRow(row.id, 'cache_create_price', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.cache_create_price_1h}
                        onChange={(e) =>
                          updateRow(row.id, 'cache_create_price_1h', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.image_output_price}
                        onChange={(e) =>
                          updateRow(row.id, 'image_output_price', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.note}
                        onChange={(e) => updateRow(row.id, 'note', e.target.value)}
                      />
                    </TableCell>
                    <TableCell>
                      <Input
                        value={row.sort_order}
                        onChange={(e) =>
                          updateRow(row.id, 'sort_order', e.target.value)
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Button
                        variant='ghost'
                        size='icon'
                        onClick={() => removeRow(row.id)}
                      >
                        <Trash2 className='h-4 w-4' />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>
    )
  }
)
