import { describe, expect, test } from 'vitest'

import type { Channel } from '../../types'
import { transformChannelToFormDefaults } from '../channel-form'

function channel(overrides: Partial<Channel>): Channel {
  return {
    id: 7,
    type: 1,
    key: 'sk-test',
    name: 'priority',
    status: 1,
    models: 'gpt-4',
    group: 'default',
    channel_info: {
      is_multi_key: false,
      multi_key_size: 0,
      multi_key_polling_index: 0,
      multi_key_mode: 'random',
    },
    ...overrides,
  } as Channel
}

describe('channel priority form defaults', () => {
  test('keeps a negative priority instead of reloading it as zero', () => {
    expect(transformChannelToFormDefaults(channel({ priority: -3 })).priority).toBe(
      -3
    )
  })

  test('keeps an explicit zero priority and zero weight', () => {
    const values = transformChannelToFormDefaults(
      channel({ priority: 0, weight: 0 })
    )
    expect(values.priority).toBe(0)
    expect(values.weight).toBe(0)
  })
})
