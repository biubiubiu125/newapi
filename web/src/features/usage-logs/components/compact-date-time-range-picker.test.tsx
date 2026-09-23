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
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createInstance } from 'i18next'
import { I18nextProvider, initReactI18next } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import zh from '@/i18n/locales/zh.json'

import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'

async function renderZhPicker() {
  const zhI18n = createInstance()
  await zhI18n.use(initReactI18next).init({
    lng: 'zhCN',
    fallbackLng: 'zhCN',
    interpolation: { escapeValue: false },
    resources: { zhCN: zh },
  })
  const user = userEvent.setup()
  render(
    <I18nextProvider i18n={zhI18n}>
      <CompactDateTimeRangePicker onChange={() => undefined} />
    </I18nextProvider>
  )
  return user
}

describe('CompactDateTimeRangePicker', () => {
  it('does not use a native datetime-local control', async () => {
    const user = userEvent.setup()
    render(<CompactDateTimeRangePicker onChange={() => undefined} />)

    await user.click(screen.getByRole('button'))

    expect(document.querySelector('input[type="datetime-local"]')).toBeNull()
    expect(
      document.querySelector('[data-slot="calendar"], .rdp-root, table')
    ).not.toBeNull()
  })

  it('does not use a native time control', async () => {
    const user = userEvent.setup()
    render(<CompactDateTimeRangePicker onChange={() => undefined} />)

    await user.click(screen.getByRole('button'))

    expect(document.querySelector('input[type="time"]')).toBeNull()
    expect(screen.getAllByLabelText('Hour')).toHaveLength(2)
    expect(screen.getAllByLabelText('Minute')).toHaveLength(2)
  })

  it('keeps the month preset separate from check-in quota copy', async () => {
    const user = userEvent.setup()
    render(<CompactDateTimeRangePicker onChange={() => undefined} />)

    await user.click(screen.getByRole('button'))

    expect(
      screen.getByRole('button', { name: 'This calendar month' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'This month' })
    ).not.toBeInTheDocument()
  })

  it('shows a calendar-month label in simplified Chinese', async () => {
    const user = await renderZhPicker()

    await user.click(screen.getByRole('button', { name: '时间范围' }))

    expect(screen.getByRole('button', { name: '本月' })).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: '本月获得' })
    ).not.toBeInTheDocument()
  })
})
