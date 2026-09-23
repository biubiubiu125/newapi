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
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ModelMutateDrawer } from '../components/drawers/model-mutate-drawer'
import type { Model } from '../types'

const keepMe: Model = {
  id: 7,
  model_name: 'keep-me',
  description: 'old description',
  icon: '',
  tags: '',
  endpoints: '',
  name_rule: 0,
  status: 1,
  sync_official: 1,
  created_time: 1,
  updated_time: 1,
  has_metadata: true,
  configured_channel_count: 0,
}

const clients: QueryClient[] = []
const originalAdapter = api.defaults.adapter

function optionPayload() {
  return [
    { key: 'ModelPrice', value: '{"keep-me":0.1}' },
    { key: 'ModelRatio', value: '{}' },
    { key: 'CacheRatio', value: '{}' },
    { key: 'CompletionRatio', value: '{}' },
    { key: 'ImageRatio', value: '{}' },
    { key: 'AudioRatio', value: '{}' },
    { key: 'AudioCompletionRatio', value: '{}' },
    { key: 'CreateCacheRatio', value: '{}' },
    { key: 'billing_setting.billing_mode', value: '{"keep-me":"tiered_expr"}' },
    {
      key: 'billing_setting.billing_expr',
      value: '{"keep-me":"tier(\\"standard\\", p * 1)"}',
    },
  ]
}

function renderDrawer() {
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'admin',
    role: 100,
  })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/models/7') {
      return { data: { success: true, data: keepMe } }
    }
    if (url === '/api/vendors/') {
      return { data: { success: true, data: { items: [] } } }
    }
    if (url === '/api/option/') {
      return { data: { success: true, data: optionPayload() } }
    }
    if (String(url).startsWith('/api/option/model_pricing')) {
      return {
        data: {
          success: true,
          data: {
            entries: [],
            options: {},
            empty_version: 'empty',
          },
        },
      }
    }
    return { data: { success: true, data: [] } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <ModelMutateDrawer
        open
        onOpenChange={() => undefined}
        currentRow={keepMe}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  clients.splice(0).forEach((client) => client.clear())
  api.defaults.adapter = originalAdapter
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

it('saves metadata without rewriting pricing option maps', async () => {
  const user = userEvent.setup()
  const puts: Array<{ url: string; data: unknown }> = []
  vi.spyOn(api, 'put').mockImplementation(async (url, data) => {
    puts.push({ url: String(url), data })
    return { data: { success: true } }
  })
  renderDrawer()

  await screen.findByDisplayValue('keep-me')
  expect(screen.queryByText('Pricing Configuration')).not.toBeInTheDocument()
  expect(screen.queryByText('Pricing mode')).not.toBeInTheDocument()

  const name = screen.getByLabelText('Model Name *')
  await user.clear(name)
  await user.type(name, 'keep-me-renamed')
  await user.click(screen.getByRole('button', { name: 'Update Model' }))

  await waitFor(() => {
    expect(puts.some((item) => item.url === '/api/models/')).toBe(true)
  })
  expect(
    puts.filter((item) => item.url === '/api/option/').map((item) => item.data)
  ).toEqual([])
})

it('does not keep a metadata save button on the pricing tab', async () => {
  const user = userEvent.setup()
  renderDrawer()
  await screen.findByDisplayValue('keep-me')
  await user.click(screen.getByRole('tab', { name: 'Pricing' }))
  expect(
    screen.queryByRole('button', { name: 'Update Model' })
  ).not.toBeInTheDocument()
})
