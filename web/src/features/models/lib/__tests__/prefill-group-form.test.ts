import { describe, expect, test } from 'vitest'

import { prefillGroupFormSchema } from '../../types'

describe('prefill group form', () => {
  test('trims the name and rejects a blank name', () => {
    expect(
      prefillGroupFormSchema.safeParse({
        name: '  chat  ',
        description: '  note  ',
        type: 'model',
        items: ['gpt-4o'],
      }).data
    ).toMatchObject({ name: 'chat', description: 'note' })

    expect(
      prefillGroupFormSchema.safeParse({
        name: '   ',
        description: '',
        type: 'model',
        items: [],
      }).success
    ).toBe(false)
  })

  test('rejects names and descriptions that exceed the database limits', () => {
    expect(
      prefillGroupFormSchema.safeParse({
        name: '名'.repeat(65),
        description: '',
        type: 'model',
        items: [],
      }).success
    ).toBe(false)
    expect(
      prefillGroupFormSchema.safeParse({
        name: 'ok',
        description: '述'.repeat(256),
        type: 'model',
        items: [],
      }).success
    ).toBe(false)
    expect(
      prefillGroupFormSchema.safeParse({
        name: '名'.repeat(64),
        description: '述'.repeat(255),
        type: 'endpoint',
        items: '{}',
      }).success
    ).toBe(true)
  })
})
