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
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, type AxiosAdapter } from 'axios'
import { useState, type ReactNode } from 'react'
import { useForm } from 'react-hook-form'
import { toast } from 'sonner'
import { afterEach, expect, it, vi } from 'vitest'

import { SettingsPageProvider } from '@/features/system-settings/components/settings-page-context'
import { ModelPricingEditorPanel } from '@/features/system-settings/models/model-pricing-sheet'
import { ModelRatioForm } from '@/features/system-settings/models/model-ratio-form'
import { RatioSettingsCard } from '@/features/system-settings/models/ratio-settings-card'
import { api } from '@/lib/api'
import * as authSession from '@/lib/auth-session'
import { handleServerError } from '@/lib/handle-server-error'

const clients: QueryClient[] = []
const originalAdapter = api.defaults.adapter

function stubMatchMedia(matchesQuery?: (query: string) => boolean) {
  const listeners = new Set<() => void>()
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: (query: string): MediaQueryList => ({
      matches: matchesQuery ? matchesQuery(query) : false,
      media: query,
      onchange: null,
      addListener: (listener) => {
        listeners.add(listener)
      },
      removeListener: (listener) => {
        listeners.delete(listener)
      },
      addEventListener: (_event, listener) => {
        listeners.add(listener as () => void)
      },
      removeEventListener: (_event, listener) => {
        listeners.delete(listener as () => void)
      },
      dispatchEvent: () => false,
    }),
  })
  return {
    notify() {
      for (const listener of listeners) listener()
    },
  }
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
  savedValues?: Partial<typeof EMPTY_MODEL_VALUES>
}) {
  const values = { ...EMPTY_MODEL_VALUES, ...props.values }
  const savedValues = {
    ...EMPTY_MODEL_VALUES,
    ...(props.savedValues ?? props.values),
  }
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
          savedValues={savedValues}
          onSave={props.onSave}
          onReset={() => undefined}
          isSaving={false}
          isResetting={false}
        />
      </SettingsPageProvider>
    </>
  )
}

function GroupsFormFixture() {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  return (
    <>
      <header>
        <div ref={setActionsContainer} />
      </header>
      <SettingsPageProvider actionsContainer={actionsContainer}>
        <RatioSettingsCard
          modelDefaults={EMPTY_MODEL_VALUES}
          groupDefaults={EMPTY_GROUP_VALUES}
          toolPricesDefault='{}'
          visibleTabs={['groups']}
        />
      </SettingsPageProvider>
    </>
  )
}

type ModelDefaults = typeof EMPTY_MODEL_VALUES
type GroupDefaults = typeof EMPTY_GROUP_VALUES
type DefaultsSetter<T> = {
  set: (next: T | ((prev: T) => T)) => void
}

const TOKEN_MODEL_VALUES: ModelDefaults = {
  ...EMPTY_MODEL_VALUES,
  ModelPrice: '{}',
  ModelRatio: '{"example-model":1}',
}

const TWO_MODEL_VALUES: ModelDefaults = {
  ...EMPTY_MODEL_VALUES,
  ModelPrice: '{"keep-me":0.1,"other-model":0.2}',
}

function LiveRatioCard(props: {
  initialModel?: ModelDefaults
  initialGroup?: GroupDefaults
  visibleTabs?: Array<'models' | 'unset-models' | 'groups'>
  modelCtl: DefaultsSetter<ModelDefaults>
  groupCtl?: DefaultsSetter<GroupDefaults>
}) {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const [titleStatusContainer, setTitleStatusContainer] =
    useState<HTMLSpanElement | null>(null)
  const [modelDefaults, setModelDefaults] = useState(
    props.initialModel ?? EMPTY_MODEL_VALUES
  )
  const [groupDefaults, setGroupDefaults] = useState(
    props.initialGroup ?? EMPTY_GROUP_VALUES
  )
  props.modelCtl.set = setModelDefaults
  if (props.groupCtl) props.groupCtl.set = setGroupDefaults
  return (
    <>
      <header>
        <div ref={setActionsContainer} />
        <span ref={setTitleStatusContainer} />
      </header>
      <SettingsPageProvider
        actionsContainer={actionsContainer}
        titleStatusContainer={titleStatusContainer}
      >
        <RatioSettingsCard
          modelDefaults={modelDefaults}
          groupDefaults={groupDefaults}
          toolPricesDefault='{}'
          visibleTabs={props.visibleTabs ?? ['models']}
        />
      </SettingsPageProvider>
    </>
  )
}

async function editRowFixedPrice(
  user: ReturnType<typeof userEvent.setup>,
  rowName: string,
  price: string
) {
  const label = await screen.findByText(rowName)
  const row = label.closest('tr') ?? label.closest('[role="row"]')
  expect(row).not.toBeNull()
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const input = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(input)
  await user.type(input, price)
}

function parseRequestData(data: unknown) {
  return typeof data === 'string' ? JSON.parse(data) : data
}

function optionPuts() {
  const mocked = vi.mocked(api.put)
  return (mocked.mock?.calls ?? [])
    .filter(([url]) => url === '/api/option/')
    .map(([, body]) => body as { key: string; value: string })
}

function modelPricingPatches() {
  return vi
    .mocked(api.patch)
    .mock.calls.filter(([url]) => url === '/api/option/model_pricing')
    .map(
      ([, body]) =>
        parseRequestData(body) as {
          options?: Record<string, string>
          expected_options?: Record<string, string>
        }
    )
}

