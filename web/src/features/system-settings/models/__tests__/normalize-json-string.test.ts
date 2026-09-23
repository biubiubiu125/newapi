/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { expect, it } from 'vitest'

import { parseJsonNumberMap } from '../../utils/json-parser'
import {
  canonicalizeBillingModeMap,
  createInitialLaneState,
  EMPTY_LANE_ENABLED,
  EMPTY_LANE_PRICES,
  pricingDraftCanPersist,
  pricingDraftExpressionIsValid,
  pricingDraftValuesAreValid,
  pricingPromptLaneValuesAreValid,
} from '../model-pricing-core'
import { mergeIncomingJsonObjectExtras, normalizeJsonString } from '../utils'

it('treats object key order as the same JSON value', () => {
  expect(normalizeJsonString('{"b":1,"a":2}')).toBe(
    normalizeJsonString('{"a":2,"b":1}')
  )
  expect(normalizeJsonString('{"outer":{"b":1,"a":2}}')).toBe(
    normalizeJsonString('{"outer":{"a":2,"b":1}}')
  )
})

it('rejects non-finite or negative pricing map values', () => {
  expect(parseJsonNumberMap('{"keep-me":0.1}')).toEqual({ 'keep-me': 0.1 })
  expect(parseJsonNumberMap('{"keep-me":"0.1"}')).toBeNull()
  expect(parseJsonNumberMap('{"keep-me":-1}')).toBeNull()
  expect(parseJsonNumberMap('{"keep-me":null}')).toBeNull()
})

it('adds newly fetched extras without restoring locally deleted saved keys', () => {
  expect(
    JSON.parse(
      mergeIncomingJsonObjectExtras(
        '{"example-model":0.1,"local-model":0.3}',
        '{"example-model":0.1,"extra-model":0.9}',
        '{"example-model":0.1}'
      )
    )
  ).toEqual({
    'example-model': 0.1,
    'local-model': 0.3,
    'extra-model': 0.9,
  })
  expect(
    JSON.parse(
      mergeIncomingJsonObjectExtras(
        '{}',
        '{"example-model":0.1,"extra-model":0.9}',
        '{"example-model":0.1}'
      )
    )
  ).toEqual({
    'extra-model': 0.9,
  })
})

it('keeps ratio-style billing mode aliases as stored ratio keys', () => {
  expect(
    canonicalizeBillingModeMap({
      priced: 'per-request',
      tokens: 'per-token',
      explicit: 'ratio',
      expr: 'tiered_expr',
      unknown: 'other',
    })
  ).toEqual({
    priced: 'ratio',
    tokens: 'ratio',
    explicit: 'ratio',
    expr: 'tiered_expr',
  })
})

it('rejects a zero prompt price with a non-zero dependent lane', () => {
  expect(
    pricingDraftValuesAreValid({
      name: 'example',
      billingMode: 'per-token',
      ratio: '0',
      completionRatio: '0.5',
    })
  ).toBe(false)
  expect(
    pricingDraftValuesAreValid({
      name: 'example',
      ratio: '0',
      completionRatio: '0.5',
    })
  ).toBe(false)
  expect(
    pricingDraftValuesAreValid({
      name: 'example',
      billingMode: 'per-request',
      price: '0.1',
    })
  ).toBe(true)
  expect(
    pricingPromptLaneValuesAreValid({
      billingMode: 'per-token',
      promptPrice: '0',
      laneEnabled: { ...EMPTY_LANE_ENABLED, completion: true },
      lanePrices: { ...EMPTY_LANE_PRICES, completion: '0.5' },
    })
  ).toBe(false)
})

it('rejects persistable drafts when prompt is zero and a dependent lane is enabled', () => {
  expect(
    pricingDraftCanPersist({
      name: 'example',
      billingMode: 'per-token',
      ratio: '0',
      completionRatio: '',
      promptPrice: '0',
      laneEnabled: { ...EMPTY_LANE_ENABLED, completion: true },
      lanePrices: { ...EMPTY_LANE_PRICES, completion: '0.5' },
    })
  ).toBe(false)
  expect(
    pricingDraftCanPersist({
      name: 'example',
      ratio: '0',
      completionRatio: '',
      promptPrice: '0',
      laneEnabled: { ...EMPTY_LANE_ENABLED, completion: true },
      lanePrices: { ...EMPTY_LANE_PRICES, completion: '0.5' },
    })
  ).toBe(false)
  expect(
    pricingDraftCanPersist({
      name: 'example',
      billingMode: 'per-request',
      price: '0.1',
    })
  ).toBe(true)
})

it('restores snapshot prompt and lane state instead of re-deriving from ratios', () => {
  const state = createInitialLaneState({
    name: 'example',
    billingMode: 'per-token',
    ratio: '',
    completionRatio: '',
    promptPrice: '0',
    laneEnabled: { ...EMPTY_LANE_ENABLED, completion: true },
    lanePrices: { ...EMPTY_LANE_PRICES, completion: '0.5' },
  })
  expect(state.promptPrice).toBe('0')
  expect(state.enabled.completion).toBe(true)
  expect(state.prices.completion).toBe('0.5')
})

it('rejects an uncompilable billing expression for persistable drafts', () => {
  expect(
    pricingDraftExpressionIsValid({
      name: 'example',
      billingMode: 'tiered_expr',
      billingExpr: 'not-a-valid-expr',
    })
  ).toBe(false)
  expect(
    pricingDraftExpressionIsValid({
      name: 'example',
      billingMode: 'per-request',
      price: '0.1',
    })
  ).toBe(true)
})
