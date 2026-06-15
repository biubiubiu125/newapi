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
import { useCallback, useEffect, useState } from 'react'
import { LayoutDashboard } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Switch } from '@/components/ui/switch'

type SidebarModuleConfig = {
  enabled: boolean
  [key: string]: boolean
}

type SidebarModulesConfig = Record<string, SidebarModuleConfig>

type SectionDef = {
  key: string
  title: string
  description: string
  modules: { key: string; title: string; description: string }[]
}

const REMOVED_ADMIN_MODULE_KEYS = ['risk_center', 'riskCenter'] as const

const isForcedVisibleModule = (section: string, module: string) =>
  section === 'admin' && module === 'setting'

const MODULE_TEXT_OVERRIDES: Record<
  string,
  { title: string; description: string }
> = {
  'console.image2': {
    title: 'Image2\u751f\u56fe',
    description: '\u5916\u90e8\u56fe\u7247\u751f\u6210\u5165\u53e3',
  },
  'console.model_check': {
    title: '\u6a21\u578b\u68c0\u6d4b',
    description: '\u5916\u90e8\u6a21\u578b\u68c0\u6d4b\u5165\u53e3',
  },
  'console.tickets': {
    title: '\u5de5\u5355\u4e2d\u5fc3',
    description: '\u521b\u5efa\u3001\u67e5\u770b\u548c\u56de\u590d\u5de5\u5355',
  },
  'admin.setting': {
    title: '\u7cfb\u7edf\u8bbe\u7f6e',
    description:
      '\u7cfb\u7edf\u8bbe\u7f6e\u4e3a\u5fc5\u9700\u5165\u53e3\uff0c\u4e0d\u80fd\u9690\u85cf',
  },
}

function getModuleDisplayText(
  sectionKey: string,
  module: { key: string; title: string; description: string }
) {
  return MODULE_TEXT_OVERRIDES[`${sectionKey}.${module.key}`] ?? module
}

function sanitizeSidebarModulesConfig(
  config: SidebarModulesConfig,
  role?: number
): SidebarModulesConfig {
  const sanitized: SidebarModulesConfig = { ...config }
  if ((role ?? ROLE.USER) < ROLE.ADMIN) {
    delete sanitized.admin
    return sanitized
  }

  const admin = sanitized.admin
  if (admin) {
    sanitized.admin = { ...admin }
    REMOVED_ADMIN_MODULE_KEYS.forEach((key) => {
      delete sanitized.admin[key]
    })
    if ((role ?? ROLE.USER) >= ROLE.SUPER_ADMIN) {
      sanitized.admin.setting = true
    } else {
      delete sanitized.admin.setting
    }
  }
  return sanitized
}