function setJsonEditorValue(name: string, value: string) {
  fireEvent.input(screen.getByRole('textbox', { name }), {
    target: { value },
  })
}

function jsonEditorValue(name: string) {
  return JSON.parse(
    (screen.getByRole('textbox', { name }) as HTMLTextAreaElement).value
  )
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

it('PATCHes the committed ModelPrice JSON through /api/option/model_pricing', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'put')
  vi.spyOn(api, 'patch').mockResolvedValue({
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

  await waitFor(() => expect(modelPricingPatches()).toHaveLength(1))
  const options = modelPricingPatches()[0]?.options ?? {}
  expect(JSON.parse(String(options.ModelPrice))).toEqual({
    'example-model': 0.25,
  })
  expect(Object.keys(JSON.parse(String(options.ModelPrice)))).not.toContain(
    'undefined'
  )
  expect(
    JSON.parse(String(modelPricingPatches()[0]?.expected_options?.ModelPrice))
  ).toEqual({ 'example-model': 0.1 })
  const billingMode = options['billing_setting.billing_mode']
  if (billingMode !== undefined) {
    expect(JSON.parse(String(billingMode))).toEqual({
      'example-model': 'ratio',
    })
  }
  expect(optionPuts()).toEqual([])
})

it('keeps stored ratio billing modes when saving another model price', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture
      onSave={save}
      values={{
        ...EMPTY_MODEL_VALUES,
        ModelPrice: '{"keep":0.1,"example-model":0.1}',
        BillingMode: '{"keep":"ratio"}',
      }}
    />
  )

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].BillingMode)).toMatchObject({
    keep: 'ratio',
    'example-model': 'ratio',
  })
})

it('retries only Expose after pricing PATCH succeeds and Expose PUT fails', async () => {
  const user = userEvent.setup()
  const patches: Array<{
    options?: Record<string, string>
    expected_options?: Record<string, string>
  }> = []
  const puts: Array<{ key: string; value: unknown }> = []
  const error = vi.spyOn(toast, 'error')
  const success = vi.spyOn(toast, 'success')
  const info = vi.spyOn(toast, 'info')
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      patches.push(parseRequestData(config.data))
      return {
        data: { success: true, message: '' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    if (config.method === 'put' && config.url === '/api/option/') {
      const body = parseRequestData(config.data) as {
        key: string
        value: unknown
      }
      puts.push(body)
      if (body.key === 'ExposeRatioEnabled') {
        return {
          data: { success: false, message: 'expose rejected' },
          status: 200,
          statusText: 'OK',
          headers: {},
          config,
        }
      }
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard initialModel={EMPTY_MODEL_VALUES} modelCtl={modelCtl} />
  )

  const expose = await screen.findByRole('switch', {
    name: 'Expose ratio API',
  })
  await user.click(expose)
  expect(expose).toBeChecked()
  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(patches).toHaveLength(1))
  await waitFor(() => expect(puts).toHaveLength(1))
  expect(JSON.parse(String(patches[0]?.expected_options?.ModelPrice))).toEqual({
    'example-model': 0.1,
  })
  expect(JSON.parse(String(patches[0]?.options?.ModelPrice))).toEqual({
    'example-model': 0.25,
  })
  expect(puts[0]).toMatchObject({
    key: 'ExposeRatioEnabled',
    value: true,
  })
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toContain('expose rejected')
  )
  expect(
    success.mock.calls.filter(
      ([text]) => text === 'Setting updated successfully'
    )
  ).toHaveLength(0)

  const save = screen.getByRole('button', { name: 'Save model prices' })
  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)

  await waitFor(() => expect(puts).toHaveLength(2))
  expect(patches).toHaveLength(1)
  expect(puts[1]).toMatchObject({
    key: 'ExposeRatioEnabled',
    value: true,
  })
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
  expect(screen.getByRole('switch', { name: 'Expose ratio API' })).toBeChecked()
})

