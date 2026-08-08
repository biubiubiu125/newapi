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

import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

export interface BEpusdtSettingsValues {
  BEpusdtEnabled: boolean
  USDTGatewayType: string
  BEpusdtBaseURL: string
  BEpusdtPID: string
  BEpusdtSecretKey: string
  BEpusdtCurrency: string
  BEpusdtDisplayName: string
  BEpusdtAssetDisplayNames: string
  BEpusdtMinTopUp: number
}

interface Props {
  defaultValues: BEpusdtSettingsValues
}

export function BEpusdtSettingsSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [loading, setLoading] = useState(false)
  const form = useForm<BEpusdtSettingsValues>({
    defaultValues: props.defaultValues,
  })

  useEffect(() => {
    form.reset(props.defaultValues)
  }, [form, props.defaultValues])

  const handleSave = async () => {
    const values = form.getValues()

    setLoading(true)
    try {
      const options: { key: string; value: string }[] = [
        { key: 'BEpusdtEnabled', value: 'true' },
        { key: 'USDTGatewayType', value: 'bepusdt' },
        { key: 'BEpusdtBaseURL', value: values.BEpusdtBaseURL.trim() },
        { key: 'BEpusdtPID', value: '' },
        { key: 'BEpusdtCurrency', value: 'CNY' },
        {
          key: 'BEpusdtMinTopUp',
          value: String(values.BEpusdtMinTopUp || 1),
        },
      ]

      if (values.BEpusdtSecretKey.trim()) {
        options.push({
          key: 'BEpusdtSecretKey',
          value: values.BEpusdtSecretKey.trim(),
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
    <SettingsSection title={t('USDT Gateway')}>
      <div className='space-y-5'>
        <div>
          <h3 className='text-lg font-medium'>{t('USDT Gateway')}</h3>
          <p className='text-muted-foreground text-sm'>
            {t('BEpusdt USDT top-up and subscription payment integration.')}
          </p>
        </div>

        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('Gateway endpoint')}</Label>
            <Input
              {...form.register('BEpusdtBaseURL')}
              placeholder='https://pay.example.com'
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Callback address')}</Label>
            <Input value='/api/user/bepusdt/notify' readOnly />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Set the public site URL in server address or callback override. Top-up callback: /api/user/bepusdt/notify. Subscription callback: /api/subscription/bepusdt/notify.'
              )}
            </p>
          </div>
          <div className='space-y-2'>
            <Label>{t('Secret Key')}</Label>
            <Input
              type='password'
              autoComplete='new-password'
              {...form.register('BEpusdtSecretKey')}
              placeholder={t('Leave blank unless updating')}
            />
          </div>
        </div>

        <Button
          type='button'
          onClick={handleSave}
          disabled={loading || updateOption.isPending}
        >
          {loading || updateOption.isPending
            ? t('Saving...')
            : t('Save USDT gateway settings')}
        </Button>
      </div>
    </SettingsSection>
  )
}
