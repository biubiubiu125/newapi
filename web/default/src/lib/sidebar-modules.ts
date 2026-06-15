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
    image2: true,
    model_check: true,
    log: true,
    tickets: true,
    midjourney: true,
    task: true,
  },
  personal: {
    enabled: true,
    topup: true,
    referral: true,
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
    provider_price_export: true,
  },
}

export const FORCE_ENABLED_SIDEBAR_MODULES: Record<string, string[]> = {
  admin: ['setting'],
}

export const SIDEBAR_MODULE_ALIASES: Record<string, Record<string, string[]>> =
  {
    admin: {
      referral: ['adminReferral'],
      provider_price_export: ['providerPricing'],
    },
  }

export const REMOVED_SIDEBAR_MODULES: Record<string, string[]> = {
  admin: ['risk_center', 'riskCenter'],
}

export const SIDEBAR_MODULES_META: Record<string, SidebarSectionMeta> = {
  chat: {
    title: '聊天区域',
    description: 'Playground 实验和实时对话。',
    modules: {
      playground: {
        title: '游乐场',
        description: '用于测试提示词和模型。',
      },
      chat: {
        title: '聊天',
        description: '访问历史对话并开始新的对话。',
      },
    },
  },
  console: {
    title: '控制台区域',
    description: '仪表板、令牌和使用分析。',
    modules: {
      detail: {
        title: '数据看板',
        description: '聚合使用指标和趋势图表。',
      },
      token: {
        title: '令牌管理',
        description: '创建、撤销和审计 API 令牌。',
      },
      image2: {
        title: 'Image2 生图',
        description: '外部图片生成入口。',
      },
      model_check: {
        title: '模型检测',
        description: '外部模型检测入口。',
      },
      log: {
        title: '使用日志',
        description: '用于调查的详细请求日志。',
      },
      tickets: {
        title: '工单中心',
        description: '用户创建、查看和回复自己的工单。',
      },
      midjourney: {
        title: '绘制日志',
        description: 'Midjourney 风格图像任务历史。',
      },
      task: {
        title: '任务日志',
        description: '队列工作的后台任务跟踪器。',
      },
    },
  },
  personal: {
    title: '个人中心',
    description: '钱包管理和个人偏好设置。',
    modules: {
      topup: {
        title: '钱包',
        description: '充值余额并查看账单历史。',
      },
      referral: {
        title: '推广中心',
        description: '邀请链接、佣金和提现。',
      },
      personal: {
        title: '个人资料',
        description: '个人设置和资料管理。',
      },
    },
  },
  admin: {
    title: '管理员区域',
    description: '全局配置和管理工具。',
    modules: {
      channel: {
        title: '渠道',
        description: '配置上游提供者和路由。',
      },
      models: {
        title: '模型',
        description: '管理模型目录可见性和定价。',
      },
      redemption: {
        title: '兑换码',
        description: '创建和审核邀请或额度代码。',
      },
      user: {
        title: '用户',
        description: '管理用户账户和角色。',
      },
      referral: {
        title: '推广管理',
        description: '推广员、返佣和提现管理。',
      },
      ticket_management: {
        title: '工单管理',
        description: '管理员查看和处理所有用户工单。',
      },
      setting: {
        title: '系统设置',
        description: '高级平台配置。',
      },
      subscription: {
        title: '订阅管理',
        description: '管理订阅套餐和定价。',
      },
      recharge_audit: {
        title: '订单管理',
        description: '查看充值和订阅订单。',
      },
      provider_price_export: {
        title: '公开价格导出',
        description: '发布公开供应商价格数据。',
      },
    },
  },
}

export const isForcedVisibleSidebarModule = (
  section: string,
  module: string
) => FORCE_ENABLED_SIDEBAR_MODULES[section]?.includes(module) ?? false

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
