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

import { describe, expect, it } from 'vitest'

import { shouldApplyServerDefaults } from './settings-form-reset'

describe('shouldApplyServerDefaults', () => {
  it('ignores a new object with the same values', () => {
    const value = JSON.stringify({ enabled: true })

    expect(shouldApplyServerDefaults(value, value, value, false)).toBe(false)
    expect(shouldApplyServerDefaults(value, value, value, true)).toBe(false)
  })

  it('keeps in-progress edits when the server payload changes', () => {
    expect(
      shouldApplyServerDefaults(
        JSON.stringify({ enabled: false }),
        JSON.stringify({ enabled: true }),
        JSON.stringify({ enabled: true, draft: 'typed' }),
        true
      )
    ).toBe(false)
  })

  it('applies server values when the form is clean or already matches them', () => {
    const next = JSON.stringify({ enabled: false })
    const previous = JSON.stringify({ enabled: true })

    expect(shouldApplyServerDefaults(next, previous, previous, false)).toBe(
      true
    )
    expect(shouldApplyServerDefaults(next, previous, next, true)).toBe(true)
  })
})
