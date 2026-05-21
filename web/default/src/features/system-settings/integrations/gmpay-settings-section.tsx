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

export interface GMPaySettingsValues {
  GMPayEnabled: boolean
  GMPayBaseURL: string
  GMPayPID: string
  GMPaySecretKey: string
  GMPayCurrency: string
  GMPayDisplayName: string
  GMPayAssetDisplayNames: string
  GMPayMinTopUp: number
}

interface Props {
  defaultValues: GMPaySettingsValues
}

const GMPAY_ASSET_DISPLAY_NAMES =
  '{"gmpay:usdt":"GMPay USDT","gmpay:usdt:tron":"GMPay USDT-TRC20","gmpay:usdt:bsc":"GMPay USDT-BEP20","gmpay:usdt:polygon":"GMPay USDT-Polygon"}'

export function GMPaySettingsSection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [loading, setLoading] = useState(false)
  const form = useForm<GMPaySettingsValues>({
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
        { key: 'GMPayEnabled', value: 'true' },
        { key: 'GMPayBaseURL', value: values.GMPayBaseURL.trim() },
        { key: 'GMPayPID', value: values.GMPayPID.trim() },
        { key: 'GMPayCurrency', value: 'CNY' },
        {
          key: 'GMPayDisplayName',
          value: 'GMPay',
        },
        {
          key: 'GMPayMinTopUp',
          value: String(values.GMPayMinTopUp || 1),
        },
        { key: 'GMPayAssetDisplayNames', value: GMPAY_ASSET_DISPLAY_NAMES },
      ]
      if (values.GMPaySecretKey.trim()) {
        options.push({
          key: 'GMPaySecretKey',
          value: values.GMPaySecretKey.trim(),
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
      title={t('GMPay Gateway')}
      description={t(
        'GMPay payment integration for USDT topup and subscription payment.'
      )}
    >
      <div className='space-y-5'>
        <div className='grid gap-4 md:grid-cols-2'>
          <div className='space-y-2'>
            <Label>{t('GMPay Endpoint')}</Label>
            <Input
              {...form.register('GMPayBaseURL')}
              placeholder='https://pay.example.com'
            />
          </div>
          <div className='space-y-2'>
            <Label>{t('Callback address')}</Label>
            <Input value='/api/user/gmpay/notify' readOnly />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Topup uses /api/user/gmpay/notify and subscription uses /api/subscription/gmpay/notify.'
              )}
            </p>
          </div>
          <div className='space-y-2'>
            <Label>{t('GMPay Merchant ID')}</Label>
            <Input {...form.register('GMPayPID')} />
          </div>
          <div className='space-y-2'>
            <Label>{t('GMPay Secret Key')}</Label>
            <Input
              type='password'
              autoComplete='new-password'
              {...form.register('GMPaySecretKey')}
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
            : t('Save GMPay settings')}
        </Button>
      </div>
    </SettingsSection>
  )
}
