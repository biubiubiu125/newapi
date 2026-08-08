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
import { Link, useSearch } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import {
  removeAffiliateCode,
  saveAffiliateCode,
} from '@/features/auth/lib/storage'
import { useStatus } from '@/hooks/use-status'

import { AuthLayout } from '../auth-layout'
import { SignUpForm } from './components/sign-up-form'

export function SignUp() {
  const { t } = useTranslation()
  const search = useSearch({ from: '/(auth)/sign-up' })
  const { status } = useStatus()
  const referralError = (search.referral_error || '').trim()
  const referralCookieTTLDays =
    Number(
      status?.referral_cookie_ttl_days ?? status?.data?.referral_cookie_ttl_days
    ) || undefined

  useEffect(() => {
    if (referralError === 'invalid') {
      removeAffiliateCode()
      return
    }
    const code = (search.aff || '').trim()
    if (code) {
      saveAffiliateCode(code, referralCookieTTLDays)
    }
  }, [referralCookieTTLDays, referralError, search.aff])

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {t('Create an account')}
          </h2>
          {referralError === 'invalid' && (
            <p className='text-sm text-amber-600'>
              {t(
                'This referral link is invalid or no longer available. You can still sign up without it.'
              )}
            </p>
          )}
          <p className='text-muted-foreground text-left text-sm sm:text-base'>
            {t('Already have an account?')}{' '}
            <Link
              to='/sign-in'
              className='hover:text-primary font-medium underline underline-offset-4'
            >
              {t('Sign in')}
            </Link>
            .
          </p>
        </div>

        <SignUpForm />
      </div>
    </AuthLayout>
  )
}
