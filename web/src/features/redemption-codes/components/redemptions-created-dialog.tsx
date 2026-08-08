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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatQuota } from '@/lib/format'

type CreatedRedemptionsDialogProps = {
  open: boolean
  codes: string[]
  quota: number
  onOpenChange: (open: boolean) => void
}

export function CreatedRedemptionsDialog(props: CreatedRedemptionsDialogProps) {
  const { t } = useTranslation()
  const copyValue = useMemo(() => props.codes.join('\n'), [props.codes])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-[560px]'>
        <DialogHeader>
          <DialogTitle>{t('Created Redemption Codes')}</DialogTitle>
          <DialogDescription>
            {t(
              'Review the generated redemption codes before closing this dialog.'
            )}
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className='max-h-[45vh] rounded-lg border'>
          <div className='divide-border divide-y'>
            {props.codes.map((code) => (
              <div
                key={code}
                className='grid gap-2 p-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'
              >
                <code className='bg-muted/70 min-w-0 rounded-md px-2 py-1 font-mono text-xs break-all'>
                  {code}
                </code>
                <StatusBadge
                  label={formatQuota(props.quota)}
                  variant='neutral'
                  copyable={false}
                  className='w-fit'
                />
              </div>
            ))}
          </div>
        </ScrollArea>
        <DialogFooter>
          <CopyButton
            value={copyValue}
            variant='default'
            size='default'
            tooltip={t('Copy all redemption codes')}
            successTooltip={t('Copied!')}
            aria-label={t('Copy all redemption codes')}
          >
            {t('Copy all redemption codes')}
          </CopyButton>
          <DialogClose render={<Button variant='outline' />}>
            {t('Close')}
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
