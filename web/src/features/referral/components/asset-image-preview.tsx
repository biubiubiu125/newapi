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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

type AssetImagePreviewProps = {
  url?: string
  label: string
  thumbnailClassName?: string
}

export function AssetImagePreview({
  url,
  label,
  thumbnailClassName,
}: AssetImagePreviewProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)

  if (!url) {
    return <span>-</span>
  }

  return (
    <>
      <button
        type='button'
        className={cn(
          'border-input bg-background inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-md border text-xs text-muted-foreground',
          failed ? 'cursor-default px-2 text-center' : 'cursor-zoom-in',
          thumbnailClassName
        )}
        title={failed ? t('Image unavailable') : t('Click to enlarge')}
        onClick={() => {
          if (!failed) {
            setOpen(true)
          }
        }}
      >
        {failed ? (
          t('Image unavailable')
        ) : (
          <img
            src={url}
            alt={label}
            className='h-full w-full object-contain'
            onError={() => setFailed(true)}
          />
        )}
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-h-[90vh] overflow-hidden sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{label || t('Image Preview')}</DialogTitle>
          </DialogHeader>
          <div className='flex max-h-[calc(90vh-6rem)] items-center justify-center overflow-auto rounded-md border bg-background p-3'>
            <img src={url} alt={label} className='max-h-full max-w-full' />
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
