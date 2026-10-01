import { describe, expect, test } from 'vitest'
import type { TFunction } from 'i18next'

import { API_KEY_FORM_DEFAULT_VALUES, getApiKeyFormSchema } from '../api-key-form'

const t = ((key: string) => key) as TFunction

function parseName(name: string) {
  return getApiKeyFormSchema(t).safeParse({
    ...API_KEY_FORM_DEFAULT_VALUES,
    name,
  })
}

describe('API key name length', () => {
  test('accepts 50 bytes and rejects 51 bytes, including multibyte characters', () => {
    expect(parseName('a'.repeat(50)).success).toBe(true)
    expect(parseName('a'.repeat(51)).success).toBe(false)
    expect(parseName('名'.repeat(16)).success).toBe(true)
    const tooLong = parseName('名'.repeat(17))
    expect(tooLong.success).toBe(false)
    if (!tooLong.success) {
      expect(tooLong.error.issues.some((issue) => issue.path[0] === 'name')).toBe(
        true
      )
    }
  })
})