it('keeps model prices dirty when PATCH returns HTTP 409', async () => {
  const user = userEvent.setup()
  const patches: Record<string, string>[] = []
  const error = vi.spyOn(toast, 'error')
  const info = vi.spyOn(toast, 'info')
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      const body = parseRequestData(config.data) as {
        options?: Record<string, string>
      }
      patches.push(body.options ?? {})
      const response = {
        data: {
          success: false,
          message: 'model pricing changed; reload before saving',
        },
        status: 409,
        statusText: 'Conflict',
        headers: {},
        config,
      }
      throw new AxiosError(
        'Request failed with status code 409',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        response
      )
    }
    throw new Error()
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
  await waitFor(() => expect(patches).toHaveLength(1))
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'model pricing changed; reload before saving',
    ])
  )

  const save = screen.getByRole('button', { name: 'Save model prices' })
  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)

  await waitFor(() => expect(patches).toHaveLength(2))
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
  expect(JSON.parse(patches[1].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('keeps model prices dirty when PATCH returns success:false', async () => {
  const user = userEvent.setup()
  const patches: Record<string, string>[] = []
  const error = vi.spyOn(toast, 'error')
  const info = vi.spyOn(toast, 'info')
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      const body = parseRequestData(config.data) as {
        options?: Record<string, string>
      }
      patches.push(body.options ?? {})
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
  await waitFor(() => expect(patches).toHaveLength(1))
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'model_price rejected',
    ])
  )

  const save = screen.getByRole('button', { name: 'Save model prices' })
  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)

  await waitFor(() => expect(patches).toHaveLength(2))
  expect(error.mock.calls.map(([text]) => text)).toEqual([
    'model_price rejected',
    'model_price rejected',
  ])
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
  expect(JSON.parse(patches[1].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('does not wipe sibling maps when current pricing JSON is invalid', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  const error = vi.spyOn(toast, 'error')
  const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
  renderWithClient(
    <PricingFormFixture onSave={save} values={{ ModelRatio: '{not-json' }} />
  )

  await screen.findByText('example-model')
  expect(
    consoleError.mock.calls.some((args) =>
      args.some((arg) => String(arg).includes('JSON Parse Error'))
    )
  ).toBe(false)

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

it('keeps group ratios dirty when PUT returns success:false', async () => {
  const user = userEvent.setup()
  const puts: { key: string; value: string }[] = []
  const error = vi.spyOn(toast, 'error')
  const adapter: AxiosAdapter = async (config) => {
    if (config.method === 'put' && config.url === '/api/option/') {
      const body =
        typeof config.data === 'string' ? JSON.parse(config.data) : config.data
      puts.push(body)
      return {
        data: { success: false, message: 'group_ratio rejected' },
        status: 200,
        statusText: 'OK',
        headers: {},
        config,
      }
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  renderWithClient(<GroupsFormFixture />)

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  const groupRatio = await screen.findByLabelText('Group ratios')
  fireEvent.change(groupRatio, { target: { value: '{"vip":1.5}' } })
  expect(groupRatio).toHaveValue('{"vip":1.5}')
  const save = await screen.findByRole('button', { name: 'Save group ratios' })
  await user.click(save)

  await waitFor(() => expect(puts).toHaveLength(1))
  expect(puts[0]).toEqual({
    key: 'GroupRatio',
    value: expect.any(String),
  })
  expect(JSON.parse(puts[0].value)).toEqual({ vip: 1.5 })
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'group_ratio rejected',
    ])
  )

  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)
  await waitFor(() => expect(puts).toHaveLength(2))
  expect(error.mock.calls.map(([text]) => text)).toEqual([
    'group_ratio rejected',
    'group_ratio rejected',
  ])
  expect(JSON.parse(puts[1].value)).toEqual({ vip: 1.5 })
})

it('keeps the unsaved editor draft when the viewport crosses 767px', async () => {
  let mobile = false
  const media = stubMatchMedia(
    (query) => query.includes('max-width: 767px') && mobile
  )
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const price = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')

  mobile = true
  media.notify()

  await waitFor(() => {
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
      '0.25'
    )
  })
  expect(
    screen.getAllByRole('button', {
      name: 'Save model prices',
      hidden: true,
    })
  ).toHaveLength(1)

  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('does not persist a zero input price with a dependent lane when the viewport crosses 767px', async () => {
  let mobile = false
  const media = stubMatchMedia(
    (query) => query.includes('max-width: 767px') && mobile
  )
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TOKEN_MODEL_VALUES} />
  )

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  const input = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(input)
  await user.type(input, '0')
  await user.click(screen.getByRole('switch', { name: 'Completion price' }))
  const completion = screen.getByRole('textbox', {
    name: 'Completion price',
  })
  await user.clear(completion)
  await user.type(completion, '0.5')

  mobile = true
  media.notify()

  await waitFor(() => {
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Input price' })).toHaveValue(
      '0'
    )
    expect(
      screen.getByRole('textbox', { name: 'Completion price' })
    ).toHaveValue('0.5')
  })

  await user.click(screen.getByRole('button', { name: 'Close' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelRatio)).toEqual({
    'example-model': 1,
  })
  expect(JSON.parse(save.mock.calls[0][0].CompletionRatio)).toEqual({})
})

it('keeps all dirty pricing maps when model pricing PATCH fails after saved defaults refresh', async () => {
  const user = userEvent.setup()
  const patches: Record<string, string>[] = []
  const error = vi.spyOn(toast, 'error')
  const info = vi.spyOn(toast, 'info')
  const success = vi.spyOn(toast, 'success')
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      const body = parseRequestData(config.data) as {
        options?: Record<string, string>
      }
      patches.push(body.options ?? {})
      modelCtl.set((prev) => ({ ...prev }))
      return {
        data: { success: false, message: 'model_ratio rejected' },
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
    <LiveRatioCard initialModel={TOKEN_MODEL_VALUES} modelCtl={modelCtl} />
  )

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(patches).toHaveLength(1))
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toContain(
      'model_ratio rejected'
    )
  )
  expect(success).not.toHaveBeenCalled()
  expect(JSON.parse(patches[0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
  expect(JSON.parse(patches[0].ModelRatio)).toEqual({})

  const save = screen.getByRole('button', { name: 'Save model prices' })
  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)

  await waitFor(() => expect(patches).toHaveLength(2))
  expect(JSON.parse(patches[1].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
  expect(JSON.parse(patches[1].ModelRatio)).toEqual({})
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
})

it('toasts setting updated once after all pricing keys succeed', async () => {
  const user = userEvent.setup()
  const success = vi.spyOn(toast, 'success')
  vi.spyOn(api, 'put')
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, message: '' },
  } as never)
  renderWithClient(
    <RatioSettingsCard
      modelDefaults={TOKEN_MODEL_VALUES}
      groupDefaults={EMPTY_GROUP_VALUES}
      toolPricesDefault='{}'
      visibleTabs={['models']}
    />
  )

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(modelPricingPatches()).toHaveLength(1))
  const options = modelPricingPatches()[0]?.options ?? {}
  expect(JSON.parse(String(options.ModelPrice))).toEqual({
    'example-model': 0.25,
  })
  expect(JSON.parse(String(options.ModelRatio))).toEqual({})
  expect(optionPuts()).toEqual([])
  await waitFor(() =>
    expect(
      success.mock.calls.filter(
        ([text]) => text === 'Setting updated successfully'
      )
    ).toHaveLength(1)
  )
})

