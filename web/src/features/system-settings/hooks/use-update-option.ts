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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'

import { DEFAULT_LOGO } from '@/lib/constants'
import { emitSettingsRefresh } from '@/lib/settings-refresh'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { updateSystemOption } from '../api'
import type { UpdateOptionRequest } from '../types'

type UpdateOptionMutationRequest = UpdateOptionRequest & {
  skipInvalidate?: boolean
  skipToast?: boolean
}

// Configuration keys that require status refresh
const STATUS_RELATED_KEYS = new Set([
  'theme.frontend',
  'SystemName',
  'ServerAddress',
  'Logo',
  'Footer',
  'HeaderNavModules',
  'SidebarModulesAdmin',
  'LogConsumeEnabled',
  'Price',
  'QuotaPerUnit',
  'USDExchangeRate',
  'DisplayInCurrencyEnabled',
  'DisplayTokenStatEnabled',
  'console_setting.announcements',
  'console_setting.announcements_enabled',
  'general_setting.quota_display_type',
  'general_setting.custom_currency_symbol',
  'general_setting.custom_currency_exchange_rate',
])

const NOTICE_RELATED_KEYS = new Set(['Notice'])
const WALLET_RELATED_KEYS = ['TopUpLink', 'payment_setting.wallet_notice']

function syncDisplayOptionToSystemConfig(request: UpdateOptionMutationRequest) {
  const value = String(request.value ?? '')
  const { setConfig, setLoadedLogoUrl } = useSystemConfigStore.getState()

  switch (request.key) {
    case 'SystemName':
      setConfig({ systemName: value })
      break
    case 'Logo':
      setConfig({ logo: value || DEFAULT_LOGO })
      setLoadedLogoUrl('')
      break
    case 'ServerAddress':
      setConfig({ serverAddress: value.replace(/\/+$/, '') })
      break
    case 'Footer':
      setConfig({ footerHtml: value })
      break
    default:
      break
  }
}

export function useUpdateOption() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: UpdateOptionMutationRequest) =>
      updateSystemOption({ key: request.key, value: request.value }),
    onSuccess: (data, variables) => {
      if (data.success) {
        // Always refresh system-options
        if (!variables.skipInvalidate) {
          queryClient.invalidateQueries({ queryKey: ['system-options'] })
        }

        // Notice is loaded from /api/notice, not /api/status.
        if (NOTICE_RELATED_KEYS.has(variables.key)) {
          queryClient.invalidateQueries({ queryKey: ['notice'] })
          emitSettingsRefresh([variables.key])
        }

        if (WALLET_RELATED_KEYS.includes(variables.key)) {
          emitSettingsRefresh([variables.key])
        }

        // If updating frontend-display-related config, also refresh status
        if (STATUS_RELATED_KEYS.has(variables.key)) {
          syncDisplayOptionToSystemConfig(variables)
          queryClient.invalidateQueries({ queryKey: ['status'] })
          try {
            window.localStorage.removeItem('status')
          } catch {
            /* empty */
          }
          emitSettingsRefresh([variables.key])
        }

        if (!variables.skipToast) {
          toast.success(i18next.t('Setting updated successfully'))
        }
      } else {
        toast.error(data.message || i18next.t('Failed to update setting'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to update setting'))
    },
  })
}
