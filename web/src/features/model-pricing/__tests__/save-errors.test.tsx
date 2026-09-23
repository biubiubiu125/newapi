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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosAdapter } from 'axios'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { handleServerError } from '@/lib/handle-server-error'
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
let client: QueryClient | undefined

afterEach(() => {
  unsavedModelPricingDrafts.clear()
  client?.clear()
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.setUser(null)
  localStorage.clear()
  vi.restoreAllMocks()
})

it.each([200, 400])(
  'shows the backend pricing error once, preserves the draft, and reports a new attempt separately (HTTP %i)',
  async (status) => {
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
    const message = 'model_pricing: expression validation failed (1:18)'
    const notify = vi.spyOn(toast, 'error').mockReturnValue('pricing-error')
    const requests: unknown[] = []
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
      if (
        config.method === 'get' &&
        config.url === '/api/option/model_pricing'
      ) {
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
        requests.push(config.data)
        const response = {
          data: { success: false, message },
          status,
          statusText: 'Error',
          headers: {},
          config,
        }
        if (status === 400) {
          throw new AxiosError(
            'Request failed with status code 400',
            'ERR_BAD_REQUEST',
            config,
            undefined,
            response
          )
        }
        return response
      }
      throw new Error(`Unexpected request: ${config.method} ${config.url}`)
    }
    api.defaults.adapter = adapter
    client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: {
          retry: false,
          onError: handleServerError,
        },
      },
    })
    render(
      <QueryClientProvider client={client}>
        <ModelPricingPanel modelName='example' />
      </QueryClientProvider>
    )
    const user = userEvent.setup()
    const save = await screen.findByRole('button', {
      name: 'Save model prices',
    })
    await user.click(screen.getByRole('tab', { name: 'Per-request' }))
    const price = screen.getByRole('textbox', { name: 'Fixed price' })
    await user.clear(price)
    await user.type(price, '0.25')
    await user.click(save)
    await waitFor(() =>
      expect(notify.mock.calls.map(([text]) => text)).toEqual([message])
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
      '0.25'
    )
    await waitFor(() => expect(save).toBeEnabled())
    await user.click(save)
    await waitFor(() =>
      expect(notify.mock.calls.map(([text]) => text)).toEqual([
        message,
        message,
      ])
    )
    expect(requests).toHaveLength(2)
    expect(String(requests[0])).toContain('"ModelPrice":0.25')
    expect(String(requests[0])).not.toMatch(/nativeEvent|SyntheticBaseEvent/)
  }
)

it('toasts when highlighted pricing fields are invalid before saving', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, role: 100, status: 1, username: 'root' })
  const snapshot: ModelPricingConfig = {
    entries: [
      {
        model_name: 'example',
        version: 'v1',
        configured: { ModelRatio: 1 },
        effective: { ModelRatio: 1 },
      },
    ],
    options: pricingOptions({ ModelRatio: '{"example":1}' }),
    empty_version: 'empty',
  }
  api.defaults.adapter = (async (config) => {
    const url = String(config.url ?? '')
    if (
      config.method === 'get' &&
      ['/api/status', '/api/pricing'].includes(url)
    ) {
      const data =
        url === '/api/status'
          ? { success: true, data: {} }
          : { success: true, data: [], vendors: [] }
      return { data, status: 200, statusText: 'OK', headers: {}, config }
    }
    if (config.method === 'get' && url.includes('/api/option/model_pricing')) {
      return {
        data: { success: true, data: snapshot },
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'application/json' },
        config,
      }
    }
    throw new Error(`unexpected ${config.method} ${url}`)
  }) as AxiosAdapter
  const notify = vi.spyOn(toast, 'error')
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelPricingPanel modelName='example' />
    </QueryClientProvider>
  )
  const user = userEvent.setup()
  await user.click(await screen.findByRole('tab', { name: 'Per-token' }))
  const inputPrice = screen.getByPlaceholderText('3')
  await user.clear(inputPrice)
  await user.type(inputPrice, '0')
  await user.click(screen.getAllByRole('switch')[0])
  await user.type(screen.getByPlaceholderText('15'), '15')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() =>
    expect(notify.mock.calls.map(([text]) => text)).toEqual([
      'Please fix the highlighted fields before saving',
    ])
  )
})

it('keeps an unsaved pricing draft after the panel unmounts', async () => {
  useAuthStore
    .getState()
    .auth.setUser({ id: 1, role: 100, status: 1, username: 'root' })
  const snapshot: ModelPricingConfig = {
    entries: [
      {
        model_name: 'example',
        version: 'v1',
        configured: { ModelPrice: 0.1 },
        effective: { ModelPrice: 0.1 },
      },
    ],
    options: pricingOptions({ ModelPrice: '{"example":0.1}' }),
    empty_version: 'empty',
  }
  api.defaults.adapter = (async (config) => {
    const url = String(config.url ?? '')
    if (config.method === 'get' && url.includes('/api/option/model_pricing')) {
      return {
        data: { success: true, data: snapshot },
        status: 200,
        statusText: 'OK',
        headers: { 'content-type': 'application/json' },
        config,
      }
    }
    throw new Error(`unexpected ${config.method} ${url}`)
  }) as AxiosAdapter
  client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={client}>
      <ModelPricingPanel modelName='example' />
    </QueryClientProvider>
  )
  const price = await screen.findByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')
  view.unmount()
  render(
    <QueryClientProvider client={client}>
      <ModelPricingPanel modelName='example' />
    </QueryClientProvider>
  )
  expect(
    await screen.findByRole('textbox', { name: 'Fixed price' })
  ).toHaveValue('0.25')
})
