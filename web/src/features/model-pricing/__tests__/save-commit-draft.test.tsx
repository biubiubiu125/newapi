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
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosAdapter } from 'axios'
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { SettingsPageProvider } from '@/features/system-settings/components/settings-page-context'
import { ModelPricingEditorPanel } from '@/features/system-settings/models/model-pricing-sheet'
import { ModelRatioForm } from '@/features/system-settings/models/model-ratio-form'
import { RatioSettingsCard } from '@/features/system-settings/models/ratio-settings-card'
import { api } from '@/lib/api'
import { handleServerError } from '@/lib/handle-server-error'

const clients: QueryClient[] = []
const originalAdapter = api.defaults.adapter

function stubMatchMedia(matchesQuery?: (query: string) => boolean) {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string): MediaQueryList => ({
      matches: matchesQuery ? matchesQuery(query) : false,
      media: query,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }),
  })
}

const EMPTY_MODEL_VALUES = {
  ModelPrice: '{"example-model":0.1}',
  ModelRatio: '{}',
  CacheRatio: '{}',
  CreateCacheRatio: '{}',
  CompletionRatio: '{}',
  ImageRatio: '{}',
  AudioRatio: '{}',
  AudioCompletionRatio: '{}',
  BillingMode: '{}',
  BillingExpr: '{}',
  ExposeRatioEnabled: false,
}

const EMPTY_GROUP_VALUES = {
  GroupRatio: '{}',
  TopupGroupRatio: '{}',
  UserUsableGroups: '{}',
  GroupGroupRatio: '{}',
  AutoGroups: '[]',
  MaxTokenAutoGroups: 1,
  DefaultUseAutoGroup: false,
  GroupSpecialUsableGroup: '{}',
}

afterEach(() => {
  cleanup()
  stubMatchMedia()
  api.defaults.adapter = originalAdapter
  for (const client of clients) client.clear()
  clients.length = 0
  vi.restoreAllMocks()
})

function renderWithClient(ui: ReactNode) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url === '/api/channel/models_enabled' ? ['example-model'] : [],
      vendors: [],
    },
  }))
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: {
        retry: false,
        onError: handleServerError,
      },
    },
  })
  clients.push(client)
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

function PricingFormFixture(props: {
  onSave: (values?: unknown) => Promise<void>
  values?: Partial<typeof EMPTY_MODEL_VALUES>
}) {
  const values = { ...EMPTY_MODEL_VALUES, ...props.values }
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const form = useForm({ defaultValues: values })
  return (
    <>
      <header>
        <div ref={setActionsContainer} />
      </header>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <ModelRatioForm
          form={form}
          savedValues={values}
          onSave={props.onSave}
          onReset={() => undefined}
          isSaving={false}
          isResetting={false}
        />
      </SettingsPageProvider>
    </>
  )
}

function optionPuts() {
  return vi
    .mocked(api.put)
    .mock.calls.filter(([url]) => url === '/api/option/')
    .map(([, body]) => body as { key: string; value: string })
}

async function editExamplePrice(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const price = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
}

async function clickAddModel(user: ReturnType<typeof userEvent.setup>) {
  const buttons = await screen.findAllByRole('button', { name: 'Add model' })
  await user.click(buttons[0])
}

it('does not pass the click event to the editor save handler', async () => {
  const onSave = vi.fn()
  renderWithClient(
    <ModelPricingEditorPanel
      embedded
      editData={{
        name: 'example-model',
        billingMode: 'per-request',
        price: '0.1',
      }}
      onSave={onSave}
    />
  )
  await userEvent.click(
    screen.getByRole('button', { name: 'Save model prices' })
  )
  expect(onSave).toHaveBeenCalledOnce()
  expect(onSave.mock.calls[0]).toEqual([])
})

it('commits the editor draft instead of the click event when saving model prices', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await editExamplePrice(user)

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  const [values] = save.mock.calls[0]
  expect(values).not.toHaveProperty('nativeEvent')
  expect(JSON.parse(values.ModelPrice)).toEqual({ 'example-model': 0.25 })
  expect(Object.keys(JSON.parse(values.ModelPrice))).not.toContain('undefined')
  expect(screen.getByRole('textbox', { name: 'Model name' })).toHaveValue(
    'example-model'
  )
})

