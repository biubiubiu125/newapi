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
import { render, screen } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { formatTimestamp } from '@/lib/format'

import { UpdateCheckerSection } from '../update-checker-section'

afterEach(() => {
  vi.unstubAllGlobals()
})

test('shows the local version and uptime without checking GitHub', () => {
  const fetchMock = vi.fn(() => {
    throw new Error('GitHub must not be called')
  })
  vi.stubGlobal('fetch', fetchMock)
  const startTime = 1_700_000_000

  render(
    <UpdateCheckerSection
      currentVersion='main-bdd3eeb29'
      startTime={startTime}
    />
  )

  expect(screen.getByText('main-bdd3eeb29')).toBeInTheDocument()
  expect(screen.getByText('Current version')).toBeInTheDocument()
  expect(screen.getByText('Uptime since')).toBeInTheDocument()
  expect(screen.getByText(formatTimestamp(startTime))).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Check for updates' })
  ).not.toBeInTheDocument()
  expect(
    screen.queryByRole('dialog', { name: 'System updates' })
  ).not.toBeInTheDocument()
  expect(fetchMock).not.toHaveBeenCalled()
})