it('does not reset unsaved model drafts when saved defaults keep the same values', async () => {
  const user = userEvent.setup()
  const info = vi.spyOn(toast, 'info')
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, message: '' },
  } as never)
  renderWithClient(
    <LiveRatioCard initialModel={TWO_MODEL_VALUES} modelCtl={modelCtl} />
  )

  await screen.findByText('other-model')
  const row =
    screen.getByText('other-model').closest('tr') ??
    screen.getByText('other-model').closest('[role="row"]')
  expect(row).not.toBeNull()
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Open menu' })
  )
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  await waitFor(() =>
    expect(screen.queryByText('other-model')).not.toBeInTheDocument()
  )

  modelCtl.set({ ...TWO_MODEL_VALUES })
  await waitFor(() =>
    expect(screen.queryByText('other-model')).not.toBeInTheDocument()
  )
  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  const priceEditor = await screen.findByRole('textbox', {
    name: 'Model fixed pricing',
  })
  expect(JSON.parse((priceEditor as HTMLTextAreaElement).value)).toEqual({
    'keep-me': 0.1,
  })
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(modelPricingPatches()).toHaveLength(1))
  expect(
    JSON.parse(String(modelPricingPatches()[0]?.options?.ModelPrice))
  ).toEqual({ 'keep-me': 0.1 })
  expect(info).not.toHaveBeenCalledWith('No model price changes to save')
})

it('keeps remaining group ratio keys dirty when a later PUT fails after defaults refresh', async () => {
  const user = userEvent.setup()
  const puts: { key: string; value: string }[] = []
  const error = vi.spyOn(toast, 'error')
  const groupCtl: DefaultsSetter<GroupDefaults> = { set: () => undefined }
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  const adapter: AxiosAdapter = async (config) => {
    if (config.method === 'put' && config.url === '/api/option/') {
      const body =
        typeof config.data === 'string' ? JSON.parse(config.data) : config.data
      puts.push(body)
      if (body.key === 'GroupRatio') {
        groupCtl.set((prev) => ({ ...prev, GroupRatio: body.value }))
        return {
          data: { success: true, message: '' },
          status: 200,
          statusText: 'OK',
          headers: {},
          config,
        }
      }
      return {
        data: { success: false, message: 'topup_group_ratio rejected' },
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
    <LiveRatioCard
      visibleTabs={['groups']}
      modelCtl={modelCtl}
      groupCtl={groupCtl}
    />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  fireEvent.change(await screen.findByLabelText('Group ratios'), {
    target: { value: '{"vip":1.5}' },
  })
  fireEvent.change(await screen.findByLabelText('Top-up group ratios'), {
    target: { value: '{"vip":2}' },
  })
  const save = await screen.findByRole('button', { name: 'Save group ratios' })
  await user.click(save)

  await waitFor(() =>
    expect(puts.some((body) => body.key === 'TopupGroupRatio')).toBe(true)
  )
  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toContain(
      'topup_group_ratio rejected'
    )
  )

  await waitFor(() => expect(save).toBeEnabled())
  await user.click(save)
  await waitFor(() => {
    const topupPuts = puts.filter((body) => body.key === 'TopupGroupRatio')
    expect(topupPuts.length).toBeGreaterThanOrEqual(2)
    expect(JSON.parse(topupPuts.at(-1)?.value ?? 'null')).toEqual({ vip: 2 })
  })
})

it('keeps the unsaved editor draft when the viewport crosses 767px from mobile to desktop', async () => {
  let mobile = true
  const media = stubMatchMedia(
    (query) => query.includes('max-width: 767px') && mobile
  )
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const price = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')

  mobile = false
  media.notify()

  await waitFor(() => {
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
      '0.25'
    )
  })
  expect(
    screen.getAllByRole('button', {
      name: 'Save model prices',
      hidden: true,
    })
  ).toHaveLength(1)

  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('keeps the previous model draft when switching to another row', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  await editRowFixedPrice(user, 'keep-me', '0.25')
  await editRowFixedPrice(user, 'other-model', '0.2')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'keep-me': 0.25,
    'other-model': 0.2,
  })
})

it('keeps the unsaved editor draft when adding a new model', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await editRowFixedPrice(user, 'example-model', '0.25')
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
    'example-model': 0.25,
    'new-model': 0.5,
  })
})

