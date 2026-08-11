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
import { redirect } from '@tanstack/react-router'

import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { isFreshSidebarModuleEnabled } from './nav-modules'

type SidebarRouteGuardOptions = {
  section: string
  module: string
  minRole?: number
  rootOnly?: boolean
  redirectTo?: string
}

export async function requireSidebarModule(options: SidebarRouteGuardOptions) {
  const { auth } = useAuthStore.getState()
  const minRole = options.minRole ?? ROLE.USER

  if (!auth.user || auth.user.role < minRole) {
    throw redirect({ to: '/403' })
  }

  if (options.rootOnly && auth.user.role !== ROLE.SUPER_ADMIN) {
    throw redirect({ to: '/403' })
  }

  const enabled = await isFreshSidebarModuleEnabled(
    options.section,
    options.module
  )
  if (!enabled) {
    throw redirect({ to: options.redirectTo ?? '/profile' })
  }
}

export function requireSystemSettingsModule() {
  const { auth } = useAuthStore.getState()
  if (!auth.user || auth.user.role !== ROLE.SUPER_ADMIN) {
    throw redirect({ to: '/403' })
  }
}
