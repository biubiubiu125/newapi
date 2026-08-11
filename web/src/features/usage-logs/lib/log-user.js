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
export const getLogUserId = (log) => {
  const userId = log?.user_id
  if (
    userId === undefined ||
    userId === null ||
    userId === '' ||
    userId === 0
  ) {
    return null
  }
  return userId
}

export const getLogUserDisplayName = (log) => {
  const username =
    typeof log?.username === 'string' ? log.username.trim() : log?.username
  if (username) {
    return String(username)
  }
  const userId = getLogUserId(log)
  return userId === null ? '' : `#${userId}`
}

export const openLogUserInfo = (
  log,
  setSelectedUserId,
  setUserInfoDialogOpen,
  event
) => {
  const userId = getLogUserId(log)
  if (
    userId === null ||
    typeof setSelectedUserId !== 'function' ||
    typeof setUserInfoDialogOpen !== 'function'
  ) {
    return false
  }
  event?.stopPropagation?.()
  setSelectedUserId(userId)
  setUserInfoDialogOpen(true)
  return true
}
