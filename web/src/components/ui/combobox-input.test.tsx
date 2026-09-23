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
import i18n from 'i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { ComboboxInput } from './combobox-input'

describe('ComboboxInput copy', () => {
  beforeEach(async () => {
    i18n.addResourceBundle(
      'en',
      'translation',
      {
        'Select or type...': 'Select or type...',
        'No option found.': 'No option found.',
        'My Claude': 'SHOULD_NOT_TRANSLATE',
      },
      true,
      true
    )
    await i18n.changeLanguage('en')
  })

  it('does not treat caller-provided placeholders as i18n keys', () => {
    render(
      <ComboboxInput
        options={[]}
        value=''
        onValueChange={() => undefined}
        placeholder='My Claude'
        emptyText=''
        allowCustomValue
      />
    )

    expect(screen.getByPlaceholderText('My Claude')).toBeInTheDocument()
    expect(
      screen.queryByPlaceholderText('SHOULD_NOT_TRANSLATE')
    ).not.toBeInTheDocument()
  })

  it('translates the default placeholder when none is provided', async () => {
    i18n.addResourceBundle(
      'zhCN',
      'translation',
      { 'Select or type...': '选择或输入…' },
      true,
      true
    )
    await i18n.changeLanguage('zhCN')

    render(
      <ComboboxInput options={[]} value='' onValueChange={() => undefined} />
    )

    expect(screen.getByPlaceholderText('选择或输入…')).toBeInTheDocument()
  })
})
