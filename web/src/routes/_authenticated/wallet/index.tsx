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
import { createFileRoute } from '@tanstack/react-router'
import { z } from 'zod'

import { Wallet } from '@/features/wallet'
import { requireSidebarModule } from '@/lib/sidebar-route-guard'

const boolSearchSchema = z.preprocess((value) => {
  if (value === 'true') return true
  if (value === 'false') return false
  return value
}, z.boolean())

const walletSearchSchema = z.object({
  show_history: boolSearchSchema.optional(),
  pay: z.enum(['success', 'pending', 'fail']).optional(),
  payment_provider: z.string().optional(),
  order_type: z.enum(['topup', 'subscription']).optional(),
  trade_no: z.string().optional(),
})

export const Route = createFileRoute('/_authenticated/wallet/')({
  beforeLoad: () =>
    requireSidebarModule({
      section: 'personal',
      module: 'topup',
    }),
  component: RouteComponent,
  validateSearch: walletSearchSchema,
})

function RouteComponent() {
  const search = Route.useSearch()
  return <Wallet paymentReturn={search} />
}