it('keeps the unsaved editor draft when table search closes the editor', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.type(screen.getByPlaceholderText('Search models...'), 'example')
  await waitFor(() =>
    expect(
      screen.queryByRole('textbox', { name: 'Fixed price' })
    ).not.toBeInTheDocument()
  )

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('keeps the unsaved editor draft when the mobile sheet is closed', async () => {
  stubMatchMedia((query) => query.includes('max-width: 767px'))
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const price = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')
  await user.click(screen.getByRole('button', { name: 'Close' }))

  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
      '0.25'
    )
  )
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('copies the flushed mobile editor price after the sheet is closed', async () => {
  stubMatchMedia((query) => query.includes('max-width: 767px'))
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  const keepMe = await screen.findByText('keep-me')
  const keepRow = keepMe.closest('tr') ?? keepMe.closest('[role="row"]')
  expect(keepRow).not.toBeNull()
  await user.click(
    within(keepRow as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  const price = screen.getByRole('textbox', { name: 'Fixed price' })
  await user.clear(price)
  await user.type(price, '0.25')
  await user.click(screen.getByRole('button', { name: 'Close' }))

  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )

  const other = screen.getByText('other-model')
  const otherRow = other.closest('tr') ?? other.closest('[role="row"]')
  expect(otherRow).not.toBeNull()
  await user.click(
    within(otherRow as HTMLElement).getByRole('checkbox', {
      name: 'Select row',
    })
  )
  await user.click(
    await screen.findByRole('button', { name: 'Copy keep-me pricing' })
  )
  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  await user.click(
    await screen.findByRole('button', { name: 'Save model prices' })
  )

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'keep-me': 0.25,
    'other-model': 0.25,
  })
})

it('keeps the unsaved editor draft after switching to JSON', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  await user.click(
    await screen.findByRole('button', { name: 'Save model prices' })
  )

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('keeps the unsaved editor draft after switching billing tabs', async () => {
  const user = userEvent.setup()
  vi.spyOn(api, 'patch').mockResolvedValue({ data: { success: true } })
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard
      initialModel={EMPTY_MODEL_VALUES}
      visibleTabs={['models', 'unset-models']}
      modelCtl={modelCtl}
    />
  )

  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(
    await screen.findByRole('tab', { name: 'Unset price models' })
  )
  await user.click(await screen.findByRole('tab', { name: 'Model prices' }))
  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
      '0.25'
    )
  )
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() =>
    expect(
      modelPricingPatches().some(
        (body) =>
          JSON.parse(String(body.options?.ModelPrice))['example-model'] === 0.25
      )
    ).toBe(true)
  )
})

it('does not wipe a model when the flushed editor draft has an empty price', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  const keepMe = await screen.findByText('keep-me')
  const keepRow = keepMe.closest('tr') ?? keepMe.closest('[role="row"]')
  expect(keepRow).not.toBeNull()
  await user.click(
    within(keepRow as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.clear(screen.getByRole('textbox', { name: 'Fixed price' }))
  await editRowFixedPrice(user, 'other-model', '0.33')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'keep-me': 0.1,
    'other-model': 0.33,
  })
})

it('keeps the unsaved editor draft after the visual editor unmounts', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)

  function Harness() {
    const [actionsContainer, setActionsContainer] =
      useState<HTMLDivElement | null>(null)
    const [showVisual, setShowVisual] = useState(true)
    const form = useForm({
      defaultValues: EMPTY_MODEL_VALUES,
    })
    return (
      <>
        <header>
          <div ref={setActionsContainer} />
        </header>
        <button type='button' onClick={() => setShowVisual(false)}>
          Leave visual editor
        </button>
        <SettingsPageProvider actionsContainer={actionsContainer}>
          {showVisual ? (
            <ModelRatioForm
              form={form}
              savedValues={EMPTY_MODEL_VALUES}
              onSave={save}
              onReset={() => undefined}
              isSaving={false}
              isResetting={false}
            />
          ) : (
            <button
              type='button'
              onClick={() => void form.handleSubmit(save)()}
            >
              Save model prices
            </button>
          )}
        </SettingsPageProvider>
      </>
    )
  }

  renderWithClient(<Harness />)
  await editRowFixedPrice(user, 'example-model', '0.25')
  await user.click(screen.getByRole('button', { name: 'Leave visual editor' }))
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.25,
  })
})

it('shows a single error toast when model price PATCH returns HTTP 500', async () => {
  const user = userEvent.setup()
  const error = vi.spyOn(toast, 'error')
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      const response = {
        data: { success: false, message: 'option store unavailable' },
        status: 500,
        statusText: 'Internal Server Error',
        headers: {},
        config,
      }
      throw new AxiosError(
        'Request failed with status code 500',
        'ERR_BAD_RESPONSE',
        config,
        undefined,
        response
      )
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

  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'option store unavailable',
    ])
  )
  expect(screen.getByRole('textbox', { name: 'Fixed price' })).toHaveValue(
    '0.25'
  )
})

it('still opens the editor after a failed delete while a pricing map is invalid JSON', async () => {
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
  await user.keyboard('{Escape}')
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  expect(
    await screen.findByRole('textbox', { name: 'Model name' })
  ).toHaveValue('delete-me')
})

it('keeps a re-added model after switching to JSON', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  await screen.findByText('other-model')
  const row =
    screen.getByText('other-model').closest('tr') ??
    screen.getByText('other-model').closest('[role="row"]')
  expect(row).not.toBeNull()
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Open menu' })
  )
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  await waitFor(() =>
    expect(screen.queryByText('other-model')).not.toBeInTheDocument()
  )

  await clickAddModel(user)
  await user.type(
    screen.getByRole('textbox', { name: 'Model name' }),
    'other-model'
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.type(screen.getByRole('textbox', { name: 'Fixed price' }), '0.5')
  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))

  const priceEditor = await screen.findByRole('textbox', {
    name: 'Model fixed pricing',
  })
  expect(JSON.parse((priceEditor as HTMLTextAreaElement).value)).toEqual({
    'keep-me': 0.1,
    'other-model': 0.5,
  })
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'keep-me': 0.1,
    'other-model': 0.5,
  })
})

