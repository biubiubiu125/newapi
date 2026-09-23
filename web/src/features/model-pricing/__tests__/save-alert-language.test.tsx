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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosAdapter } from 'axios'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import i18n, { whenInterfaceLanguageReady } from '@/i18n/config'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/http-client'
import { useAuthStore } from '@/stores/auth-store'
import { usePricingPreferencesStore } from '@/stores/pricing-preferences-store'

import type { ModelPricingConfig } from '../api'
import {
  ModelPricingPanel,
  unsavedModelPricingDrafts,
} from '../model-pricing-panel'
import { pricingOptions } from '../pricing'

const originalAdapter = api.defaults.adapter

beforeEach(async () => {
  await whenInterfaceLanguageReady
  i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  await i18n.changeLanguage('zhCN')
})

afterEach(() => {
  unsavedModelPricingDrafts.clear()
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.setUser(null)
  vi.restoreAllMocks()
})

it('does not show a raw network error when saving model pricing fails', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, username: 'administrator', role: 100 })
  usePricingPreferencesStore.setState({ currency: 'USD' })
  const values = {
    'billing_setting.billing_mode': 'tiered_expr',
    'billing_setting.billing_expr': 'tier("standard", p * 1 + c * 2)',
  }
  const snapshot: ModelPricingConfig = {
    entries: [
      {
        model_name: 'example',
        version: 'v1',
        configured: values,
        effective: values,
      },
    ],
    options: pricingOptions({}),
    empty_version: 'empty',
  }
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'get' &&
      ['/api/status', '/api/pricing'].includes(config.url ?? '')
    ) {
      const data =
        config.url === '/api/status'
          ? { success: true, data: {} }
          : { success: true, data: [], vendors: [] }
      return { data, status: 200, statusText: 'OK', headers: {}, config }
    }
    if (config.method === 'get' && config.url === '/api/option/model_pricing') {
      return {
        data: { success: true, data: snapshot },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      throw new AxiosError('Network Error', 'ERR_NETWORK', config)
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelPricingPanel modelName='example' />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(await screen.findByRole('tab', { name: '按次' }))
  const price = screen.getByRole('textbox', { name: '固定价格' })
  await user.clear(price)
  await user.type(price, '0.25')
  await user.click(screen.getByRole('button', { name: '保存模型价格' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('无法连接服务器')
  expect(screen.queryByText('Network Error')).not.toBeInTheDocument()
})
