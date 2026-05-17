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
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { ProviderPriceExportSection } from '@/features/system-settings/billing/provider-price-export-section'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'

const defaultProviderPriceExportSettings = {
  ProviderPriceOverrides: '[]',
}

export function ProviderPriceExport() {
  const { t } = useTranslation()
  const { data, isLoading } = useSystemOptions()

  const settings = getOptionValue(
    data?.data,
    defaultProviderPriceExportSettings
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Public Price Export')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Configure a display-only public pricing feed for external sites'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        {isLoading ? (
          <div className='text-muted-foreground py-12 text-center'>
            {t('Loading settings...')}
          </div>
        ) : (
          <ProviderPriceExportSection
            defaultValue={settings.ProviderPriceOverrides}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
