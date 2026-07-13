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
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import {
  SIDEBAR_MODULES_DEFAULT,
  SIDEBAR_MODULES_META,
  applyForcedSidebarModules,
  cloneSidebarModulesDefault,
  isForcedVisibleSidebarModule,
  normalizeSidebarModuleAliases,
  removeRemovedSidebarModules,
  type SidebarModulesAdminConfig,
} from '@/lib/sidebar-modules'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Switch } from '@/components/ui/switch'

type SectionDef = {
  key: string
  title: string
  description: string
  modules: { key: string; title: string; description: string }[]
}

function sanitizeSidebarModulesConfig(
  config: SidebarModulesAdminConfig,
  role?: number
): SidebarModulesAdminConfig {
  const sanitized = applyForcedSidebarModules(
    removeRemovedSidebarModules(normalizeSidebarModuleAliases(config))
  )
  if ((role ?? ROLE.USER) < ROLE.ADMIN) {
    delete sanitized.admin
    return sanitized
  }

  const admin = sanitized.admin
  if (admin) {
    sanitized.admin = { ...admin }
    if ((role ?? ROLE.USER) >= ROLE.SUPER_ADMIN) {
      sanitized.admin.setting = true
    } else {
      delete sanitized.admin.setting
    }
  }
  return sanitized
}

function buildSidebarDefaults(
  sections: SectionDef[],
  role?: number
): SidebarModulesAdminConfig {
  const defaults = cloneSidebarModulesDefault()
  const allowedSections = new Set(sections.map((section) => section.key))
  Object.keys(defaults).forEach((sectionKey) => {
    if (!allowedSections.has(sectionKey)) delete defaults[sectionKey]
  })
  return sanitizeSidebarModulesConfig(defaults, role)
}

export function SidebarModulesCard() {
  const [loading, setLoading] = useState(false)
  const [config, setConfig] = useState<SidebarModulesAdminConfig>({})
  const currentUser = useAuthStore((s) => s.auth.user)
  const setUser = useAuthStore((s) => s.auth.setUser)
  const userRole = currentUser?.role ?? ROLE.USER
  const canConfigureAdmin = userRole >= ROLE.ADMIN
  const canConfigureSystemSettings = userRole >= ROLE.SUPER_ADMIN

  const allSectionDefs: SectionDef[] = Object.entries(SIDEBAR_MODULES_DEFAULT)
    .map(([sectionKey, sectionConfig]) => {
      const sectionMeta = SIDEBAR_MODULES_META[sectionKey]
      if (!sectionMeta) return null
      const modules = Object.keys(sectionConfig)
        .filter((moduleKey) => moduleKey !== 'enabled')
        .map((moduleKey) => ({
          key: moduleKey,
          title: sectionMeta.modules[moduleKey]?.title ?? moduleKey,
          description: sectionMeta.modules[moduleKey]?.description ?? '',
        }))
      return {
        key: sectionKey,
        title: sectionMeta.title,
        description: sectionMeta.description,
        modules,
      }
    })
    .filter((section): section is SectionDef => Boolean(section))

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
        setConfig(buildSidebarDefaults(sectionDefs, userRole))
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
        toast.success('侧边栏设置已保存')
      } else {
        toast.error(res.data.message || '侧边栏设置保存失败')
      }
    } catch {
      toast.error('侧边栏设置保存失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  const handleReset = () => {
    setConfig(buildSidebarDefaults(sectionDefs, userRole))
    toast.success('已重置为默认配置')
  }

  return (
    <Card className='gap-0 overflow-hidden py-0'>
      <CardHeader className='border-b p-3 !pb-3 sm:p-5 sm:!pb-5'>
        <div className='flex items-center gap-3'>
          <IconBadge tone='info' size='title'>
            <LayoutDashboard />
          </IconBadge>
          <div className='min-w-0'>
            <CardTitle className='text-lg tracking-tight sm:text-xl'>
              侧边栏个人设置
            </CardTitle>
            <CardDescription className='text-xs sm:text-sm'>
              自定义侧边栏显示内容。
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
                  const forcedVisible = isForcedVisibleSidebarModule(
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
                          {mod.title}
                        </p>
                        <p className='text-muted-foreground truncate text-xs'>
                          {forcedVisible
                            ? '系统设置为必需入口，不能隐藏。'
                            : mod.description}
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
            重置为默认
          </Button>
          <Button onClick={handleSave} disabled={loading}>
            {loading ? '保存中...' : '保存更改'}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
