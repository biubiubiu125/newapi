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
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const __dirname = dirname(fileURLToPath(import.meta.url))
const rootRouteSource = readFileSync(
  resolve(__dirname, '../__root.tsx'),
  'utf8'
)

describe('root setup status guard', () => {
  it('rechecks setup status after a full page reload instead of trusting localStorage', () => {
    expect(rootRouteSource).toContain('let setupStatusChecked = false')
    expect(rootRouteSource).not.toContain('SETUP_CHECKED_KEY')
    expect(rootRouteSource).not.toContain('setup_status_checked')
    expect(rootRouteSource).not.toContain('setSetupStatusCache')
  })
})
