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

import { userFormSchema } from './user-form'

const base = {
  username: 'alice',
  password: '',
  role: 1,
  quota_dollars: 0,
  group: 'default',
}

describe('userFormSchema length limits', () => {
  it('accepts display names, emails, and remarks at the backend limit', () => {
    const result = userFormSchema.safeParse({
      ...base,
      display_name: '名'.repeat(20),
      email: `${'a'.repeat(38)}@example.com`,
      remark: '注'.repeat(255),
    })

    expect(result.success).toBe(true)
  })

  it('rejects values the user API will not store', () => {
    expect(
      userFormSchema.safeParse({
        ...base,
        display_name: '名'.repeat(21),
      }).success
    ).toBe(false)
    expect(
      userFormSchema.safeParse({
        ...base,
        email: `${'a'.repeat(40)}@example.com`,
      }).success
    ).toBe(false)
    expect(
      userFormSchema.safeParse({
        ...base,
        remark: '注'.repeat(256),
      }).success
    ).toBe(false)
    expect(
      userFormSchema.safeParse({
        ...base,
        email: '',
      }).success
    ).toBe(true)
  })
})
