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
import type { PaymentOrphanEvent } from './api'

export function paymentOrphanCreditDialogCopy(
  event: Pick<PaymentOrphanEvent, 'provider'>
) {
  if (event.provider.trim().toLowerCase() === 'stripe') {
    return {
      title: 'Credit this Stripe payment?',
      description:
        'This creates the missing top-up or subscription and credits the matched Stripe customer exactly once.',
    }
  }
  return {
    title: 'Credit this verified payment?',
    description:
      'This credits only the matching local top-up or subscription order exactly once. It does not recreate missing orders.',
  }
}
