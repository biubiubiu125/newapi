import { describe, expect, test } from 'vitest'

import { isCompleteApiKeyBatch } from '../api-key-form'

describe('api key batch create', () => {
  test('closes the drawer only when every requested key was created', () => {
    expect(isCompleteApiKeyBatch(2, 2)).toBe(true)
    expect(isCompleteApiKeyBatch(1, 2)).toBe(false)
    expect(isCompleteApiKeyBatch(0, 2)).toBe(false)
  })
})