it('shows a session expired toast when model price PATCH returns HTTP 401', async () => {
  const user = userEvent.setup()
  const error = vi.spyOn(toast, 'error')
  vi.spyOn(authSession, 'refreshAuthentication').mockResolvedValue({
    kind: 'anonymous',
  })
  const replace = vi.fn()
  const originalLocation = window.location
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: {
      ...originalLocation,
      pathname: '/dashboard',
      replace,
    },
  })
  const adapter: AxiosAdapter = async (config) => {
    if (
      config.method === 'patch' &&
      config.url === '/api/option/model_pricing'
    ) {
      const response = {
        data: { success: false },
        status: 401,
        statusText: 'Unauthorized',
        headers: {},
        config,
      }
      throw new AxiosError(
        'Request failed with status code 401',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        response
      )
    }
    throw new Error(`Unexpected request: ${config.method} ${config.url}`)
  }
  api.defaults.adapter = adapter
  try {
    renderWithClient(
      <RatioSettingsCard
        modelDefaults={EMPTY_MODEL_VALUES}
        groupDefaults={EMPTY_GROUP_VALUES}
        toolPricesDefault='{}'
        visibleTabs={['models']}
      />
    )

    await editExamplePrice(user)

    await waitFor(() =>
      expect(error.mock.calls.map(([text]) => text)).toEqual([
        'Session expired!',
      ])
    )
    expect(error).not.toHaveBeenCalledWith('Something went wrong!')
  } finally {
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    })
  }
})

it('applies newly fetched model price keys after a successful save', async () => {
  const user = userEvent.setup()
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  vi.spyOn(api, 'patch').mockResolvedValue({
    data: { success: true, message: '' },
  } as never)
  renderWithClient(
    <LiveRatioCard initialModel={EMPTY_MODEL_VALUES} modelCtl={modelCtl} />
  )

  await editExamplePrice(user)
  await waitFor(() => expect(modelPricingPatches()).toHaveLength(1))

  modelCtl.set({
    ...EMPTY_MODEL_VALUES,
    ModelPrice: '{"example-model":0.25,"extra-model":0.9}',
  })
  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  const priceEditor = await screen.findByRole('textbox', {
    name: 'Model fixed pricing',
  })
  await waitFor(() =>
    expect(JSON.parse((priceEditor as HTMLTextAreaElement).value)).toEqual({
      'example-model': 0.25,
      'extra-model': 0.9,
    })
  )
})

it('applies newly fetched group ratio keys after a successful save', async () => {
  const user = userEvent.setup()
  const success = vi.spyOn(toast, 'success')
  const groupCtl: DefaultsSetter<GroupDefaults> = { set: () => undefined }
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  vi.spyOn(api, 'put').mockResolvedValue({
    data: { success: true, message: '' },
  } as never)
  renderWithClient(
    <LiveRatioCard
      visibleTabs={['groups']}
      modelCtl={modelCtl}
      groupCtl={groupCtl}
    />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  fireEvent.change(await screen.findByLabelText('Group ratios'), {
    target: { value: '{"vip":1.5}' },
  })
  await user.click(
    await screen.findByRole('button', { name: 'Save group ratios' })
  )
  await waitFor(() =>
    expect(success.mock.calls.map(([text]) => text)).toContain(
      'Setting updated successfully'
    )
  )

  groupCtl.set({
    ...EMPTY_GROUP_VALUES,
    GroupRatio: '{"vip":1.5,"svip":2}',
  })
  await waitFor(() =>
    expect(
      JSON.parse(
        (screen.getByLabelText('Group ratios') as HTMLTextAreaElement).value
      )
    ).toEqual({ vip: 1.5, svip: 2 })
  )
})

it('still shows saved models when draft pricing JSON is invalid', async () => {
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture
      onSave={save}
      values={{ ModelPrice: '{' }}
      savedValues={EMPTY_MODEL_VALUES}
    />
  )
  expect(await screen.findByText('example-model')).toBeInTheDocument()
})

it('does not persist a zero input price with a dependent lane when switching to JSON', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TOKEN_MODEL_VALUES} />
  )

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  const input = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(input)
  await user.type(input, '0')
  await user.click(screen.getByRole('switch', { name: 'Completion price' }))
  const completion = screen.getByRole('textbox', {
    name: 'Completion price',
  })
  await user.clear(completion)
  await user.type(completion, '0.5')
  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )

  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelRatio)).toEqual({
    'example-model': 1,
  })
  expect(JSON.parse(save.mock.calls[0][0].CompletionRatio)).toEqual({})
})

it('rewrites leftover per-request price when saving the per-token tab', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Per-token' }))
  const input = screen.getByRole('textbox', { name: 'Input price' })
  await user.clear(input)
  await user.type(input, '4')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({})
  expect(JSON.parse(save.mock.calls[0][0].ModelRatio)).toEqual({
    'example-model': 2,
  })
})

