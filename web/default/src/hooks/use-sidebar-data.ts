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
import {
  LayoutDashboard,
  Activity,
  Key,
  FileText,
  Wallet,
  Box,
  Users,
  Ticket,
  User,
  Command,
  Radio,
  FlaskConical,
  MessageSquare,
  CreditCard,
  BadgeDollarSign,
  ListTodo,
  Settings,
  Share2,
  ShieldAlert,
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { WORKSPACE_IDS } from '@/components/layout/lib/workspace-registry'
import { type SidebarData } from '@/components/layout/types'
import {
  formatAdminReferralBadgeCount,
  useAdminReferralBadges,
} from '@/features/admin-referral/hooks/use-admin-referral-badges'
import { getRechargeAuditSummary } from '@/features/recharge-audit/api'
import { getRiskOverview } from '@/features/risk-center/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

type AdminAlertBadges = {
  newUsers: number
  orderIssues: number
  riskSignals: number
}

const EMPTY_ADMIN_ALERT_BADGES: AdminAlertBadges = {
  newUsers: 0,
  orderIssues: 0,
  riskSignals: 0,
}

const ADMIN_BADGE_ACK_STORAGE_KEY = 'admin-sidebar-alert-badge-ack-v2'

function normalizeBadgeCount(value: number | undefined): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return 0
  }
  return Math.floor(value)
}

function readBadgeAck(key: string, version: number): number {
  void version
  if (typeof window === 'undefined') return 0
  try {
    const raw = window.localStorage.getItem(ADMIN_BADGE_ACK_STORAGE_KEY)
    if (!raw) return 0
    const parsed = JSON.parse(raw) as Record<string, number>
    const value = parsed[key]
    return typeof value === 'number' && Number.isFinite(value) ? value : 0
  } catch {
    return 0
  }
}

function lowerBadgeAckBaselines(counts: Record<string, number>): boolean {
  if (typeof window === 'undefined') return false
  try {
    const raw = window.localStorage.getItem(ADMIN_BADGE_ACK_STORAGE_KEY)
    if (!raw) return false
    const parsed = JSON.parse(raw) as Record<string, number>
    let changed = false

    Object.entries(counts).forEach(([key, value]) => {
      const acknowledged = parsed[key]
      const normalized = normalizeBadgeCount(value)
      if (
        typeof acknowledged === 'number' &&
        Number.isFinite(acknowledged) &&
        acknowledged > normalized
      ) {
        parsed[key] = normalized
        changed = true
      }
    })

    if (changed) {
      window.localStorage.setItem(
        ADMIN_BADGE_ACK_STORAGE_KEY,
        JSON.stringify(parsed)
      )
    }
    return changed
  } catch {
    return false
  }
}

function unreadBadgeCount(
  key: string,
  value: number,
  ackVersion: number
): number {
  const normalized = normalizeBadgeCount(value)
  const acknowledged = readBadgeAck(key, ackVersion)
  return Math.max(0, normalized - acknowledged)
}

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const [badgeAckVersion, setBadgeAckVersion] = useState(0)
  const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)
  const { counts } = useAdminReferralBadges(isAdmin)
  const adminAlertQuery = useQuery({
    queryKey: ['admin-sidebar-alert-badges'],
    enabled: isAdmin,
    queryFn: async (): Promise<AdminAlertBadges> => {
      const riskParams = new URLSearchParams({ window_hours: '24' })
      const orderParams = new URLSearchParams({ window_hours: '24' })
      const [riskRes, orderRes] = await Promise.all([
        getRiskOverview(riskParams),
        getRechargeAuditSummary(orderParams),
      ])
      const failedOrders = normalizeBadgeCount(
        orderRes.data?.totals?.failed_count
      )
      const orderAnomalies = normalizeBadgeCount(
        orderRes.data?.anomalies?.length
      )

      return {
        newUsers: normalizeBadgeCount(riskRes.data?.new_user_count),
        orderIssues: failedOrders + orderAnomalies,
        riskSignals: normalizeBadgeCount(riskRes.data?.signal_count),
      }
    },
  })
  const adminAlerts = adminAlertQuery.data ?? EMPTY_ADMIN_ALERT_BADGES
  const referralManagementUnread = unreadBadgeCount(
    'admin-referral',
    counts.total,
    badgeAckVersion
  )
  const usersUnread = unreadBadgeCount(
    'users',
    adminAlerts.newUsers,
    badgeAckVersion
  )
  const orderManagementUnread = unreadBadgeCount(
    'recharge-audit',
    adminAlerts.orderIssues,
    badgeAckVersion
  )
  const riskCenterUnread = unreadBadgeCount(
    'risk-center',
    adminAlerts.riskSignals,
    badgeAckVersion
  )
  const usersBadge = formatAdminReferralBadgeCount(usersUnread)
  const orderManagementBadge = formatAdminReferralBadgeCount(
    orderManagementUnread
  )
  const riskCenterBadge = formatAdminReferralBadgeCount(riskCenterUnread)

  useEffect(() => {
    const lowered = lowerBadgeAckBaselines({
      'admin-referral': counts.total,
      users: adminAlerts.newUsers,
      'recharge-audit': adminAlerts.orderIssues,
      'risk-center': adminAlerts.riskSignals,
    })
    if (lowered) setBadgeAckVersion((value) => value + 1)
  }, [
    counts.total,
    adminAlerts.newUsers,
    adminAlerts.orderIssues,
    adminAlerts.riskSignals,
  ])

  useEffect(() => {
    const onAck = () => setBadgeAckVersion((value) => value + 1)
    window.addEventListener('admin-sidebar-badge-ack', onAck)
    window.addEventListener('storage', onAck)
    return () => {
      window.removeEventListener('admin-sidebar-badge-ack', onAck)
      window.removeEventListener('storage', onAck)
    }
  }, [])

  return {
    workspaces: [
      {
        id: WORKSPACE_IDS.DEFAULT,
        name: '', // Dynamically fetches system name
        logo: Command,
        plan: '', // Dynamically fetches system version
      },
    ],
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Dashboard'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            activeUrls: ['/usage-logs/drawing'],
            configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
            icon: ListTodo,
          },
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('Referral Center'),
            url: '/referral/center',
            icon: Share2,
          },
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Models'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
            badge: usersBadge,
            badgeKey: 'users',
            badgeValue: adminAlerts.newUsers,
          },
          {
            title: t('Referral Management'),
            url: '/admin-referral/overview',
            icon: Share2,
            badge: formatAdminReferralBadgeCount(referralManagementUnread),
            badgeKey: 'admin-referral',
            badgeValue: counts.total,
          },
          {
            title: t('Redemption Codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Subscription Management'),
            url: '/subscriptions',
            icon: CreditCard,
          },
          {
            title: t('Order Management'),
            url: '/recharge-audit',
            icon: BadgeDollarSign,
            badge: orderManagementBadge,
            badgeKey: 'recharge-audit',
            badgeValue: adminAlerts.orderIssues,
          },
          {
            title: t('Risk Center'),
            url: '/risk-center',
            icon: ShieldAlert,
            badge: riskCenterBadge,
            badgeKey: 'risk-center',
            badgeValue: adminAlerts.riskSignals,
          },
          {
            title: t('Public Price Export'),
            url: '/provider-price-export',
            icon: BadgeDollarSign,
          },
          {
            title: t('System Settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
        ],
      },
    ],
  }
}
