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
export const getLogUserId = (record) => {
  const userId = record?.user_id;
  if (
    userId === undefined ||
    userId === null ||
    userId === '' ||
    userId === 0
  ) {
    return null;
  }
  return userId;
};

export const getLogUserDisplayName = (record) => {
  const username =
    typeof record?.username === 'string'
      ? record.username.trim()
      : record?.username;
  if (username) {
    return String(username);
  }
  const userId = getLogUserId(record);
  return userId === null ? '' : `#${userId}`;
};

export const openLogUserInfo = (record, showUserInfoFunc, event) => {
  const userId = getLogUserId(record);
  if (userId === null || typeof showUserInfoFunc !== 'function') {
    return false;
  }
  event?.stopPropagation?.();
  showUserInfoFunc(userId);
  return true;
};
