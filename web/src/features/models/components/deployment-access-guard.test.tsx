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
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n, { whenInterfaceLanguageReady } from '@/i18n/config'
import zh from '@/i18n/locales/zh.json'

import { DeploymentAccessGuard } from './deployment-access-guard'

vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => () => undefined,
}))

describe('deployment connection error', () => {
  beforeEach(async () => {
    await whenInterfaceLanguageReady
    i18n.addResourceBundle('zhCN', 'translation', zh.translation, true, true)
    await i18n.changeLanguage('zhCN')
  })

  it('does not show raw transport English on Chinese chrome', () => {
    render(
      <DeploymentAccessGuard
        loading={false}
        loadingPhase='done'
        isEnabled
        connectionLoading={false}
        connectionOk={false}
        connectionError='Network Error'
        onRetry={() => undefined}
      >
        <div>ready</div>
      </DeploymentAccessGuard>
    )

    expect(screen.getByText('无法连接服务器')).toBeInTheDocument()
    expect(screen.queryByText('Network Error')).not.toBeInTheDocument()
  })
})
