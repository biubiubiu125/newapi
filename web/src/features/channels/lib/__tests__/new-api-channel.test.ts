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
import assert from 'node:assert/strict'
import { describe, test } from 'vitest'

import {
  CHANNEL_TYPE_NEW_API,
  CHANNEL_TYPE_OPTIONS,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  buildSettingJSON,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
  type ChannelFormValues,
} from '../channel-form'
import { getChannelTypeConfig } from '../channel-type-config'
import {
  canQueryBalanceChannel,
  getChannelTypeIcon,
  getChannelTypeLabel,
  canTestChannel,
  getKeyPromptForType,
} from '../channel-utils'
import type { Channel } from '../../types'

function newAPIForm(baseUrl: string) {
  return {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'New API upstream',
    type: CHANNEL_TYPE_NEW_API,
    base_url: baseUrl,
    key: 'test-key',
    models: 'gpt-5',
  }
}

describe('New API channel', () => {
  test('registers selection, ordering, model discovery, and icon metadata', () => {
    const option = CHANNEL_TYPE_OPTIONS.find(
      (item) => item.value === CHANNEL_TYPE_NEW_API
    )

    assert.deepEqual(option, {
      value: CHANNEL_TYPE_NEW_API,
      label: 'New API',
    })
    assert.equal(
      CHANNEL_TYPE_OPTIONS.findIndex(
        (item) => item.value === CHANNEL_TYPE_NEW_API
      ) + 1,
      CHANNEL_TYPE_OPTIONS.findIndex((item) => item.value === 58)
    )
    assert.equal(MODEL_FETCHABLE_TYPES.has(CHANNEL_TYPE_NEW_API), true)
    assert.equal(getChannelTypeIcon(CHANNEL_TYPE_NEW_API), 'NewAPI')
    assert.equal(
      getKeyPromptForType(CHANNEL_TYPE_NEW_API),
      'Enter API key for this channel'
    )
    assert.equal(getChannelTypeConfig(CHANNEL_TYPE_NEW_API).icon, 'NewAPI')
  })

  test('registers task plugin channels and round-trips their binding', () => {
    const taskPluginChannelType = 61
    const option = CHANNEL_TYPE_OPTIONS.find(
      (item) => item.value === taskPluginChannelType
    )

    assert.deepEqual(option, {
      value: taskPluginChannelType,
      label: 'Task Plugin',
    })
    assert.equal(getChannelTypeLabel(taskPluginChannelType), 'Task Plugin')
    assert.equal(getChannelTypeIcon(taskPluginChannelType), 'NewAPI')
    assert.equal(getChannelTypeConfig(taskPluginChannelType).id, 61)

    const formData = {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Task plugin upstream',
      type: taskPluginChannelType,
      key: 'upstream-secret',
      models: 'plugin-model',
      task_plugin_key: 'demo-plugin',
    } as ChannelFormValues & { task_plugin_key: string }

    assert.equal(
      JSON.parse(buildSettingJSON(formData)).task_plugin_key,
      'demo-plugin'
    )

    const channel = {
      id: 17,
      type: taskPluginChannelType,
      key: 'redacted',
      name: 'Task plugin upstream',
      status: 1,
      models: 'plugin-model',
      group: 'default',
      channel_info: {
        is_multi_key: false,
        multi_key_size: 0,
        multi_key_polling_index: 0,
        multi_key_mode: 'random',
      },
      setting: JSON.stringify({ task_plugin_key: 'demo-plugin' }),
      settings: '{}',
    } as Channel
    const defaults = transformChannelToFormDefaults(channel) as ChannelFormValues & {
      task_plugin_key?: string
    }
    assert.equal(defaults.task_plugin_key, 'demo-plugin')
    assert.equal(
      JSON.parse(transformFormDataToUpdatePayload(defaults, channel.id).setting as string)
        .task_plugin_key,
      'demo-plugin'
    )
  })

  test('disables direct testing for task plugin channels', () => {
    assert.equal(canTestChannel(61), false)
    assert.equal(canTestChannel(CHANNEL_TYPE_NEW_API), true)
  })

  test('disables direct balance queries for task plugin channels', () => {
    assert.equal(canQueryBalanceChannel(61), false)
    assert.equal(canQueryBalanceChannel(CHANNEL_TYPE_NEW_API), true)
  })

  test('requires a task plugin binding for task plugin channels', () => {
    const result = channelFormSchema.safeParse({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'Task plugin upstream',
      type: 61,
      key: 'upstream-secret',
      models: 'plugin-model',
      task_plugin_key: '  ',
    })

    assert.equal(result.success, false)
    if (!result.success) {
      assert.equal(
        result.error.issues.some(
          (issue) =>
            issue.path[0] === 'task_plugin_key' &&
            issue.message === 'Task plugin binding is required'
        ),
        true
      )
    }
  })

  test('requires a non-blank Base URL', () => {
    const blankResult = channelFormSchema.safeParse(newAPIForm('  '))

    assert.equal(blankResult.success, false)
    if (!blankResult.success) {
      assert.equal(
        blankResult.error.issues.some(
          (issue) =>
            issue.path[0] === 'base_url' &&
            issue.message === 'Base URL is required for this channel type'
        ),
        true
      )
    }

    assert.equal(
      channelFormSchema.safeParse(newAPIForm('https://new-api.example'))
        .success,
      true
    )
  })

  test('requires a Base URL for Sub2API', () => {
    const result = channelFormSchema.safeParse({
      ...newAPIForm(''),
      type: 59,
    })

    assert.equal(result.success, false)
    if (!result.success) {
      assert.equal(
        result.error.issues.some(
          (issue) =>
            issue.path[0] === 'base_url' &&
            issue.message === 'Base URL is required for this channel type'
        ),
        true
      )
    }
  })
})
