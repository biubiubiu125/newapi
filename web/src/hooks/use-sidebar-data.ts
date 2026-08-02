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
import { useQuery } from '@tanstack/react-query'
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
  ServerCog,
  Settings,
  Ticket,
  User,
  BadgeDollarSign,
  Share2,
  Users,
  Wallet,
} from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  ADMIN_SIDEBAR_BADGE_ACK_EVENT,
  hasAdminSidebarBadgeAck,
  initializeAdminSidebarBadgeCursors,
  normalizeSidebarBadgeCount,
  readAdminSidebarBadgeAck,
  unreadAdminSidebarBadgeCount,
} from '@/components/layout/lib/admin-sidebar-badge-ack'
import type { SidebarData } from '@/components/layout/types'
import {
  formatAdminReferralBadgeCount,
  useAdminReferralBadges,
} from '@/features/admin-referral/hooks/use-admin-referral-badges'
import { getRechargeAuditSummary } from '@/features/recharge-audit/api'
import { getTicketBadge } from '@/features/tickets/api'
import { getAdminUsersSummary } from '@/features/users/api'
import { useStatus } from '@/hooks/use-status'
import { isSidebarModuleEnabledFromStatus } from '@/lib/nav-modules'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

type AdminAlertBadges = {
  newUsers: number
  latestUserId?: number | string
  orderIssues: number
  latestOrderCursor?: string
  userTicketReplies: number
  latestUserTicketCursor?: string
  adminTicketEvents: number
  latestAdminTicketCursor?: string
}

const EMPTY_ADMIN_ALERT_BADGES: AdminAlertBadges = {
  newUsers: 0,
  latestUserId: undefined,
  orderIssues: 0,
  latestOrderCursor: undefined,
  userTicketReplies: 0,
  latestUserTicketCursor: undefined,
  adminTicketEvents: 0,
  latestAdminTicketCursor: undefined,
}

