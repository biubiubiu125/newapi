import { describe, expect, test } from 'vitest'
import type { TFunction } from 'i18next'

import type { SubscriptionPlan } from '../../types'
import {
  PLAN_FORM_DEFAULTS,
  convertPlanPriceAmount,
  formValuesToPlanPayload,
  getPlanFormSchema,
  planToFormValues,
} from '../plan-form'

const t = ((key: string) => key) as TFunction

function plan(overrides: Partial<SubscriptionPlan> = {}): SubscriptionPlan {
  return {
    id: 1,
    title: 'Pro',
    price_amount: 9.99,
    currency: 'USD',
    duration_unit: 'month',
    duration_value: 1,
    enabled: true,
    sort_order: 0,
    max_purchase_per_user: 0,
    total_amount: 0,
    ...overrides,
  }
}

describe('subscription plan form', () => {
  test('keeps the stored currency and price on a round trip', () => {
    const values = planToFormValues(plan({ currency: 'USD', price_amount: 9.99 }))
    const payload = formValuesToPlanPayload(values)

    expect(values.currency).toBe('USD')
    expect(payload.plan.currency).toBe('USD')
    expect(payload.plan.price_amount).toBe(9.99)
  })

  test('treats a missing currency as the database default USD', () => {
    expect(planToFormValues(plan({ currency: '' })).currency).toBe('USD')
    expect(formValuesToPlanPayload(PLAN_FORM_DEFAULTS).plan.currency).toBe('CNY')
  })

  test('converts price only when the admin changes currency', () => {
    expect(convertPlanPriceAmount(10, 'USD', 'CNY', 7.2)).toBe(72)
    expect(convertPlanPriceAmount(72, 'CNY', 'USD', 7.2)).toBe(10)
    expect(convertPlanPriceAmount(10, 'USD', 'CNY', 0)).toBeNull()
  })

  test('rejects a custom duration or reset cycle of zero', () => {
    const schema = getPlanFormSchema(t)
    const base = { ...PLAN_FORM_DEFAULTS, title: 'Pro', currency: 'CNY' as const }

    expect(
      schema.safeParse({
        ...base,
        duration_unit: 'custom',
        custom_seconds: 0,
      }).success
    ).toBe(false)
    expect(
      schema.safeParse({
        ...base,
        duration_unit: 'custom',
        custom_seconds: 60,
      }).success
    ).toBe(true)
    expect(
      schema.safeParse({
        ...base,
        quota_reset_period: 'custom',
        quota_reset_custom_seconds: 0,
      }).success
    ).toBe(false)
  })
})
