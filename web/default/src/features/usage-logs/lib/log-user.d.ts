export interface LogUserLike {
  username?: string | null
  user_id?: number | null
}

export function getLogUserId(log: LogUserLike | null | undefined): number | null

export function getLogUserDisplayName(
  log: LogUserLike | null | undefined
): string

export function openLogUserInfo(
  log: LogUserLike | null | undefined,
  setSelectedUserId: (userId: number) => void,
  setUserInfoDialogOpen: (open: boolean) => void,
  event?: { stopPropagation?: () => void }
): boolean