function normalizeSidebarCursorValue(
  value: number | string | undefined | null
): number | string | undefined {
  if (typeof value === 'number') {
    if (!Number.isFinite(value) || value < 0) return undefined
    return Math.floor(value)
  }
  const normalized = String(value ?? '').trim()
  return normalized ? normalized : undefined
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
    'personal',
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
  const referralBadgeParams = new URLSearchParams()
  const referralPendingAffiliateAck = readAdminSidebarBadgeAck(
    'admin-referral:pending-affiliates',
    userId,
    { mode: 'cursor' }
  )
  const referralPendingWithdrawalAck = readAdminSidebarBadgeAck(
    'admin-referral:pending-withdrawals',
    userId,
    { mode: 'cursor' }
  )
  const hasReferralPendingAffiliatesAck = hasAdminSidebarBadgeAck(
    'admin-referral:pending-affiliates',
    userId,
    { mode: 'cursor' }
  )
  const hasReferralPendingWithdrawalsAck = hasAdminSidebarBadgeAck(
    'admin-referral:pending-withdrawals',
    userId,
    { mode: 'cursor' }
  )
  if (
    hasReferralPendingAffiliatesAck &&
    typeof referralPendingAffiliateAck === 'string' &&
    referralPendingAffiliateAck.trim()
  ) {
    referralBadgeParams.set(
      'after_pending_affiliate_cursor',
      referralPendingAffiliateAck.trim()
    )
  }
  if (
    hasReferralPendingWithdrawalsAck &&
    typeof referralPendingWithdrawalAck === 'string' &&
    referralPendingWithdrawalAck.trim()
  ) {
    referralBadgeParams.set(
      'after_pending_withdrawal_cursor',
      referralPendingWithdrawalAck.trim()
    )
  }
  const { counts } = useAdminReferralBadges(
    Boolean(isAdmin && hasSidebarStatus && adminReferralEnabled),
    referralBadgeParams
  )
  const adminAlertQuery = useQuery({
    queryKey: [
      'admin-sidebar-alert-badges',
      userId,
      userTicketsEnabled,
      adminTicketsEnabled,
      adminUsersEnabled,
      isAdmin,
      adminReferralEnabled,
      adminRechargeAuditEnabled,
    ],
    enabled: Boolean(
      userId &&
      hasSidebarStatus &&
      (userTicketsEnabled ||
        (isAdmin &&
          (adminTicketsEnabled ||
            adminUsersEnabled ||
            adminReferralEnabled ||
            adminRechargeAuditEnabled)))
    ),
    queryFn: async (): Promise<AdminAlertBadges> => {
      const userTicketAck = readAdminSidebarBadgeAck('tickets:self', userId, {
        mode: 'cursor',
      })
      const adminTicketAck = readAdminSidebarBadgeAck('tickets:admin', userId, {
        mode: 'cursor',
      })
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
      const hasUserTicketAck = hasAdminSidebarBadgeAck('tickets:self', userId, {
        mode: 'cursor',
      })
      const hasAdminTicketAck = hasAdminSidebarBadgeAck(
        'tickets:admin',
        userId,
        {
          mode: 'cursor',
        }
      )
      const userTicketParams = new URLSearchParams()
      const adminTicketParams = new URLSearchParams()
      const userParams = new URLSearchParams()
      const orderParams = new URLSearchParams()
      orderParams.set('badge_only', '1')
      if (
        hasUserTicketAck &&
        typeof userTicketAck === 'string' &&
        userTicketAck.trim()
      ) {
        userTicketParams.set('after_cursor', userTicketAck.trim())
      }
      if (
        hasAdminTicketAck &&
        typeof adminTicketAck === 'string' &&
        adminTicketAck.trim()
      ) {
        adminTicketParams.set('after_cursor', adminTicketAck.trim())
      }
      if (hasUserAck && typeof userAck === 'number' && userAck >= 0) {
        userParams.set('after_id', String(userAck))
      } else if (hasUserAck && typeof userAck === 'string' && userAck.trim()) {
        userParams.set('after_id', userAck.trim())
      }
      if (hasOrderAck && typeof orderAck === 'string' && orderAck.trim()) {
        orderParams.set('after_order_cursor', orderAck.trim())
      }
      const [userTicketRes, adminTicketRes, userRes, orderRes] =
        await Promise.all([
          userTicketsEnabled
            ? getTicketBadge(false, userTicketParams)
            : Promise.resolve(undefined),
          isAdmin && adminTicketsEnabled
            ? getTicketBadge(true, adminTicketParams)
            : Promise.resolve(undefined),
          isAdmin && adminUsersEnabled
            ? getAdminUsersSummary(userParams)
            : Promise.resolve(undefined),
          isAdmin && adminRechargeAuditEnabled
            ? getRechargeAuditSummary(orderParams)
            : Promise.resolve(undefined),
        ])

      return {
        newUsers:
          isAdmin && adminUsersEnabled && hasUserAck
            ? normalizeSidebarBadgeCount(userRes?.data?.new_user_count)
            : 0,
        latestUserId:
          isAdmin && adminUsersEnabled
            ? normalizeSidebarCursorValue(userRes?.data?.latest_user_id)
            : undefined,
        orderIssues:
          isAdmin && adminRechargeAuditEnabled && hasOrderAck
            ? normalizeSidebarBadgeCount(orderRes?.data?.new_order_count)
            : 0,
        latestOrderCursor:
          isAdmin && adminRechargeAuditEnabled
            ? orderRes?.data?.latest_order_cursor || undefined
            : undefined,
        userTicketReplies:
          userTicketsEnabled && hasUserTicketAck
            ? normalizeSidebarBadgeCount(userTicketRes?.new_count)
            : 0,
        latestUserTicketCursor: userTicketsEnabled
          ? userTicketRes?.latest_cursor || undefined
          : undefined,
        adminTicketEvents:
          isAdmin && adminTicketsEnabled && hasAdminTicketAck
            ? normalizeSidebarBadgeCount(adminTicketRes?.new_count)
            : 0,
        latestAdminTicketCursor:
          isAdmin && adminTicketsEnabled
            ? adminTicketRes?.latest_cursor || undefined
            : undefined,
      }
    },
    refetchOnWindowFocus: false,
    refetchInterval: 60 * 1000,
    staleTime: 60 * 1000,
  })
  const adminAlerts = adminAlertQuery.data ?? EMPTY_ADMIN_ALERT_BADGES
  const adminAlertsLoaded = adminAlertQuery.isSuccess
  void badgeAckVersion
  const referralPendingAffiliatesUnread = hasReferralPendingAffiliatesAck
    ? unreadAdminSidebarBadgeCount(
        'admin-referral:pending-affiliates',
        counts.newPendingAffiliates,
        userId,
        { mode: 'cursor', cursor: counts.latestPendingAffiliateCursor }
      )
    : 0
  const referralPendingWithdrawalsUnread = hasReferralPendingWithdrawalsAck
    ? unreadAdminSidebarBadgeCount(
        'admin-referral:pending-withdrawals',
        counts.newPendingWithdrawals,
        userId,
        { mode: 'cursor', cursor: counts.latestPendingWithdrawalCursor }
      )
    : 0
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
  const userTicketUnread = unreadAdminSidebarBadgeCount(
    'tickets:self',
    adminAlerts.userTicketReplies,
    userId,
    { mode: 'cursor', cursor: adminAlerts.latestUserTicketCursor }
  )
  const adminTicketUnread = unreadAdminSidebarBadgeCount(
    'tickets:admin',
    adminAlerts.adminTicketEvents,
    userId,
    { mode: 'cursor', cursor: adminAlerts.latestAdminTicketCursor }
  )
  const usersBadge = formatAdminReferralBadgeCount(usersUnread)
  const orderManagementBadge = formatAdminReferralBadgeCount(
    orderManagementUnread
  )
  const userTicketBadge = formatAdminReferralBadgeCount(userTicketUnread)
  const adminTicketBadge = formatAdminReferralBadgeCount(adminTicketUnread)

  useEffect(() => {
    if (!adminAlertsLoaded) return

    const cursorInitialized = initializeAdminSidebarBadgeCursors(
      {
        'tickets:self': adminAlerts.latestUserTicketCursor,
        'tickets:admin': adminAlerts.latestAdminTicketCursor,
        users: adminAlerts.latestUserId,
        'recharge-audit': adminAlerts.latestOrderCursor,
        'admin-referral:pending-affiliates':
          counts.latestPendingAffiliateCursor,
        'admin-referral:pending-withdrawals':
          counts.latestPendingWithdrawalCursor,
      },
      userId,
      true
    )
    if (cursorInitialized) setBadgeAckVersion((value) => value + 1)
  }, [
    userId,
    adminAlerts.latestUserTicketCursor,
    adminAlerts.latestAdminTicketCursor,
    adminAlerts.latestUserId,
    adminAlerts.latestOrderCursor,
    counts.latestPendingAffiliateCursor,
    counts.latestPendingWithdrawalCursor,
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
            title: t('Image Tasks'),
            url: '/image-tasks',
            icon: Image,
          },
          {
            title: 'Image2生图',
            url: 'https://image.rkai6.com',
            icon: Image,
            external: true,
            configUrls: ['sidebar:console.image2'],
          },
          {
            title: '模型状态监测',
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
            title: '工单中心',
            url: '/tickets',
            icon: Ticket,
            configUrls: ['/tickets'],
            badge: userTicketBadge,
            badgeKey: 'tickets:self',
            badgeValue: adminAlerts.userTicketReplies,
            badgeCursor: adminAlerts.latestUserTicketCursor,
            badgeMode: 'cursor',
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
            badgeValue: referralManagementUnread,
            badgeAcks: [
              {
                key: 'admin-referral:pending-affiliates',
                value: counts.newPendingAffiliates,
                cursor: counts.latestPendingAffiliateCursor,
                mode: 'cursor',
              },
              {
                key: 'admin-referral:pending-withdrawals',
                value: counts.newPendingWithdrawals,
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
            badgeKey: 'tickets:admin',
            badgeValue: adminAlerts.adminTicketEvents,
            badgeCursor: adminAlerts.latestAdminTicketCursor,
            badgeMode: 'cursor',
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
          ...(isRoot
            ? [
                {
                  title: t('System Info'),
                  url: '/system-info',
                  icon: ServerCog,
                },
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
