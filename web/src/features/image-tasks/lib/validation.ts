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
import { z } from 'zod'

export const MAX_IMAGE_TASK_COUNT = 10
export const MAX_IMAGE_TASK_REFERENCE_IMAGES = 6
export const CUSTOM_IMAGE_TASK_SIZE_VALUE = 'custom'

export const IMAGE_TASK_SIZE_OPTIONS = [
  { value: '1024x1024', labelKey: '1K square' },
  { value: '1536x1024', labelKey: '1K landscape' },
  { value: '1024x1536', labelKey: '1K portrait' },
  { value: '2560x1440', labelKey: '2K landscape' },
  { value: '1440x2560', labelKey: '2K portrait' },
  { value: '3824x2144', labelKey: '4K landscape' },
  { value: '2144x3824', labelKey: '4K portrait' },
] as const

const IMAGE_TASK_SIZE_VALUES = new Set(
  IMAGE_TASK_SIZE_OPTIONS.map((option) => option.value)
)
const TWO_K_PIXELS = 2560 * 1440

function isFile(value: unknown): value is File {
  return typeof File !== 'undefined' && value instanceof File
}

function parsePositiveInteger(value: string): number | null {
  if (!/^\d+$/.test(value.trim())) return null
  const parsed = Number.parseInt(value.trim(), 10)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null
}

function parseImageTaskSize(
  size: string
): { width: number; height: number } | null {
  const [widthValue, heightValue, extra] = size.split('x')
  if (extra !== undefined || !widthValue || !heightValue) return null
  const width = parsePositiveInteger(widthValue)
  const height = parsePositiveInteger(heightValue)
  if (!width || !height) return null
  return { width, height }
}

export function getImageTaskRequestSize(
  values: Pick<ImageTaskFormValues, 'size' | 'customWidth' | 'customHeight'>
): string {
  if (values.size === CUSTOM_IMAGE_TASK_SIZE_VALUE) {
    return `${values.customWidth.trim()}x${values.customHeight.trim()}`
  }
  return values.size.trim()
}

export function isLargeImageTaskSize(size: string): boolean {
  const parsed = parseImageTaskSize(size)
  if (!parsed) return false
  return parsed.width * parsed.height >= TWO_K_PIXELS
}

export const imageTaskFormSchema = z
  .object({
    tokenId: z.number().int().positive('API Key is required'),
    mode: z.enum(['generation', 'edit']),
    model: z.string().trim().min(1, 'Model is required'),
    prompt: z.string().trim().min(1, 'Prompt is required'),
    n: z
      .number({ error: 'Count must be between 1 and 10' })
      .int('Count must be between 1 and 10')
      .min(1, 'Count must be between 1 and 10')
      .max(MAX_IMAGE_TASK_COUNT, 'Count must be between 1 and 10'),
    size: z.string().trim().min(1, 'Size is required'),
    quality: z.string().trim().min(1, 'Quality is required'),
    images: z
      .array(z.custom<File>(isFile))
      .max(MAX_IMAGE_TASK_REFERENCE_IMAGES, 'You can upload up to 6 images'),
    mask: z.custom<File | null>((value) => value === null || isFile(value)),
    customWidth: z.string().trim(),
    customHeight: z.string().trim(),
  })
  .superRefine((value, context) => {
    if (
      value.size !== CUSTOM_IMAGE_TASK_SIZE_VALUE &&
      !IMAGE_TASK_SIZE_VALUES.has(
        value.size as (typeof IMAGE_TASK_SIZE_OPTIONS)[number]['value']
      )
    ) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['size'],
        message: 'Size is invalid',
      })
    }
    if (value.size === CUSTOM_IMAGE_TASK_SIZE_VALUE) {
      const width = parsePositiveInteger(value.customWidth)
      const height = parsePositiveInteger(value.customHeight)
      if (!width) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['customWidth'],
          message: 'Width must be a positive integer',
        })
      } else if (width % 16 !== 0) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['customWidth'],
          message: 'Width must be a multiple of 16',
        })
      }
      if (!height) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['customHeight'],
          message: 'Height must be a positive integer',
        })
      } else if (height % 16 !== 0) {
        context.addIssue({
          code: z.ZodIssueCode.custom,
          path: ['customHeight'],
          message: 'Height must be a multiple of 16',
        })
      }
      if (width && height) {
        const ratio = width / height
        if (ratio > 3 || ratio < 1 / 3) {
          context.addIssue({
            code: z.ZodIssueCode.custom,
            path: ['size'],
            message: 'Size ratio must be between 1:3 and 3:1',
          })
        }
      }
    }
    if (value.mode === 'edit' && value.images.length === 0) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['images'],
        message: 'At least one image is required for image-to-image',
      })
    }
  })

export type ImageTaskFormValues = z.infer<typeof imageTaskFormSchema>
