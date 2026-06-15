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
  ScanSearch,
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
  hasAdminSidebarBadgeAck,
  initializeAdminSidebarBadgeCursors,
  normalizeSidebarBadgeCount,
  readAdminSidebarBadgeAck,
  unreadAdminSidebarBadgeCount,
} from '@/components/layout/lib/admin-sidebar-badge-ack'
import { getRechargeAuditSummary } from '@/features/recharge-audit/api'
import { getTicketBadge } from '@/features/tickets/api'
import { getAdminUsersSummary } from '@/features/users/api'
import { useStatus } from '@/hooks/use-status'
import { isSidebarModuleEnabledFromStatus } from '@/lib/nav-modules'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

type AdminAlertBadges = {
  newUsers: number
  latestUserId: number
  orderIssues: number
  latestOrderCursor?: string
}

const EMPTY_ADMIN_ALERT_BADGES: AdminAlertBadges = {
  newUsers: 0,
  latestUserId: 0,
  orderIssues: 0,
  latestOrderCursor: undefined,
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
  const { status } = useStatus()
  const hasSidebarStatus = Boolean(status)
  const isAdmin = Boolean(userRole && userRole >= ROLE.ADMIN)
  const isRoot = Boolean(userRole && userRole >= ROLE.SUPER_ADMIN)
  const statusRecord = status as Record<string, unknown> | null
  const userTicketsEnabled = isSidebarModuleEnabledFromStatus(
    statusRecord,
    'console',
    'tickets'
  )
  const adminTicketsEnabled = isSidebarModuleEnabledFromStatus(
    statusRecord,
    'admin',
    'ticket_management'
  )
  const adminUsersEnabled = isSidebarModuleEnabledFromStatus(
    statusRecord,
    'admin',
    'user'
  )
  const adminReferralEnabled = isSidebarModuleEnabledFromStatus(
    statusRecord,
    'admin',
    'referral'
  )
  const adminRechargeAuditEnabled = isSidebarModuleEnabledFromStatus(
    statusRecord,
    'admin',
    'recharge_audit'
  )
  const { counts } = useAdminReferralBadges(
    Boolean(isAdmin && hasSidebarStatus && adminReferralEnabled)
  )
  const userTicketBadgeQuery = useQuery({
    queryKey: ['sidebar-ticket-badge', userId, 'self'],
    enabled: Boolean(userId && hasSidebarStatus && userTicketsEnabled),
    queryFn: async () => {
      const data = await getTicketBadge(false)
      return normalizeSidebarBadgeCount(data?.count)
    },
    refetchOnWindowFocus: false,
    staleTime: 60 * 1000,
  })
  const adminTicketBadgeQuery = useQuery({
    queryKey: ['sidebar-ticket-badge', userId, 'admin'],
    enabled: Boolean(userId && isAdmin && hasSidebarStatus && adminTicketsEnabled),
    queryFn: async () => {
      const data = await getTicketBadge(true)
      return normalizeSidebarBadgeCount(data?.count)
    },
    refetchOnWindowFocus: false,
    staleTime: 60 * 1000,
  })
  const adminAlertQuery = useQuery({
    queryKey: [
      'admin-sidebar-alert-badges',
      userId,
      adminUsersEnabled,
      adminRechargeAuditEnabled,
    ],
    enabled: Boolean(
      isAdmin &&
        hasSidebarStatus &&
        (adminUsersEnabled || adminRechargeAuditEnabled)
    ),
    queryFn: async (): Promise<AdminAlertBadges> => {
      const userAck = readAdminSidebarBadgeAck('users', userId, {
        mode: 'cursor',
      })
      const orderAck = readAdminSidebarBadgeAck('recharge-audit', userId, {
        mode: 'cursor',
      })
      const hasUserAck = hasAdminSidebarBadgeAck('users', userId, {
        mode: 'cursor',
      })
      const hasOrderAck = hasAdminSidebarBadgeAck('recharge-audit', userId, {
        mode: 'cursor',
      })
      const userParams = new URLSearchParams()
      const orderParams = new URLSearchParams()
      orderParams.set('badge_only', '1')
      if (hasUserAck && typeof userAck === 'number' && userAck >= 0) {
        userParams.set('after_id', String(userAck))
      } else if (hasUserAck && typeof userAck === 'string' && userAck.trim()) {
        userParams.set('after_id', userAck.trim())
      }
      if (hasOrderAck && typeof orderAck === 'string' && orderAck.trim()) {
        orderParams.set('after_order_cursor', orderAck.trim())
      }
      const [userRes, orderRes] = await Promise.all([
        adminUsersEnabled
          ? getAdminUsersSummary(userParams)
          : Promise.resolve(undefined),
        adminRechargeAuditEnabled
          ? getRechargeAuditSummary(orderParams)
          : Promise.resolve(undefined),
      ])

      return {
        newUsers: adminUsersEnabled && hasUserAck
          ? normalizeSidebarBadgeCount(userRes?.data?.new_user_count)
          : 0,
        latestUserId: adminUsersEnabled
          ? normalizeSidebarBadgeCount(userRes?.data?.latest_user_id)
          : 0,
        orderIssues: adminRechargeAuditEnabled && hasOrderAck
          ? normalizeSidebarBadgeCount(orderRes?.data?.new_order_count)
          : 0,
        latestOrderCursor: adminRechargeAuditEnabled
          ? orderRes?.data?.latest_order_cursor || undefined
          : undefined,
      }
    },
    refetchOnWindowFocus: false,
    staleTime: 60 * 1000,
  })
  const adminAlerts = adminAlertQuery.data ?? EMPTY_ADMIN_ALERT_BADGES
  const adminAlertsLoaded = adminAlertQuery.isSuccess
  void badgeAckVersion
  const referralPendingAffiliatesUnread = unreadAdminSidebarBadgeCount(
    'admin-referral:pending-affiliates',
    counts.pendingAffiliates,
    userId,
    { mode: 'cursor', cursor: counts.latestPendingAffiliateCursor }
  )
  const referralPendingWithdrawalsUnread = unreadAdminSidebarBadgeCount(
    'admin-referral:pending-withdrawals',
    counts.pendingWithdrawals,
    userId,
    { mode: 'cursor', cursor: counts.latestPendingWithdrawalCursor }
  )
  const referralManagementUnread =
    referralPendingAffiliatesUnread + referralPendingWithdrawalsUnread
  const usersUnread = unreadAdminSidebarBadgeCount(
    'users',
    adminAlerts.newUsers,
    userId,
    { mode: 'cursor', cursor: adminAlerts.latestUserId }
  )
  const orderManagementUnread = unreadAdminSidebarBadgeCount(
    'recharge-audit',
    adminAlerts.orderIssues,
    userId,
    { mode: 'cursor', cursor: adminAlerts.latestOrderCursor }
  )
  const usersBadge = formatAdminReferralBadgeCount(usersUnread)
  const orderManagementBadge = formatAdminReferralBadgeCount(
    orderManagementUnread
  )
  const userTicketBadge = formatAdminReferralBadgeCount(
    userTicketBadgeQuery.data ?? 0
  )
  const adminTicketBadge = formatAdminReferralBadgeCount(
    adminTicketBadgeQuery.data ?? 0
  )

  useEffect(() => {
    if (!adminAlertsLoaded) return

    const cursorInitialized = initializeAdminSidebarBadgeCursors(
      {
        users: adminAlerts.latestUserId,
        'recharge-audit': adminAlerts.latestOrderCursor,
      },
      userId,
      true
    )
    if (cursorInitialized) setBadgeAckVersion((value) => value + 1)
  }, [
    userId,
    adminAlerts.latestUserId,
    adminAlerts.latestOrderCursor,
    adminAlertsLoaded,
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
            configUrls: ['sidebar:console.image2'],
          },
          {
            title: '模型检测',
            url: 'https://cx.rkai6.com/',
            icon: ScanSearch,
            external: true,
            configUrls: ['sidebar:console.model_check'],
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Drawing Logs'),
            url: '/usage-logs/drawing',
            configUrls: ['/usage-logs/drawing'],
            icon: Image,
          },
          {
            title: '工单中心',
            url: '/tickets',
            icon: Ticket,
            configUrls: ['/tickets'],
            badge: userTicketBadge,
          },
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            configUrls: ['/usage-logs/task'],
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
            badgeCursor: adminAlerts.latestUserId,
            badgeMode: 'cursor',
          },
          {
            title: t('Referral Management'),
            url: '/admin-referral/overview',
            icon: Share2,
            badge: formatAdminReferralBadgeCount(referralManagementUnread),
            badgeKey: 'admin-referral',
            badgeValue: counts.total,
            badgeAcks: [
              {
                key: 'admin-referral:pending-affiliates',
                value: counts.pendingAffiliates,
                cursor: counts.latestPendingAffiliateCursor,
                mode: 'cursor',
              },
              {
                key: 'admin-referral:pending-withdrawals',
                value: counts.pendingWithdrawals,
                cursor: counts.latestPendingWithdrawalCursor,
                mode: 'cursor',
              },
            ],
          },
          {
            title: '工单管理',
            url: '/admin-tickets',
            icon: Ticket,
            configUrls: ['/admin-tickets'],
            badge: adminTicketBadge,
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
            badgeCursor: adminAlerts.latestOrderCursor,
            badgeMode: 'cursor',
          },
          {
            title: t('Public Price Export'),
            url: '/provider-price-export',
            icon: BadgeDollarSign,
          },
          ...(isRoot
            ? [
                {
                  title: t('System Settings'),
                  url: '/system-settings/site',
                  activeUrls: ['/system-settings'],
                  icon: Settings,
                },
              ]
            : []),
        ],
      },
    ],
  }
}
