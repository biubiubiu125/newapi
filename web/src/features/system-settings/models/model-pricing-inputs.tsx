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
import { useTranslation } from 'react-i18next'

import { InputGroup, InputGroupAddon } from '@/components/ui/input-group'
import {
  USD_PRICING_CURRENCY,
  type PricingCurrency,
} from '@/features/model-pricing/currency'
import { PricingAmountInput } from '@/features/model-pricing/pricing-amount-input'
import { cn } from '@/lib/utils'

import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsSwitchField,
} from '../components/settings-form-layout'

export function PriceInput(props: {
  value: string
  placeholder?: string
  disabled?: boolean
  onChange: (value: string) => void
  currency?: PricingCurrency
  id?: string
  'aria-describedby'?: string
  'aria-label'?: string
}) {
  const currency = props.currency ?? USD_PRICING_CURRENCY
  return (
    <InputGroup className='has-[[data-pricing-error]]:h-auto has-[[data-pricing-error]]:flex-wrap'>
      <InputGroupAddon>{currency.symbol}</InputGroupAddon>
      <PricingAmountInput
        grouped
        id={props.id}
        aria-describedby={props['aria-describedby']}
        aria-label={props['aria-label']}
        currency={currency}
        value={props.value}
        placeholder={props.placeholder}
        disabled={props.disabled}
        onChange={props.onChange}
      />
      <InputGroupAddon align='inline-end'>$/1M</InputGroupAddon>
    </InputGroup>
  )
}

export function PriceLane(props: {
  title: string
  description: string
  placeholder: string
  value: string
  enabled: boolean
  disabled?: boolean
  disabledReason?: string
  compact?: boolean
  currency?: PricingCurrency
  onEnabledChange: (checked: boolean) => void
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const disabled = Boolean(props.disabled || !props.enabled)
  const description =
    props.disabled && props.disabledReason
      ? props.disabledReason
      : props.compact
        ? undefined
        : props.description

  return (
    <SettingsControlGroup
      className={cn(
        'space-y-3',
        disabled && 'opacity-75',
        props.compact && 'rounded-lg border bg-transparent p-3'
      )}
      data-disabled={disabled || undefined}
    >
      <SettingsSwitchField
        checked={props.enabled}
        disabled={props.disabled}
        onCheckedChange={props.onEnabledChange}
        label={props.title}
        description={description}
        aria-label={props.title}
      />
      <SettingsControlChildren className='space-y-2'>
        <PriceInput
          aria-label={props.title}
          currency={props.currency}
          value={props.value}
          placeholder={props.placeholder}
          disabled={disabled}
          onChange={props.onChange}
        />
        {!props.compact && (
          <p className='text-muted-foreground text-xs'>
            {t('{{currency}} price per 1M tokens.', {
              currency: (props.currency ?? USD_PRICING_CURRENCY).label,
            })}
          </p>
        )}
      </SettingsControlChildren>
    </SettingsControlGroup>
  )
}
