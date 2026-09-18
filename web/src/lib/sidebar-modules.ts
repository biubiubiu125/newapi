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
export const GPT_IMAGE_WORKBENCH_URL = 'https://gptimage.rkai6.com/'

export type SidebarSectionConfig = {
  enabled: boolean
  [key: string]: boolean
}

export type SidebarModulesAdminConfig = Record<string, SidebarSectionConfig>

export type SidebarModuleMeta = {
  title: string
  description: string
}

export type SidebarSectionMeta = SidebarModuleMeta & {
  modules: Record<string, SidebarModuleMeta>
}

export const SIDEBAR_MODULES_DEFAULT: SidebarModulesAdminConfig = {
  chat: {
    enabled: true,
    playground: true,
    chat: true,
  },
  console: {
    enabled: true,
    detail: true,
    token: true,
    model_check: true,
    log: true,
    midjourney: true,
    image_tasks: true,
    gpt_image: true,
    task: true,
  },
  personal: {
    enabled: true,
    topup: true,
    referral: true,
    tickets: true,
    personal: true,
  },
  admin: {
    enabled: true,
    channel: true,
    models: true,
    redemption: true,
    user: true,
    referral: true,
    ticket_management: true,
    setting: true,
    subscription: true,
    recharge_audit: true,
    task_plugins: true,
  },
}

export const FORCE_ENABLED_SIDEBAR_MODULES: Record<string, string[]> = {
  admin: ['setting'],
}

export const SIDEBAR_MODULE_ALIASES: Record<
  string,
  Record<string, string[]>
> = {
  admin: {
    referral: ['adminReferral'],
  },
}

export const REMOVED_SIDEBAR_MODULES: Record<string, string[]> = {
  console: ['image2'],
  admin: [
    'risk_center',
    'riskCenter',
    'provider_price_export',
    'providerPricing',
  ],
}

export const SIDEBAR_MODULES_META: Record<string, SidebarSectionMeta> = {
  chat: {
    title: 'Chat Area',
    description: 'Playground experiments and live conversations.',
    modules: {
      playground: {
        title: 'Playground',
        description: 'Used to test prompts and models.',
      },
      chat: {
        title: 'Chat',
        description: 'Access historical conversations and start new ones.',
      },
    },
  },
  console: {
    title: 'Console Area',
    description: 'Dashboard, tokens, and usage analytics.',
    modules: {
      detail: {
        title: 'Dashboard',
        description: 'Aggregated usage metrics and trend charts.',
      },
      token: {
        title: 'Token Management',
        description: 'Create, revoke, and audit API tokens.',
      },
      model_check: {
        title: 'Model Status Monitor',
        description: 'External model status monitor entry.',
      },
      log: {
        title: 'Usage Logs',
        description: 'Detailed request logs for investigation.',
      },
      midjourney: {
        title: 'Drawing Logs',
        description: 'Midjourney-style image task history.',
      },
      image_tasks: {
        title: 'Image Workbench',
        description:
          'Built-in text-to-image, image-to-image, and generation history.',
      },
      gpt_image: {
        title: 'GPT Image Workbench',
        description: 'Open the GPT image workbench (new tab).',
      },
      task: {
        title: 'Task Logs',
        description: 'Background task tracker for queued jobs.',
      },
    },
  },
  personal: {
    title: 'Personal area',
    description: 'Wallet management and personal preferences.',
    modules: {
      topup: {
        title: 'Wallet',
        description: 'Top up balance and view billing history.',
      },
      referral: {
        title: 'Referral Center',
        description: 'Invite links, commissions, and withdrawals.',
      },
      tickets: {
        title: 'Ticket Center',
        description: 'Users create, view, and reply to their own tickets.',
      },
      personal: {
        title: 'Profile',
        description: 'Personal settings and profile management.',
      },
    },
  },
  admin: {
    title: 'Admin area',
    description: 'Global configuration and admin tools.',
    modules: {
      channel: {
        title: 'Channels',
        description: 'Configure upstream providers and routing.',
      },
      models: {
        title: 'Models',
        description: 'Manage model catalog visibility and pricing.',
      },
      redemption: {
        title: 'Redemption Codes',
        description: 'Create and review invitation or quota codes.',
      },
      user: {
        title: 'Users',
        description: 'Manage user accounts and roles.',
      },
      referral: {
        title: 'Referral Management',
        description: 'Manage affiliates, commissions, and withdrawals.',
      },
      ticket_management: {
        title: 'Ticket Management',
        description: 'Admins review and handle all user tickets.',
      },
      setting: {
        title: 'System Settings',
        description: 'Advanced platform configuration.',
      },
      subscription: {
        title: 'Subscription Management',
        description: 'Manage subscription plans and pricing.',
      },
      recharge_audit: {
        title: 'Order Management',
        description: 'Review top-up and subscription orders.',
      },
      task_plugins: {
        title: 'Task Plugins',
        description:
          'Manage upstream task plugins, sandboxes, and built-in task protocols.',
      },
    },
  },
}

