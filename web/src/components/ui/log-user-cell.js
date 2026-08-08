export const getLogUserId = (record) => {
  const userId = record?.user_id
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

export const getLogUserDisplayName = (record) => {
  const username =
    typeof record?.username === 'string'
      ? record.username.trim()
      : record?.username
  if (username) {
    return String(username)
  }
  const userId = getLogUserId(record)
  return userId === null ? '' : `#${userId}`
}

export const openLogUserInfo = (record, showUserInfoFunc, event) => {
  const userId = getLogUserId(record)
  if (userId === null || typeof showUserInfoFunc !== 'function') {
    return false
  }
  event?.stopPropagation?.()
  showUserInfoFunc(userId)
  return true
}