export function SidebarModulesCard() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [config, setConfig] = useState<SidebarModulesConfig>({})
  const currentUser = useAuthStore((s) => s.auth.user)
  const setUser = useAuthStore((s) => s.auth.setUser)
  const userRole = currentUser?.role ?? ROLE.USER
  const canConfigureAdmin = userRole >= ROLE.ADMIN
  const canConfigureSystemSettings = userRole >= ROLE.SUPER_ADMIN

  const allSectionDefs: SectionDef[] = [
    {
      key: 'chat',
      title: t('Chat Area'),
      description: t('Playground and chat functions'),
      modules: [
        {
          key: 'playground',
          title: t('Playground'),
          description: t('AI model testing environment'),
        },
        {
          key: 'chat',
          title: t('Chat'),
          description: t('Chat session management'),
        },
      ],
    },
    {
      key: 'console',
      title: t('Console Area'),
      description: t('Data management and log viewing'),
      modules: [
        {
          key: 'detail',
          title: t('Dashboard'),
          description: t('System data statistics'),
        },
        {
          key: 'token',
          title: t('Token Management'),
          description: t('API token management'),
        },
        {
          key: 'image2',
          title: 'Image2 生图',
          description: '外部图片生成入口',
        },
        {
          key: 'model_check',
          title: '模型检测',
          description: '外部模型检测入口',
        },
        {
          key: 'log',
          title: t('Usage Logs'),
          description: t('API usage records'),
        },
        {
          key: 'tickets',
          title: '工单中心',
          description: '创建、查看和回复工单',
        },
        {
          key: 'midjourney',
          title: t('Drawing Logs'),
          description: t('Drawing task records'),
        },
        {
          key: 'task',
          title: t('Task Logs'),
          description: t('System task records'),
        },
      ],
    },
    {
      key: 'personal',
      title: t('Personal Center Area'),
      description: t('User personal functions'),
      modules: [
        {
          key: 'topup',
          title: t('Wallet Management'),
          description: t('Balance and top-up management'),
        },
        {
          key: 'referral',
          title: t('Referral Center'),
          description: t('Referral links, commissions, and withdrawals'),
        },
        {
          key: 'personal',
          title: t('Personal Settings'),
          description: t('Personal info settings'),
        },
      ],
    },
    {
      key: 'admin',
      title: '管理员区域',
      description: '系统管理功能',
      modules: [
        {
          key: 'channel',
          title: t('Channels'),
          description: 'API 渠道配置',
        },
        {
          key: 'models',
          title: t('Models'),
          description: '模型目录和元数据管理',
        },
        {
          key: 'user',
          title: t('Users'),
          description: '用户账户和角色管理',
        },
        {
          key: 'referral',
          title: t('Referral Management'),
          description: '推广员、返佣和提现管理',
        },
        {
          key: 'ticket_management',
          title: '工单管理',
          description: '管理员查看和处理所有用户工单',
        },
        {
          key: 'redemption',
          title: t('Redemption Codes'),
          description: '兑换码生成管理',
        },
        {
          key: 'subscription',
          title: t('Subscription Management'),
          description: '订阅套餐管理',
        },
        {
          key: 'recharge_audit',
          title: t('Order Management'),
          description: '充值和订阅订单管理',
        },
        {
          key: 'provider_price_export',
          title: t('Public Price Export'),
          description: '公开供应商价格数据',
        },
        {
          key: 'setting',
          title: t('System Settings'),
          description: '系统设置为必需入口，不能隐藏',
        },
      ],
    },
  ]

  const sectionDefs = allSectionDefs
    .map((section) => {
      if (section.key !== 'admin') return section
      if (!canConfigureAdmin) return null
      return {
        ...section,
        modules: section.modules.filter(
          (module) => module.key !== 'setting' || canConfigureSystemSettings
        ),
      }
    })
    .filter((section): section is SectionDef => Boolean(section))

  const loadConfig = useCallback(async () => {
    try {
      const res = await api.get('/api/user/self')
      if (res.data.success && res.data.data?.sidebar_modules) {
        const raw = res.data.data.sidebar_modules
        const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw
        setConfig(sanitizeSidebarModulesConfig(parsed, userRole))
      } else {
        const defaults: SidebarModulesConfig = {}
        for (const sec of sectionDefs) {
          defaults[sec.key] = { enabled: true }
          for (const mod of sec.modules) defaults[sec.key][mod.key] = true
        }
        setConfig(defaults)
      }
    } catch {
      /* ignore */
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userRole])

  useEffect(() => {
    loadConfig()
  }, [loadConfig])

  const toggleSection = (sectionKey: string, val: boolean) => {
    setConfig((prev) => ({
      ...prev,
      [sectionKey]: { ...prev[sectionKey], enabled: val },
    }))
  }

  const toggleModule = (
    sectionKey: string,
    moduleKey: string,
    val: boolean
  ) => {
    setConfig((prev) => ({
      ...prev,
      [sectionKey]: { ...prev[sectionKey], [moduleKey]: val },
    }))
  }

  const handleSave = async () => {
    setLoading(true)
    try {
      const sanitizedConfig = sanitizeSidebarModulesConfig(config, userRole)
      const serialized = JSON.stringify(sanitizedConfig)
      const res = await api.put('/api/user/self', {
        sidebar_modules: serialized,
      })
      if (res.data.success) {
        setConfig(sanitizedConfig)
        // Sync to auth-store so useSidebarConfig re-runs and the sidebar
        // updates immediately without needing a page refresh.
        if (currentUser) {
          setUser({ ...currentUser, sidebar_modules: serialized })
        }
        toast.success(t('Saved successfully'))
      } else {
        toast.error(res.data.message || t('Save failed'))
      }
    } catch {
      toast.error(t('Save failed, please retry'))
    } finally {
      setLoading(false)
    }
  }

  const handleReset = () => {
    const defaults: SidebarModulesConfig = {}
    for (const sec of sectionDefs) {
      defaults[sec.key] = { enabled: true }
      for (const mod of sec.modules) defaults[sec.key][mod.key] = true
    }
    setConfig(defaults)
    toast.success(t('Reset to default configuration'))
  }

  return (
    <Card className='gap-0 overflow-hidden py-0'>
      <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
        <div className='flex items-center gap-3'>
          <div className='bg-muted flex h-8 w-8 shrink-0 items-center justify-center rounded-lg sm:h-9 sm:w-9'>
            <LayoutDashboard className='h-4 w-4' />
          </div>
          <div className='min-w-0'>
            <CardTitle className='text-lg tracking-tight sm:text-xl'>
              {t('Sidebar Personal Settings')}
            </CardTitle>
            <CardDescription className='text-xs sm:text-sm'>
              {t('Customize sidebar display content')}
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className='space-y-4 p-3 sm:space-y-5 sm:p-5'>
        {sectionDefs.map((section) => {
          const sectionEnabled = config[section.key]?.enabled !== false
          return (
            <div
              key={section.key}
              className='bg-background/60 rounded-xl border p-3'
            >
              <div className='flex items-start justify-between gap-3'>
                <div className='min-w-0'>
                  <p className='text-sm font-medium'>{section.title}</p>
                  <p className='text-muted-foreground text-xs'>
                    {section.description}
                  </p>
                </div>
                <Switch
                  checked={sectionEnabled}
                  onCheckedChange={(v) => toggleSection(section.key, v)}
                />
              </div>
              <div className='mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-1'>
                {section.modules.map((mod) => {
                  const display = getModuleDisplayText(section.key, mod)
                  const forcedVisible = isForcedVisibleModule(
                    section.key,
                    mod.key
                  )
                  return (
                    <div
                      key={mod.key}
                      className={`flex min-h-16 items-center justify-between rounded-lg border p-3 transition-opacity ${
                        sectionEnabled || forcedVisible ? '' : 'opacity-50'
                      }`}
                    >
                      <div className='mr-2 min-w-0'>
                        <p className='truncate text-sm font-medium'>
                          {display.title}
                        </p>
                        <p className='text-muted-foreground truncate text-xs'>
                          {display.description}
                        </p>
                      </div>
                      <Switch
                        checked={
                          forcedVisible
                            ? true
                            : config[section.key]?.[mod.key] !== false
                        }
                        onCheckedChange={
                          forcedVisible
                            ? undefined
                            : (v) => toggleModule(section.key, mod.key, v)
                        }
                        disabled={forcedVisible || !sectionEnabled}
                      />
                    </div>
                  )
                })}
              </div>
            </div>
          )
        })}

        <div className='flex flex-col-reverse gap-2 border-t pt-4 sm:flex-row sm:justify-end'>
          <Button variant='outline' onClick={handleReset}>
            {t('Reset to Default')}
          </Button>
          <Button onClick={handleSave} disabled={loading}>
            {loading ? t('Saving...') : t('Save Changes')}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
