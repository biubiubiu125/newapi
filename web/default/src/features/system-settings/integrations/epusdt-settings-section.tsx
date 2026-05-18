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
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

export interface EpusdtSettingsValues {
  EpusdtEnabled: boolean
  EpusdtBaseURL: string
  EpusdtPID: string
  EpusdtSecretKey: string
  EpusdtCurrency: string
  EpusdtDisplayName: string
  EpusdtAssetDisplayNames: string
  EpusdtMinTopUp: number
}

interface Props {
  defaultValues: EpusdtSettingsValues
}

export function EpusdtSettingsSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [loading, setLoading] = useState(false)
  const form = useForm<EpusdtSettingsValues>({
    defaultValues: props.defaultValues,
  })

  useEffect(() => {
    form.reset(props.defaultValues)
  }, [form, props.defaultValues])

  const handleSave = async () => {
    const values = form.getValues()
    let displayNames = values.EpusdtAssetDisplayNames || '{}'
    try {
      JSON.parse(displayNames)
    } catch {
      toast.error(t('Asset display names must be valid JSON'))
      return
    }

    setLoading(true)
    try {
      const options: { key: string; value: string }[] = [
        { key: 'EpusdtEnabled', value: String(values.EpusdtEnabled) },
        { key: 'EpusdtBaseURL', value: values.EpusdtBaseURL.trim() },
        { key: 'EpusdtPID', value: values.EpusdtPID.trim() },
        { key: 'EpusdtCurrency', value: 'CNY' },
        {
          key: 'EpusdtDisplayName',
          value: values.EpusdtDisplayName.trim() || 'USDT',
        },
        {
          key: 'EpusdtMinTopUp',
          value: String(values.EpusdtMinTopUp || 1),
        },
        { key: 'EpusdtAssetDisplayNames', value: displayNames.trim() },
      ]
      if (values.EpusdtSecretKey.trim()) {
        options.push({
          key: 'EpusdtSecretKey',
          value: values.EpusdtSecretKey.trim(),
        })
      }
      for (const opt of options) {
        await updateOption.mutateAsync(opt)
      }
      toast.success(t('Updated successfully'))
    } catch {
      toast.error(t('Update failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <SettingsSection
      title={t('Epusdt USDT Payment')}
      description={t(
        'Connect an external Epusdt deployment for USDT topup and subscription payment.'
      )}
    >
      <div className='space-y-5'>
        <div className='flex items-center justify-between rounded-lg border p-4'>
          <div>
            <Label>{t('Enable Epusdt')}</Label>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t('Available chains are read from Epusdt GMPay config.')}
            </p>
          </div>
          <Switch
            checked={form.watch('EpusdtEnabled')}
            onCheckedChange={(checked) =>
              form.setValue('EpusdtEnabled', checked)
            }
          />
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('Epusdt Base URL')}</Label>
            <Input
              {...form.register('EpusdtBaseURL')}
              placeholder='https://pay.example.com'
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Epusdt PID')}</Label>
            <Input {...form.register('EpusdtPID')} />
          </div>
          <div className='space-y-2'>
            <Label>{t('Epusdt Secret Key')}</Label>
            <Input
              type='password'
              autoComplete='new-password'
              {...form.register('EpusdtSecretKey')}
              placeholder={t('Leave blank unless updating')}
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Order Pricing Currency')}</Label>
            <Input value='CNY' readOnly />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Currency for the fiat order amount sent to Epusdt, usually CNY. USDT amount is calculated by Epusdt from token and network.'
              )}
            </p>
          </div>
          <div className='space-y-2'>
            <Label>{t('Display Name')}</Label>
            <Input {...form.register('EpusdtDisplayName')} placeholder='USDT' />
          </div>
          <div className='space-y-2'>
            <Label>{t('Minimum Topup')}</Label>
            <Input type='number' min={1} {...form.register('EpusdtMinTopUp')} />
          </div>
        </div>

        <Separator />

        <div className='space-y-2'>
          <Label>{t('Chain Display Names')}</Label>
          <Textarea
            className='min-h-28 font-mono text-xs'
            {...form.register('EpusdtAssetDisplayNames')}
            placeholder='{"epusdt:usdt":"USDT"}'
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Map epusdt payment method keys to user-facing names, for example epusdt:usdt -> USDT.'
            )}
          </p>
        </div>

        <Button
          type='button'
          onClick={handleSave}
          disabled={loading || updateOption.isPending}
        >
          {loading || updateOption.isPending
            ? t('Saving...')
            : t('Save Epusdt settings')}
        </Button>
      </div>
    </SettingsSection>
  )
}