it('PUTs the committed ModelPrice JSON through /api/option', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, message: '' },
  } as never)
  renderWithClient(
    <RatioSettingsCard
      modelDefaults={EMPTY_MODEL_VALUES}
      groupDefaults={EMPTY_GROUP_VALUES}
      toolPricesDefault='{}'
      visibleTabs={['models']}
    />
  )

  await editExamplePrice(user)

  await waitFor(() => expect(optionPuts().length).toBeGreaterThan(0))
  const modelPrice = optionPuts().find((body) => body.key === 'ModelPrice')
  expect(modelPrice).toEqual({
    key: 'ModelPrice',
    value: expect.any(String),
  })
  expect(JSON.parse(String(modelPrice?.value))).toEqual({
    'example-model': 0.25,
  })
  expect(Object.keys(JSON.parse(String(modelPrice?.value)))).not.toContain(
    'undefined'
  )
})

it('keeps model prices dirty when PUT returns success:false', async () => {
  const user = userEvent.setup()
  const puts: { key: string; value: string }[] = []
  const error = vi.spyOn(toast, 'error')
  const info = vi.spyOn(toast, 'info')
  const adapter: AxiosAdapter = async (config) => {
    if (config.method === 'put' && config.url === '/api/option/') {
      const body =
        typeof config.data === 'string' ? JSON.parse(config.data) : config.data
      puts.push(body)
      return {
        data: { success: false, message: 'model_price rejected' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  renderWithClient(
    <RatioSettingsCard
      modelDefaults={EMPTY_MODEL_VALUES}
      groupDefaults={EMPTY_GROUP_VALUES}
      toolPricesDefault='{}'
      visibleTabs={['models']}
    />
  )

  await editExamplePrice(user)
  await waitFor(() => expect(puts).toHaveLength(1))
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'model_price rejected',
    ])
  )

  const save = screen.getByRole('button', { name: 'Save model prices' })
  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)

  await waitFor(() => expect(puts).toHaveLength(2))
  expect(error.mock.calls.map(([text]) => text)).toEqual([
    'model_price rejected',
    'model_price rejected',
  ])
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
  expect(JSON.parse(puts[1].value)).toEqual({
    'example-model': 0.25,
  })
})

it('does not wipe sibling maps when current pricing JSON is invalid', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  const error = vi.spyOn(toast, 'error')
  renderWithClient(
    <PricingFormFixture onSave={save} values={{ ModelRatio: '{not-json' }} />
  )

  await editExamplePrice(user)

  await waitFor(() => expect(error).toHaveBeenCalled())
  expect(save).not.toHaveBeenCalled()
  expect(error.mock.calls[0]?.[0]).toEqual('Invalid JSON format')
})

it('rejects a whitespace-only model name instead of saving', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await clickAddModel(user)
  await user.type(screen.getByRole('textbox', { name: 'Model name' }), '   ')
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.type(screen.getByRole('textbox', { name: 'Fixed price' }), '0.5')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  expect(save).not.toHaveBeenCalled()
  expect(await screen.findByText('Model name is required')).toBeInTheDocument()
})

it('saves a newly added model name into ModelPrice', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await clickAddModel(user)
  await user.type(
    screen.getByRole('textbox', { name: 'Model name' }),
    'new-model'
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.type(screen.getByRole('textbox', { name: 'Fixed price' }), '0.5')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.1,
    'new-model': 0.5,
  })
})

it('does not wipe sibling models when deleting while a pricing map is invalid JSON', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  const error = vi.spyOn(toast, 'error')
  renderWithClient(
    <PricingFormFixture
      onSave={save}
      values={{
        ModelPrice: '{"keep-me":0.1,"delete-me":0.2}',
        ModelRatio: '{not-json',
      }}
    />
  )

  await screen.findByText('keep-me')
  const row =
    screen.getByText('delete-me').closest('tr') ??
    screen.getByText('delete-me').closest('[role="row"]')
  expect(row).not.toBeNull()
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Open menu' })
  )
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))

  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toContain(
      'Invalid JSON format'
    )
  )
  expect(screen.getByText('delete-me')).toBeInTheDocument()
  expect(screen.getByText('keep-me')).toBeInTheDocument()
  expect(save).not.toHaveBeenCalled()
})

it('saves from the single mobile editor without mounting a second copy', async () => {
  stubMatchMedia((query) => query.includes('max-width: 767px'))
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await clickAddModel(user)
  expect(
    screen.getAllByRole('button', {
      name: 'Save model prices',
      hidden: true,
    })
  ).toHaveLength(1)

  await user.type(await screen.findByLabelText('Model name'), 'mobile-model')
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.type(screen.getByRole('textbox', { name: 'Fixed price' }), '0.5')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.1,
    'mobile-model': 0.5,
  })
})