it('does not persist an invalid billing expression when switching to JSON', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Expression' }))
  await user.click(screen.getByRole('combobox', { name: 'Editor mode' }))
  await user.click(screen.getByRole('option', { name: 'Expression editor' }))
  const expression = screen.getByRole('textbox', {
    name: 'Billing expression',
  })
  await user.clear(expression)
  await user.type(expression, 'not-a-valid-expr')
  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )

  await user.click(screen.getByRole('button', { name: 'Save model prices' }))
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'example-model': 0.1,
  })
  expect(JSON.parse(save.mock.calls[0][0].BillingMode)).toEqual({})
  expect(JSON.parse(save.mock.calls[0][0].BillingExpr)).toEqual({})
})

it('toasts when visual save is blocked by an invalid billing expression', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  const error = vi.spyOn(toast, 'error')
  renderWithClient(<PricingFormFixture onSave={save} />)

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Expression' }))
  await user.click(screen.getByRole('combobox', { name: 'Editor mode' }))
  await user.click(screen.getByRole('option', { name: 'Expression editor' }))
  const expression = screen.getByRole('textbox', {
    name: 'Billing expression',
  })
  await user.clear(expression)
  await user.type(expression, 'not-a-valid-expr')
  await user.click(screen.getByRole('button', { name: 'Save model prices' }))

  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toEqual([
      'Please fix the highlighted fields before saving',
    ])
  )
  expect(save).not.toHaveBeenCalled()
})

it('keeps a re-added model when table search closes the add editor', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  await screen.findByText('other-model')
  const row =
    screen.getByText('other-model').closest('tr') ??
    screen.getByText('other-model').closest('[role="row"]')
  expect(row).not.toBeNull()
  await user.click(
    within(row as HTMLElement).getByRole('button', { name: 'Open menu' })
  )
  await user.click(await screen.findByRole('menuitem', { name: 'Delete' }))
  await waitFor(() =>
    expect(screen.queryByText('other-model')).not.toBeInTheDocument()
  )

  await clickAddModel(user)
  await user.type(
    screen.getByRole('textbox', { name: 'Model name' }),
    'other-model'
  )
  await user.click(screen.getByRole('tab', { name: 'Per-request' }))
  await user.type(screen.getByRole('textbox', { name: 'Fixed price' }), '0.5')
  await user.type(screen.getByPlaceholderText('Search models...'), 'keep')
  await waitFor(() =>
    expect(
      screen.queryByRole('textbox', { name: 'Fixed price' })
    ).not.toBeInTheDocument()
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  await waitFor(() =>
    expect(jsonEditorValue('Model fixed pricing')).toEqual({
      'keep-me': 0.1,
      'other-model': 0.5,
    })
  )
})

it('keeps local dirty model price keys while applying newly fetched extras', async () => {
  const user = userEvent.setup()
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard initialModel={EMPTY_MODEL_VALUES} modelCtl={modelCtl} />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  await screen.findByRole('textbox', { name: 'Model fixed pricing' })
  setJsonEditorValue(
    'Model fixed pricing',
    '{"example-model":0.1,"local-model":0.3}'
  )
  await waitFor(() =>
    expect(jsonEditorValue('Model fixed pricing')).toEqual({
      'example-model': 0.1,
      'local-model': 0.3,
    })
  )

  modelCtl.set({
    ...EMPTY_MODEL_VALUES,
    ModelPrice: '{"example-model":0.1,"extra-model":0.9}',
  })
  await waitFor(() =>
    expect(jsonEditorValue('Model fixed pricing')).toEqual({
      'example-model': 0.1,
      'local-model': 0.3,
      'extra-model': 0.9,
    })
  )
})

it('does not restore locally deleted model price keys when applying newly fetched extras', async () => {
  const user = userEvent.setup()
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard initialModel={EMPTY_MODEL_VALUES} modelCtl={modelCtl} />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  await screen.findByRole('textbox', { name: 'Model fixed pricing' })
  setJsonEditorValue('Model fixed pricing', '{}')
  await waitFor(() =>
    expect(jsonEditorValue('Model fixed pricing')).toEqual({})
  )

  modelCtl.set({
    ...EMPTY_MODEL_VALUES,
    ModelPrice: '{"example-model":0.1,"extra-model":0.9}',
  })
  await waitFor(() =>
    expect(jsonEditorValue('Model fixed pricing')).toEqual({
      'extra-model': 0.9,
    })
  )
})

it('keeps local dirty group ratio keys while applying newly fetched extras', async () => {
  const user = userEvent.setup()
  const groupCtl: DefaultsSetter<GroupDefaults> = { set: () => undefined }
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard
      visibleTabs={['groups']}
      modelCtl={modelCtl}
      groupCtl={groupCtl}
    />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  fireEvent.change(await screen.findByLabelText('Group ratios'), {
    target: { value: '{"vip":1.5,"local-group":3}' },
  })
  await waitFor(() =>
    expect(
      JSON.parse(
        (screen.getByLabelText('Group ratios') as HTMLTextAreaElement).value
      )
    ).toEqual({ vip: 1.5, 'local-group': 3 })
  )

  groupCtl.set({
    ...EMPTY_GROUP_VALUES,
    GroupRatio: '{"vip":1.5,"extra-group":2}',
  })
  await waitFor(() =>
    expect(
      JSON.parse(
        (screen.getByLabelText('Group ratios') as HTMLTextAreaElement).value
      )
    ).toEqual({
      vip: 1.5,
      'local-group': 3,
      'extra-group': 2,
    })
  )
})

