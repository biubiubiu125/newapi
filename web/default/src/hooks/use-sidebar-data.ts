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
  Activity,
  Box,
  CreditCard,
  FileText,
  FlaskConical,
  Image,
  Key,
  LayoutDashboard,
  ListTodo,
  MessageSquare,
  Radio,
  Settings,
  Ticket,
  User,
  BadgeDollarSign,
  Share2,
  ShieldAlert,
  Users,
  Wallet,
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type SidebarData } from '@/components/layout/types'
import {
  formatAdminReferralBadgeCount,
  useAdminReferralBadges,
} from '@/features/admin-referral/hooks/use-admin-referral-badges'
import {
  ADMIN_SIDEBAR_BADGE_ACK_EVENT,
  lowerAdminSidebarBadgeAckBaselines,
  normalizeSidebarBadgeCount,
  unreadAdminSidebarBadgeCount,
} from '@/components/layout/lib/admin-sidebar-badge-ack'
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

/**
 * Root navigation groups for the application sidebar.
 *
 * These are shown when the URL does not match any nested sidebar view
 * registered in `layout/lib/sidebar-view-registry.ts`.
 */
export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const userRole = user?.role
  const userId = user?.id
  const [badgeAckVersion, setBadgeAckVersion] = useState(0)
  const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)
  const {
    counts,
    isSuccess: adminReferralBadgesLoaded,
  } = useAdminReferralBadges(isAdmin)
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
      const failedOrders = normalizeSidebarBadgeCount(
        orderRes.data?.totals?.failed_count
      )
      const orderAnomalies = normalizeSidebarBadgeCount(
        orderRes.data?.anomalies?.length
      )

      return {
        newUsers: normalizeSidebarBadgeCount(riskRes.data?.new_user_count),
        orderIssues: failedOrders + orderAnomalies,
        riskSignals: normalizeSidebarBadgeCount(riskRes.data?.signal_count),
      }
    },
  })
  const adminAlerts = adminAlertQuery.data ?? EMPTY_ADMIN_ALERT_BADGES
  const adminAlertsLoaded = adminAlertQuery.isSuccess
  void badgeAckVersion
  const referralManagementUnread = unreadAdminSidebarBadgeCount(
    'admin-referral',
    counts.total,
    userId
  )
  const usersUnread = unreadAdminSidebarBadgeCount(
    'users',
    adminAlerts.newUsers,
    userId
  )
  const orderManagementUnread = unreadAdminSidebarBadgeCount(
    'recharge-audit',
    adminAlerts.orderIssues,
    userId
  )
  const riskCenterUnread = unreadAdminSidebarBadgeCount(
    'risk-center',
    adminAlerts.riskSignals,
    userId
  )
  const usersBadge = formatAdminReferralBadgeCount(usersUnread)
  const orderManagementBadge = formatAdminReferralBadgeCount(
    orderManagementUnread
  )
  const riskCenterBadge = formatAdminReferralBadgeCount(riskCenterUnread)

  useEffect(() => {
    if (!adminAlertsLoaded || !adminReferralBadgesLoaded) return

    const lowered = lowerAdminSidebarBadgeAckBaselines(
      {
        'admin-referral': counts.total,
        users: adminAlerts.newUsers,
        'recharge-audit': adminAlerts.orderIssues,
        'risk-center': adminAlerts.riskSignals,
      },
      userId,
      true
    )
    if (lowered) setBadgeAckVersion((value) => value + 1)
  }, [
    userId,
    counts.total,
    adminAlerts.newUsers,
    adminAlerts.orderIssues,
    adminAlerts.riskSignals,
    adminAlertsLoaded,
    adminReferralBadgesLoaded,
  ])

  useEffect(() => {
    const onAck = () => setBadgeAckVersion((value) => value + 1)
    window.addEventListener(ADMIN_SIDEBAR_BADGE_ACK_EVENT, onAck)
    window.addEventListener('storage', onAck)
    return () => {
      window.removeEventListener(ADMIN_SIDEBAR_BADGE_ACK_EVENT, onAck)
      window.removeEventListener('storage', onAck)
    }
  }, [])

  return {
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
            title: 'Image2生图',
            url: 'https://image.rkai6.com',
            icon: Image,
            external: true,
            configUrls: ['/keys'],
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
