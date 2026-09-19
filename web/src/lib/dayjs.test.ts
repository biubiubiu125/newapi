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
along with this program. If you did not receive a copy of the GNU Affero
General Public License along with this program, see
<https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { afterEach, describe, expect, it } from 'vitest'

import dayjs, { applyDayjsLocale } from './dayjs'

describe('applyDayjsLocale', () => {
  afterEach(() => {
    applyDayjsLocale('en')
  })

  it('makes relative time use simplified Chinese for zhCN', () => {
    applyDayjsLocale('zhCN')
    expect(dayjs().subtract(3, 'minute').fromNow()).toMatch(/分钟|分鐘|前/)
  })

  it('keeps English relative time for en', () => {
    applyDayjsLocale('en')
    expect(dayjs().subtract(3, 'minute').fromNow()).toMatch(/minute/)
  })
})
