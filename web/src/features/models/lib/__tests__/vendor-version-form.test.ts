import { describe, expect, test } from 'vitest'

import { vendorFormSchema as dialogVendorFormSchema } from '../../types'
import { vendorFormSchema as libraryVendorFormSchema } from '../model-form'

describe('vendor form version', () => {
  test('dialog schema submits the optimistic version', () => {
    const parsed = dialogVendorFormSchema.parse({
      name: 'Acme',
      description: '',
      icon: '',
      version: 'rev-1',
    })

    expect(parsed.version).toBe('rev-1')
  })

  test('library schema submits the optimistic version', () => {
    const parsed = libraryVendorFormSchema.parse({
      name: 'Acme',
      description: '',
      icon: '',
      status: 1,
      version: 'rev-1',
    })

    expect(parsed.version).toBe('rev-1')
  })
})