export const isForcedVisibleSidebarModule = (section: string, module: string) =>
  FORCE_ENABLED_SIDEBAR_MODULES[section]?.includes(module) ?? false

export const cloneSidebarModulesDefault = (): SidebarModulesAdminConfig =>
  Object.entries(SIDEBAR_MODULES_DEFAULT).reduce<SidebarModulesAdminConfig>(
    (acc, [section, config]) => {
      acc[section] = { ...config }
      return acc
    },
    {}
  )

export const removeRemovedSidebarModules = (
  config: SidebarModulesAdminConfig
): SidebarModulesAdminConfig => {
  const normalized: SidebarModulesAdminConfig = { ...config }

  Object.entries(REMOVED_SIDEBAR_MODULES).forEach(
    ([sectionKey, moduleKeys]) => {
      const section = normalized[sectionKey]
      if (!section) return
      normalized[sectionKey] = { ...section }
      moduleKeys.forEach((moduleKey) => {
        delete normalized[sectionKey][moduleKey]
      })
    }
  )

  return normalized
}

export const normalizeSidebarModuleAliases = (
  config: SidebarModulesAdminConfig
): SidebarModulesAdminConfig => {
  const normalized: SidebarModulesAdminConfig = { ...config }

  Object.entries(SIDEBAR_MODULE_ALIASES).forEach(
    ([sectionKey, moduleAliases]) => {
      const section = normalized[sectionKey]
      if (!section) return

      normalized[sectionKey] = { ...section }
      Object.entries(moduleAliases).forEach(([canonicalKey, aliases]) => {
        if (normalized[sectionKey][canonicalKey] === undefined) {
          const alias = aliases.find(
            (aliasKey) => normalized[sectionKey][aliasKey] !== undefined
          )
          if (alias) {
            normalized[sectionKey][canonicalKey] = normalized[sectionKey][alias]
          }
        }
        aliases.forEach((aliasKey) => {
          delete normalized[sectionKey][aliasKey]
        })
      })
    }
  )

  if (normalized.console?.tickets !== undefined) {
    normalized.personal = { ...normalized.personal, enabled: true }
    if (normalized.personal.tickets === undefined) {
      normalized.personal.tickets = normalized.console.tickets
    }
    normalized.console = { ...normalized.console }
    delete normalized.console.tickets
  }

  return removeRemovedSidebarModules(normalized)
}

export const applyForcedSidebarModules = (
  config: SidebarModulesAdminConfig
): SidebarModulesAdminConfig => {
  const normalized: SidebarModulesAdminConfig = { ...config }

  Object.entries(FORCE_ENABLED_SIDEBAR_MODULES).forEach(
    ([sectionKey, moduleKeys]) => {
      normalized[sectionKey] = {
        ...(normalized[sectionKey] ?? { enabled: true }),
      }
      moduleKeys.forEach((moduleKey) => {
        normalized[sectionKey][moduleKey] = true
      })
    }
  )

  return normalized
}

export const mergeWithDefaultSidebarModules = (
  config: SidebarModulesAdminConfig
): SidebarModulesAdminConfig => {
  const merged = normalizeSidebarModuleAliases(config)

  Object.entries(SIDEBAR_MODULES_DEFAULT).forEach(
    ([sectionKey, defaultSection]) => {
      const existingSection = merged[sectionKey]
      if (!existingSection) {
        merged[sectionKey] = { ...defaultSection }
        return
      }

      merged[sectionKey] = { ...defaultSection, ...existingSection }
    }
  )

  return applyForcedSidebarModules(merged)
}
