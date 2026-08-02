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
