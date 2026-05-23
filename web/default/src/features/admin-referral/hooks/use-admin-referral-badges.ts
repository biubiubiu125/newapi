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
  getAdminReferralOverview,
  listAdminReferralWithdrawals,
} from '@/features/referral/api'

export type AdminReferralBadgeCounts = {
  pendingAffiliates: number
  pendingWithdrawals: number
  total: number
}

const EMPTY_COUNTS: AdminReferralBadgeCounts = {
  pendingAffiliates: 0,
  pendingWithdrawals: 0,
  total: 0,
}

function normalizeCount(value: number | undefined): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    return 0
  }
  return Math.floor(value)
}

export function formatAdminReferralBadgeCount(count: number): string | undefined {
  if (!Number.isFinite(count) || count <= 0) {
    return undefined
  }
  return count > 99 ? '99+' : String(count)
}

export function useAdminReferralBadges(enabled = true) {
  const query = useQuery({
    queryKey: ['admin-referral-badges'],
    enabled,
    queryFn: async (): Promise<AdminReferralBadgeCounts> => {
      const [overviewRes, pendingWithdrawalRes] = await Promise.all([
        getAdminReferralOverview(),
        listAdminReferralWithdrawals({
          p: 1,
          page_size: 1,
          status: 'pending',
        }),
      ])

      const pendingAffiliates = normalizeCount(
        overviewRes.data?.pending_affiliates
      )
      const pendingWithdrawals = normalizeCount(
        pendingWithdrawalRes.data?.total
      )

      return {
        pendingAffiliates,
        pendingWithdrawals,
        total: pendingAffiliates + pendingWithdrawals,
      }
    },
  })

  return {
    counts: query.data ?? EMPTY_COUNTS,
    badge: formatAdminReferralBadgeCount(query.data?.total ?? 0),
    error: query.error,
    isFetching: query.isFetching,
    isLoading: query.isLoading,
    isSuccess: query.isSuccess,
    refetch: query.refetch,
  }
}
