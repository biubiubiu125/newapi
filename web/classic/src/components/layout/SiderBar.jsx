/*
Copyright (C) 2025 QuantumNous

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

import { Nav, Divider, Button, Badge } from '@douyinfe/semi-ui';
import { ChevronLeft } from 'lucide-react';
import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router-dom';

import { API, isAdmin, isRoot, showError } from '../../helpers';
import { getLucideIcon } from '../../helpers/render';
import { useMinimumLoadingTime } from '../../hooks/common/useMinimumLoadingTime';
import { useSidebar } from '../../hooks/common/useSidebar';
import { useSidebarCollapsed } from '../../hooks/common/useSidebarCollapsed';
import SkeletonWrapper from './components/SkeletonWrapper';

const routerMap = {
  home: '/',
  channel: '/console/channel',
  token: '/console/token',
  tickets: '/console/tickets',
  redemption: '/console/redemption',
  topup: '/console/topup',
  referral: '/console/referral',
  adminReferral: '/console/admin-referral',
  ticket_management: '/console/admin-tickets',
  recharge_audit: '/console/recharge-audit',
  user: '/console/user',
  subscription: '/console/subscription',
  log: '/console/log',
  midjourney: '/console/midjourney',
  setting: '/console/setting',
  about: '/about',
  detail: '/console',
  pricing: '/pricing',
  task: '/console/task',
  models: '/console/models',
  deployment: '/console/deployment',
  image2: 'https://image.rkai6.com',
  model_check: 'https://cx.rkai6.com/',
  playground: '/console/playground',
  personal: '/console/personal',
};

const SIDEBAR_BADGE_ACK_STORAGE_KEY_PREFIX = 'admin-sidebar-alert-badge-ack-v3';

const normalizeBadgeCount = (value) => {
  const count = Number(value);
  return Number.isFinite(count) && count > 0
    ? Math.min(Math.floor(count), 99)
    : 0;
};

const normalizeBadgeCursor = (value) => {
  if (typeof value === 'number') {
    return Number.isFinite(value) && value >= 0
      ? String(Math.floor(value))
      : '';
  }
  return String(value ?? '').trim();
};

const parseBadgeCursorParts = (value) => {
  const normalized = normalizeBadgeCursor(value);
  if (!normalized) return null;
  const parts = normalized.split(':').map((part) => Number(part));
  return parts.some((part) => !Number.isFinite(part)) ? null : parts;
};

const compareBadgeCursor = (left, right) => {
  const leftParts = parseBadgeCursorParts(left);
  const rightParts = parseBadgeCursorParts(right);
  if (!leftParts || !rightParts) {
    return normalizeBadgeCursor(left).localeCompare(
      normalizeBadgeCursor(right),
    );
  }
  const length = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < length; index += 1) {
    const diff = (leftParts[index] || 0) - (rightParts[index] || 0);
    if (diff !== 0) return diff;
  }
  return 0;
};

const getSidebarBadgeUserId = () => {
  try {
    const user = JSON.parse(localStorage.getItem('user') || 'null');
    return user?.id ?? user?.username ?? 'anonymous';
  } catch {
    return 'anonymous';
  }
};

const sidebarBadgeStorageKey = () =>
  `${SIDEBAR_BADGE_ACK_STORAGE_KEY_PREFIX}:${getSidebarBadgeUserId()}`;

const readSidebarBadgeAckState = () => {
  try {
    return JSON.parse(localStorage.getItem(sidebarBadgeStorageKey()) || '{}');
  } catch {
    return {};
  }
};

const writeSidebarBadgeAckState = (state) => {
  localStorage.setItem(sidebarBadgeStorageKey(), JSON.stringify(state));
};

const sidebarBadgeAckEntryKey = (key) => `${key}:cursor`;

const hasSidebarBadgeAck = (key) =>
  Object.hasOwn(readSidebarBadgeAckState(), sidebarBadgeAckEntryKey(key));

const readSidebarBadgeAck = (key) =>
  readSidebarBadgeAckState()[sidebarBadgeAckEntryKey(key)];

const acknowledgeSidebarBadge = (key, cursor) => {
  const normalized = normalizeBadgeCursor(cursor);
  if (!key || !normalized) return false;
  const state = readSidebarBadgeAckState();
  state[sidebarBadgeAckEntryKey(key)] = normalized;
  writeSidebarBadgeAckState(state);
  return true;
};

const initializeSidebarBadgeCursors = (cursors) => {
  const state = readSidebarBadgeAckState();
  let changed = false;
  Object.entries(cursors).forEach(([key, cursor]) => {
    const normalized = normalizeBadgeCursor(cursor);
    const entryKey = sidebarBadgeAckEntryKey(key);
    if (!normalized || normalizeBadgeCursor(state[entryKey])) return;
    state[entryKey] = normalized;
    changed = true;
  });
  if (changed) writeSidebarBadgeAckState(state);
  return changed;
};

const unreadSidebarBadgeCount = (key, count, cursor) => {
  if (!hasSidebarBadgeAck(key)) return 0;
  const normalizedCount = normalizeBadgeCount(count);
  const normalizedCursor = normalizeBadgeCursor(cursor);
  if (!normalizedCursor) return normalizedCount;
  const acknowledgedCursor = readSidebarBadgeAck(key);
  return compareBadgeCursor(acknowledgedCursor, normalizedCursor) >= 0
    ? 0
    : normalizedCount;
};

const withQuery = (url, params) => {
  const query = params.toString();
  return query ? `${url}?${query}` : url;
};

const createEmptySidebarBadges = () => ({
  userTicket: { count: 0, cursor: '' },
  adminTicket: { count: 0, cursor: '' },
  users: { count: 0, cursor: '' },
  orders: { count: 0, cursor: '' },
  referralAffiliates: { count: 0, cursor: '' },
  referralWithdrawals: { count: 0, cursor: '' },
});

const SiderBar = ({ onNavigate = () => {} }) => {
  const { t } = useTranslation();
  const [collapsed, toggleCollapsed] = useSidebarCollapsed();
  const {
    isModuleVisible,
    hasSectionVisibleModules,
    loading: sidebarLoading,
  } = useSidebar();

  const showSkeleton = useMinimumLoadingTime(sidebarLoading, 200);

  const [selectedKeys, setSelectedKeys] = useState(['home']);
  const [chatItems, setChatItems] = useState([]);
  const [openedKeys, setOpenedKeys] = useState([]);
  const [sidebarBadges, setSidebarBadges] = useState(createEmptySidebarBadges);
  const [badgeAckVersion, setBadgeAckVersion] = useState(0);
  const location = useLocation();
  const [routerMapState, setRouterMapState] = useState(routerMap);

  const ticketBadge = unreadSidebarBadgeCount(
    'tickets:self',
    sidebarBadges.userTicket.count,
    sidebarBadges.userTicket.cursor,
  );
  const adminTicketBadge = unreadSidebarBadgeCount(
    'tickets:admin',
    sidebarBadges.adminTicket.count,
    sidebarBadges.adminTicket.cursor,
  );
  const usersBadge = unreadSidebarBadgeCount(
    'users',
    sidebarBadges.users.count,
    sidebarBadges.users.cursor,
  );
  const orderManagementBadge = unreadSidebarBadgeCount(
    'recharge-audit',
    sidebarBadges.orders.count,
    sidebarBadges.orders.cursor,
  );
  const adminReferralBadge =
    unreadSidebarBadgeCount(
      'admin-referral:pending-affiliates',
      sidebarBadges.referralAffiliates.count,
      sidebarBadges.referralAffiliates.cursor,
    ) +
    unreadSidebarBadgeCount(
      'admin-referral:pending-withdrawals',
      sidebarBadges.referralWithdrawals.count,
      sidebarBadges.referralWithdrawals.cursor,
    );
  void badgeAckVersion;

  const workspaceItems = useMemo(() => {
    const items = [
      {
        text: t('数据看板'),
        itemKey: 'detail',
        to: '/detail',
        className:
          localStorage.getItem('enable_data_export') === 'true'
            ? ''
            : 'tableHiddle',
      },
      {
        text: t('令牌管理'),
        itemKey: 'token',
        to: '/token',
      },
      {
        text: 'Image2生图',
        itemKey: 'image2',
        to: 'https://image.rkai6.com',
        external: true,
      },
      {
        text: '模型状态监测',
        itemKey: 'model_check',
        to: 'https://cx.rkai6.com/',
        external: true,
      },
      {
        text: t('使用日志'),
        itemKey: 'log',
        to: '/log',
      },
      {
        text: t('绘图日志'),
        itemKey: 'midjourney',
        to: '/midjourney',
        className:
          localStorage.getItem('enable_drawing') === 'true'
            ? ''
            : 'tableHiddle',
      },
      {
        text: t('任务日志'),
        itemKey: 'task',
        to: '/task',
        className:
          localStorage.getItem('enable_task') === 'true' ? '' : 'tableHiddle',
      },
    ];

    // 根据配置过滤项目
    const filteredItems = items.filter((item) => {
      const configVisible = isModuleVisible('console', item.itemKey);
      return configVisible;
    });

    return filteredItems;
  }, [
    localStorage.getItem('enable_data_export'),
    localStorage.getItem('enable_drawing'),
    localStorage.getItem('enable_task'),
    t,
    isModuleVisible,
  ]);

  const financeItems = useMemo(() => {
    const items = [
      {
        text: t('钱包管理'),
        itemKey: 'topup',
        to: '/topup',
      },
      {
        text: t('推广中心'),
        itemKey: 'referral',
        to: '/console/referral',
      },
      {
        text: '工单中心',
        itemKey: 'tickets',
        to: '/console/tickets',
        badge: ticketBadge,
      },
      {
        text: t('个人设置'),
        itemKey: 'personal',
        to: '/personal',
      },
    ];

    // 根据配置过滤项目
    const filteredItems = items.filter((item) => {
      const configVisible = isModuleVisible('personal', item.itemKey);
      return configVisible;
    });

    return filteredItems;
  }, [ticketBadge, t, isModuleVisible]);

  const adminItems = useMemo(() => {
    const items = [
      {
        text: t('渠道管理'),
        itemKey: 'channel',
        to: '/channel',
        className: isAdmin() ? '' : 'tableHiddle',
      },
      {
        text: t('订阅管理'),
        itemKey: 'subscription',
        to: '/subscription',
        className: isAdmin() ? '' : 'tableHiddle',
      },
      {
        text: t('推广管理'),
        itemKey: 'adminReferral',
        to: '/console/admin-referral',
        className: isAdmin() ? '' : 'tableHiddle',
        badge: adminReferralBadge,
      },
      {
        text: '工单管理',
        itemKey: 'ticket_management',
        to: '/console/admin-tickets',
        className: isAdmin() ? '' : 'tableHiddle',
        badge: adminTicketBadge,
      },
      {
        text: '订单管理',
        itemKey: 'recharge_audit',
        to: '/console/recharge-audit',
        className: isAdmin() ? '' : 'tableHiddle',
        badge: orderManagementBadge,
      },
      {
        text: t('模型管理'),
        itemKey: 'models',
        to: '/console/models',
        className: isAdmin() ? '' : 'tableHiddle',
      },
      {
        text: t('模型部署'),
        itemKey: 'deployment',
        to: '/deployment',
        className: isAdmin() ? '' : 'tableHiddle',
      },
      {
        text: t('兑换码管理'),
        itemKey: 'redemption',
        to: '/redemption',
        className: isAdmin() ? '' : 'tableHiddle',
      },
      {
        text: t('用户管理'),
        itemKey: 'user',
        to: '/user',
        className: isAdmin() ? '' : 'tableHiddle',
        badge: usersBadge,
      },
      {
        text: t('系统设置'),
        itemKey: 'setting',
        to: '/setting',
        className: isRoot() ? '' : 'tableHiddle',
      },
    ];

    // 根据配置过滤项目
    const filteredItems = items.filter((item) => {
      if (item.className === 'tableHiddle') return false;
      const configVisible = isModuleVisible('admin', item.itemKey);
      return configVisible;
    });

    return filteredItems;
  }, [
    adminReferralBadge,
    adminTicketBadge,
    orderManagementBadge,
    usersBadge,
    isAdmin(),
    isRoot(),
    t,
    isModuleVisible,
  ]);

  const chatMenuItems = useMemo(() => {
    const items = [
      {
        text: t('操练场'),
        itemKey: 'playground',
        to: '/playground',
      },
      {
        text: t('聊天'),
        itemKey: 'chat',
        items: chatItems,
      },
    ];

    // 根据配置过滤项目
    const filteredItems = items.filter((item) => {
      const configVisible = isModuleVisible('chat', item.itemKey);
      return configVisible;
    });

    return filteredItems;
  }, [chatItems, t, isModuleVisible]);

  const loadSidebarBadges = async () => {
    const nextBadges = createEmptySidebarBadges();
    try {
      const params = new URLSearchParams();
      if (hasSidebarBadgeAck('tickets:self')) {
        const cursor = normalizeBadgeCursor(
          readSidebarBadgeAck('tickets:self'),
        );
        if (cursor) params.set('after_cursor', cursor);
      }
      const res = await API.get(withQuery('/api/user/tickets/badge', params), {
        disableDuplicate: true,
      });
      if (res?.data?.success) {
        nextBadges.userTicket = {
          count: normalizeBadgeCount(res.data.data?.new_count),
          cursor: normalizeBadgeCursor(res.data.data?.latest_cursor),
        };
      }
    } catch {
      nextBadges.userTicket = { count: 0, cursor: '' };
    }

    if (!isAdmin()) {
      if (
        initializeSidebarBadgeCursors({
          'tickets:self': nextBadges.userTicket.cursor,
        })
      ) {
        setBadgeAckVersion((value) => value + 1);
      }
      setSidebarBadges(nextBadges);
      return;
    }

    try {
      const params = new URLSearchParams();
      if (hasSidebarBadgeAck('tickets:admin')) {
        const cursor = normalizeBadgeCursor(
          readSidebarBadgeAck('tickets:admin'),
        );
        if (cursor) params.set('after_cursor', cursor);
      }
      const res = await API.get(
        withQuery('/api/user/admin/tickets/badge', params),
        {
          disableDuplicate: true,
        },
      );
      if (res?.data?.success) {
        nextBadges.adminTicket = {
          count: normalizeBadgeCount(res.data.data?.new_count),
          cursor: normalizeBadgeCursor(res.data.data?.latest_cursor),
        };
      }
    } catch {
      nextBadges.adminTicket = { count: 0, cursor: '' };
    }

    try {
      const params = new URLSearchParams();
      if (hasSidebarBadgeAck('users')) {
        const cursor = normalizeBadgeCursor(readSidebarBadgeAck('users'));
        if (cursor) params.set('after_id', cursor);
      }
      const res = await API.get(
        withQuery('/api/user/admin/users/summary', params),
        {
          disableDuplicate: true,
        },
      );
      if (res?.data?.success) {
        nextBadges.users = {
          count: hasSidebarBadgeAck('users')
            ? normalizeBadgeCount(res.data.data?.new_user_count)
            : 0,
          cursor: normalizeBadgeCursor(res.data.data?.latest_user_id),
        };
      }
    } catch {
      nextBadges.users = { count: 0, cursor: '' };
    }

    try {
      const params = new URLSearchParams();
      params.set('badge_only', '1');
      if (hasSidebarBadgeAck('recharge-audit')) {
        const cursor = normalizeBadgeCursor(
          readSidebarBadgeAck('recharge-audit'),
        );
        if (cursor) params.set('after_order_cursor', cursor);
      }
      const res = await API.get(
        withQuery('/api/user/admin/finance/recharge-audit/summary', params),
        { disableDuplicate: true },
      );
      if (res?.data?.success) {
        nextBadges.orders = {
          count: hasSidebarBadgeAck('recharge-audit')
            ? normalizeBadgeCount(res.data.data?.new_order_count)
            : 0,
          cursor: normalizeBadgeCursor(res.data.data?.latest_order_cursor),
        };
      }
    } catch {
      nextBadges.orders = { count: 0, cursor: '' };
    }

    try {
      const params = new URLSearchParams();
      if (hasSidebarBadgeAck('admin-referral:pending-affiliates')) {
        const cursor = normalizeBadgeCursor(
          readSidebarBadgeAck('admin-referral:pending-affiliates'),
        );
        if (cursor) params.set('after_pending_affiliate_cursor', cursor);
      }
      if (hasSidebarBadgeAck('admin-referral:pending-withdrawals')) {
        const cursor = normalizeBadgeCursor(
          readSidebarBadgeAck('admin-referral:pending-withdrawals'),
        );
        if (cursor) params.set('after_pending_withdrawal_cursor', cursor);
      }
      const res = await API.get(
        withQuery('/api/user/admin/referral/badges', params),
        {
          disableDuplicate: true,
        },
      );
      if (res?.data?.success) {
        nextBadges.referralAffiliates = {
          count: normalizeBadgeCount(res.data.data?.new_pending_affiliates),
          cursor: normalizeBadgeCursor(
            res.data.data?.latest_pending_affiliate_cursor ||
              res.data.data?.latest_pending_affiliate_id,
          ),
        };
        nextBadges.referralWithdrawals = {
          count: normalizeBadgeCount(res.data.data?.new_pending_withdrawals),
          cursor: normalizeBadgeCursor(
            res.data.data?.latest_pending_withdrawal_cursor ||
              res.data.data?.latest_pending_withdrawal_id,
          ),
        };
      }
    } catch {
      nextBadges.referralAffiliates = { count: 0, cursor: '' };
      nextBadges.referralWithdrawals = { count: 0, cursor: '' };
    }

    if (
      initializeSidebarBadgeCursors({
        'tickets:self': nextBadges.userTicket.cursor,
        'tickets:admin': nextBadges.adminTicket.cursor,
        users: nextBadges.users.cursor,
        'recharge-audit': nextBadges.orders.cursor,
        'admin-referral:pending-affiliates':
          nextBadges.referralAffiliates.cursor,
        'admin-referral:pending-withdrawals':
          nextBadges.referralWithdrawals.cursor,
      })
    ) {
      setBadgeAckVersion((value) => value + 1);
    }

    setSidebarBadges(nextBadges);
  };

  // 更新路由映射，添加聊天路由
  const updateRouterMapWithChats = (chats) => {
    const newRouterMap = { ...routerMap };

    if (Array.isArray(chats) && chats.length > 0) {
      for (let i = 0; i < chats.length; i++) {
        newRouterMap[`chat${i}`] = `/console/chat/${i}`;
      }
    }

    setRouterMapState(newRouterMap);
    return newRouterMap;
  };

  // 加载聊天项
  useEffect(() => {
    let chats = localStorage.getItem('chats');
    if (chats) {
      try {
        chats = JSON.parse(chats);
        if (Array.isArray(chats)) {
          const chatItems = [];
          for (let i = 0; i < chats.length; i++) {
            let shouldSkip = false;
            const chat = {};
            for (const key in chats[i]) {
              const link = chats[i][key];
              if (typeof link !== 'string') continue; // 确保链接是字符串
              if (
                link.startsWith('fluent') ||
                link.startsWith('ccswitch') ||
                link.startsWith('deepchat')
              ) {
                shouldSkip = true;
                break;
              }
              chat.text = key;
              chat.itemKey = `chat${i}`;
              chat.to = `/console/chat/${i}`;
            }
            if (shouldSkip || !chat.text) continue; // 避免推入空项
            chatItems.push(chat);
          }
          setChatItems(chatItems);
          updateRouterMapWithChats(chats);
        }
      } catch {
        showError('聊天数据解析失败');
      }
    }
  }, []);

  useEffect(() => {
    loadSidebarBadges();
    const timer = setInterval(loadSidebarBadges, 60 * 1000);
    return () => clearInterval(timer);
  }, []);

  const acknowledgeMenuBadge = (itemKey) => {
    let changed = false;
    switch (itemKey) {
      case 'tickets':
        changed = acknowledgeSidebarBadge(
          'tickets:self',
          sidebarBadges.userTicket.cursor,
        );
        break;
      case 'ticket_management':
        changed = acknowledgeSidebarBadge(
          'tickets:admin',
          sidebarBadges.adminTicket.cursor,
        );
        break;
      case 'user':
        changed = acknowledgeSidebarBadge('users', sidebarBadges.users.cursor);
        break;
      case 'recharge_audit':
        changed = acknowledgeSidebarBadge(
          'recharge-audit',
          sidebarBadges.orders.cursor,
        );
        break;
      case 'adminReferral':
        {
          const affiliatesChanged = acknowledgeSidebarBadge(
            'admin-referral:pending-affiliates',
            sidebarBadges.referralAffiliates.cursor,
          );
          const withdrawalsChanged = acknowledgeSidebarBadge(
            'admin-referral:pending-withdrawals',
            sidebarBadges.referralWithdrawals.cursor,
          );
          changed = affiliatesChanged || withdrawalsChanged;
        }
        break;
      default:
        break;
    }
    if (changed) setBadgeAckVersion((value) => value + 1);
  };

  // 根据当前路径设置选中的菜单项
  useEffect(() => {
    const currentPath = location.pathname;
    let matchingKey = Object.keys(routerMapState).find(
      (key) => routerMapState[key] === currentPath,
    );

    // 处理聊天路由
    if (!matchingKey && currentPath.startsWith('/console/chat/')) {
      const chatIndex = currentPath.split('/').pop();
      if (!isNaN(chatIndex)) {
        matchingKey = `chat${chatIndex}`;
      } else {
        matchingKey = 'chat';
      }
    }

    if (!matchingKey && currentPath.startsWith('/console/admin-referral')) {
      matchingKey = 'adminReferral';
    }

    if (!matchingKey && currentPath.startsWith('/console/recharge-audit')) {
      matchingKey = 'recharge_audit';
    }

    if (!matchingKey && currentPath.startsWith('/console/referral')) {
      matchingKey = 'referral';
    }

    // 如果找到匹配的键，更新选中的键
    if (matchingKey) {
      setSelectedKeys([matchingKey]);
    }
  }, [location.pathname, routerMapState]);

  // 监控折叠状态变化以更新 body class
  useEffect(() => {
    if (collapsed) {
      document.body.classList.add('sidebar-collapsed');
    } else {
      document.body.classList.remove('sidebar-collapsed');
    }
  }, [collapsed]);

  // 选中高亮颜色（统一）
  const SELECTED_COLOR = 'var(--semi-color-primary)';

  // 渲染自定义菜单项
  const renderNavItem = (item) => {
    // 跳过隐藏的项目
    if (item.className === 'tableHiddle') return null;

    const isSelected = selectedKeys.includes(item.itemKey);
    const textColor = isSelected ? SELECTED_COLOR : 'inherit';
    const content = (
      <span
        className='truncate font-medium text-sm'
        style={{ color: textColor }}
      >
        {item.text}
      </span>
    );

    return (
      <Nav.Item
        key={item.itemKey}
        itemKey={item.itemKey}
        text={
          item.badge ? (
            <Badge count={item.badge} type='danger' overflowCount={99}>
              {content}
            </Badge>
          ) : (
            content
          )
        }
        icon={
          <div className='sidebar-icon-container flex-shrink-0'>
            {getLucideIcon(item.itemKey, isSelected)}
          </div>
        }
        className={item.className}
      />
    );
  };

  // 渲染子菜单项
  const renderSubItem = (item) => {
    if (item.items && item.items.length > 0) {
      const isSelected = selectedKeys.includes(item.itemKey);
      const textColor = isSelected ? SELECTED_COLOR : 'inherit';

      return (
        <Nav.Sub
          key={item.itemKey}
          itemKey={item.itemKey}
          text={
            <span
              className='truncate font-medium text-sm'
              style={{ color: textColor }}
            >
              {item.text}
            </span>
          }
          icon={
            <div className='sidebar-icon-container flex-shrink-0'>
              {getLucideIcon(item.itemKey, isSelected)}
            </div>
          }
        >
          {item.items.map((subItem) => {
            const isSubSelected = selectedKeys.includes(subItem.itemKey);
            const subTextColor = isSubSelected ? SELECTED_COLOR : 'inherit';

            return (
              <Nav.Item
                key={subItem.itemKey}
                itemKey={subItem.itemKey}
                text={
                  <span
                    className='truncate font-medium text-sm'
                    style={{ color: subTextColor }}
                  >
                    {subItem.text}
                  </span>
                }
              />
            );
          })}
        </Nav.Sub>
      );
    } else {
      return renderNavItem(item);
    }
  };

  return (
    <div
      className='sidebar-container'
      style={{
        width: 'var(--sidebar-current-width)',
      }}
    >
      <SkeletonWrapper
        loading={showSkeleton}
        type='sidebar'
        className=''
        collapsed={collapsed}
        showAdmin={isAdmin()}
      >
        <Nav
          className='sidebar-nav'
          defaultIsCollapsed={collapsed}
          isCollapsed={collapsed}
          onCollapseChange={toggleCollapsed}
          selectedKeys={selectedKeys}
          itemStyle='sidebar-nav-item'
          hoverStyle='sidebar-nav-item:hover'
          selectedStyle='sidebar-nav-item-selected'
          renderWrapper={({ itemElement, props }) => {
            const to =
              routerMapState[props.itemKey] || routerMap[props.itemKey];
            const handleNavigate = () => {
              acknowledgeMenuBadge(props.itemKey);
              onNavigate();
            };

            // 如果没有路由，直接返回元素
            if (!to) return itemElement;

            if (/^https?:\/\//i.test(to)) {
              return (
                <a
                  style={{ textDecoration: 'none' }}
                  href={to}
                  target='_blank'
                  rel='noopener noreferrer'
                  onClick={handleNavigate}
                >
                  {itemElement}
                </a>
              );
            }

            return (
              <Link
                style={{ textDecoration: 'none' }}
                to={to}
                onClick={handleNavigate}
              >
                {itemElement}
              </Link>
            );
          }}
          onSelect={(key) => {
            // 如果点击的是已经展开的子菜单的父项，则收起子菜单
            if (openedKeys.includes(key.itemKey)) {
              setOpenedKeys(openedKeys.filter((k) => k !== key.itemKey));
            }

            setSelectedKeys([key.itemKey]);
            acknowledgeMenuBadge(key.itemKey);
          }}
          openKeys={openedKeys}
          onOpenChange={(data) => {
            setOpenedKeys(data.openKeys);
          }}
        >
          {/* 聊天区域 */}
          {hasSectionVisibleModules('chat') && (
            <div className='sidebar-section'>
              {!collapsed && (
                <div className='sidebar-group-label'>{t('聊天')}</div>
              )}
              {chatMenuItems.map((item) => renderSubItem(item))}
            </div>
          )}

          {/* 控制台区域 */}
          {hasSectionVisibleModules('console') && (
            <>
              <Divider className='sidebar-divider' />
              <div>
                {!collapsed && (
                  <div className='sidebar-group-label'>{t('控制台')}</div>
                )}
                {workspaceItems.map((item) => renderNavItem(item))}
              </div>
            </>
          )}

          {/* 个人中心区域 */}
          {hasSectionVisibleModules('personal') && (
            <>
              <Divider className='sidebar-divider' />
              <div>
                {!collapsed && (
                  <div className='sidebar-group-label'>{t('个人中心')}</div>
                )}
                {financeItems.map((item) => renderNavItem(item))}
              </div>
            </>
          )}

          {/* 管理员区域 - 只在管理员时显示且配置允许时显示 */}
          {isAdmin() &&
            hasSectionVisibleModules('admin') &&
            adminItems.length > 0 && (
              <>
                <Divider className='sidebar-divider' />
                <div>
                  {!collapsed && (
                    <div className='sidebar-group-label'>{t('管理员')}</div>
                  )}
                  {adminItems.map((item) => renderNavItem(item))}
                </div>
              </>
            )}
        </Nav>
      </SkeletonWrapper>

      {/* 底部折叠按钮 */}
      <div className='sidebar-collapse-button'>
        <SkeletonWrapper
          loading={showSkeleton}
          type='button'
          width={collapsed ? 36 : 156}
          height={24}
          className='w-full'
        >
          <Button
            theme='outline'
            type='tertiary'
            size='small'
            icon={
              <ChevronLeft
                size={16}
                strokeWidth={2.5}
                color='var(--semi-color-text-2)'
                style={{
                  transform: collapsed ? 'rotate(180deg)' : 'rotate(0deg)',
                }}
              />
            }
            onClick={toggleCollapsed}
            icononly={collapsed}
            style={
              collapsed
                ? { width: 36, height: 24, padding: 0 }
                : { padding: '4px 12px', width: '100%' }
            }
          >
            {!collapsed ? t('收起侧边栏') : null}
          </Button>
        </SkeletonWrapper>
      </div>
    </div>
  );
};

export default SiderBar;
