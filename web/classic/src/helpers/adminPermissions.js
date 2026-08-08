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

export const ADMIN_PERMISSION_RESOURCES = {
  CHANNEL: 'channel',
};

export const ADMIN_PERMISSION_ACTIONS = {
  READ: 'read',
  OPERATE: 'operate',
  WRITE: 'write',
  SENSITIVE_WRITE: 'sensitive_write',
  SECRET_VIEW: 'secret_view',
};

const ROLE_SUPER_ADMIN = 100;

const getLocalUser = () => {
  if (typeof localStorage === 'undefined') {
    return null;
  }
  try {
    const raw = localStorage.getItem('user');
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
};

export const hasAdminPermission = (permissions, resource, action, user) => {
  const currentUser = user || getLocalUser();
  if ((Number(currentUser?.role) || 0) >= ROLE_SUPER_ADMIN) {
    return true;
  }

  const effectivePermissions = permissions || currentUser?.permissions;
  return effectivePermissions?.admin_permissions?.[resource]?.[action] === true;
};

export const getChannelPermissionFlags = (permissions, user) => ({
  canReadChannel: hasAdminPermission(
    permissions,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.READ,
    user,
  ),
  canOperateChannel: hasAdminPermission(
    permissions,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.OPERATE,
    user,
  ),
  canWriteChannel: hasAdminPermission(
    permissions,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.WRITE,
    user,
  ),
  canSensitiveWriteChannel: hasAdminPermission(
    permissions,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE,
    user,
  ),
  canViewChannelSecret: hasAdminPermission(
    permissions,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SECRET_VIEW,
    user,
  ),
});

export const canRepairChannelConsistency = (channelPermissions) =>
  channelPermissions?.canOperateChannel === true;
