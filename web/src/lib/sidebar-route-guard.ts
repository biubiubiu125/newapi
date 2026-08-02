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

export async function requireSidebarModule(
  options: SidebarRouteGuardOptions
) {
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
