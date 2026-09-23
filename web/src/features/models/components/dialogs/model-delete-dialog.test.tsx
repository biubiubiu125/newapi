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
import { afterEach, beforeEach, expect, it } from 'vitest'

import i18n, { whenInterfaceLanguageReady } from '@/i18n/config'
import zh from '@/i18n/locales/zh.json'
import { api } from '@/lib/http-client'

import { ModelDeleteDialog } from './model-delete-dialog'

const originalAdapter = api.defaults.adapter

beforeEach(async () => {
  await whenInterfaceLanguageReady
  i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
  await i18n.changeLanguage('zhCN')
})

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

it('does not show a raw network error when deleting a model fails', async () => {
  const adapter: AxiosAdapter = async (config) => {
    throw new AxiosError('Network Error', 'ERR_NETWORK', config)
  }
  api.defaults.adapter = adapter
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <ModelDeleteDialog
        models={[{ id: 1, model_name: 'example', name_rule: 0 }]}
        onClose={() => undefined}
      />
    </QueryClientProvider>
  )

  await userEvent.click(screen.getByRole('button', { name: '删除' }))

  expect(await screen.findByRole('alert')).toHaveTextContent('无法连接服务器')
  await waitFor(() =>
    expect(screen.queryByText('Network Error')).not.toBeInTheDocument()
  )
})