it('does not restore locally deleted group ratio keys when applying newly fetched extras', async () => {
  const user = userEvent.setup()
  const groupCtl: DefaultsSetter<GroupDefaults> = { set: () => undefined }
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard
      visibleTabs={['groups']}
      initialGroup={{
        ...EMPTY_GROUP_VALUES,
        GroupRatio: '{"vip":1.5}',
      }}
      modelCtl={modelCtl}
      groupCtl={groupCtl}
    />
  )

  await user.click(
    await screen.findByRole('button', { name: 'Switch to JSON' })
  )
  fireEvent.change(await screen.findByLabelText('Group ratios'), {
    target: { value: '{}' },
  })
  await waitFor(() =>
    expect(
      JSON.parse(
        (screen.getByLabelText('Group ratios') as HTMLTextAreaElement).value
      )
    ).toEqual({})
  )

  groupCtl.set({
    ...EMPTY_GROUP_VALUES,
    GroupRatio: '{"vip":1.5,"extra-group":2}',
  })
  await waitFor(() =>
    expect(
      JSON.parse(
        (screen.getByLabelText('Group ratios') as HTMLTextAreaElement).value
      )
    ).toEqual({
      'extra-group': 2,
    })
  )
})

it('toasts when batch copy is blocked by an invalid billing expression', async () => {
  const user = userEvent.setup()
  const save = vi.fn(async () => undefined)
  const error = vi.spyOn(toast, 'error')
  renderWithClient(
    <PricingFormFixture onSave={save} values={TWO_MODEL_VALUES} />
  )

  await screen.findByText('keep-me')
  const keepRow =
    screen.getByText('keep-me').closest('tr') ??
    screen.getByText('keep-me').closest('[role="row"]')
  const otherRow =
    screen.getByText('other-model').closest('tr') ??
    screen.getByText('other-model').closest('[role="row"]')
  expect(keepRow).not.toBeNull()
  expect(otherRow).not.toBeNull()

  await user.click(
    within(keepRow as HTMLElement).getByRole('button', { name: 'Edit' })
  )
  await user.click(screen.getByRole('tab', { name: 'Expression' }))
  await user.click(screen.getByRole('combobox', { name: 'Editor mode' }))
  await user.click(screen.getByRole('option', { name: 'Expression editor' }))
  const expression = screen.getByRole('textbox', { name: 'Billing expression' })
  await user.clear(expression)
  await user.type(expression, 'not-a-valid-expr')

  await user.click(
    within(otherRow as HTMLElement).getByRole('checkbox', {
      name: 'Select row',
    })
  )
  await user.click(
    await screen.findByRole('button', { name: 'Copy keep-me pricing' })
  )

  await waitFor(() =>
    expect(error.mock.calls.map(([text]) => text)).toContain(
      'Please fix the highlighted fields before saving'
    )
  )

  await user.click(screen.getByRole('button', { name: 'Switch to JSON' }))
  await user.click(
    await screen.findByRole('button', { name: 'Save model prices' })
  )
  await waitFor(() => expect(save).toHaveBeenCalledOnce())
  expect(JSON.parse(save.mock.calls[0][0].ModelPrice)).toEqual({
    'keep-me': 0.1,
    'other-model': 0.2,
  })
  expect(JSON.parse(save.mock.calls[0][0].BillingMode)).toEqual({})
  expect(JSON.parse(save.mock.calls[0][0].BillingExpr)).toEqual({})
})

it('hides the expose ratio switch when switching to group ratios', async () => {
  const user = userEvent.setup()
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard visibleTabs={['models', 'groups']} modelCtl={modelCtl} />
  )

  expect(
    await screen.findByRole('switch', { name: 'Expose ratio API' })
  ).toBeVisible()

  await user.click(screen.getByRole('tab', { name: 'Group ratios' }))
  await waitFor(() =>
    expect(
      screen.queryByRole('switch', { name: 'Expose ratio API' })
    ).not.toBeInTheDocument()
  )

  await user.click(screen.getByRole('tab', { name: 'Model prices' }))
  expect(
    await screen.findByRole('switch', { name: 'Expose ratio API' })
  ).toBeVisible()
})

it('keeps an invalid billing expression after switching away from the models tab', async () => {
  const user = userEvent.setup()
  const modelCtl: DefaultsSetter<ModelDefaults> = { set: () => undefined }
  renderWithClient(
    <LiveRatioCard visibleTabs={['models', 'groups']} modelCtl={modelCtl} />
  )

  await user.click(await screen.findByRole('button', { name: 'Edit' }))
  await user.click(screen.getByRole('tab', { name: 'Expression' }))
  await user.click(screen.getByRole('combobox', { name: 'Editor mode' }))
  await user.click(screen.getByRole('option', { name: 'Expression editor' }))
  const expression = screen.getByRole('textbox', { name: 'Billing expression' })
  await user.clear(expression)
  await user.type(expression, 'not-a-valid-expr')

  await user.click(screen.getByRole('tab', { name: 'Group ratios' }))
  await user.click(screen.getByRole('tab', { name: 'Model prices' }))

  expect(
    screen.getByRole('textbox', { name: 'Billing expression' })
  ).toHaveValue('not-a-valid-expr')
})
