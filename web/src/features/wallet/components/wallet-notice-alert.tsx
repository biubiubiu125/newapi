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
import { ExternalLink, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

interface WalletNoticeAlertProps {
  notice?: string
  topupLink?: string
}

export function WalletNoticeAlert(props: WalletNoticeAlertProps) {
  const { t } = useTranslation()
  const notice = props.notice?.trim()

  if (!notice) return null

  return (
    <Alert className='border-amber-200 bg-amber-50/80 text-amber-950 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-100'>
      <AlertDescription className='flex flex-col gap-3 text-sm sm:flex-row sm:items-center sm:justify-between'>
        <span className='flex min-w-0 flex-1 items-center gap-2'>
          <Info className='h-4 w-4 shrink-0 text-amber-600 dark:text-amber-300' />
          <span className='min-w-0 whitespace-pre-line break-words leading-relaxed [overflow-wrap:anywhere]'>
            {notice}
          </span>
        </span>
        {props.topupLink ? (
          <Button
            render={
              <a
                href={props.topupLink}
                target='_blank'
                rel='noopener noreferrer'
              />
            }
            variant='outline'
            size='sm'
            className='shrink-0 border-amber-300 bg-background/80 text-amber-950 hover:bg-amber-100 dark:border-amber-800 dark:text-amber-100 dark:hover:bg-amber-900/40'
          >
            <ExternalLink className='mr-2 h-4 w-4' />
            {t('Buy redemption code')}
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  )
}
