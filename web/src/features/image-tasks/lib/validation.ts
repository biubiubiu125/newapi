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

export const imageTaskFormSchema = z
  .object({
    tokenId: z.number().int().positive('API Key is required'),
    mode: z.enum(['generation', 'edit']),
    model: z.string().trim().min(1, 'Model is required'),
    prompt: z.string().trim().min(1, 'Prompt is required'),
    n: z.number().int().min(1).max(128),
    size: z.string(),
    quality: z.string(),
    responseFormat: z.enum(['', 'url', 'b64_json']),
    image: z.custom<File | null>(
      (value) =>
        value === null || (typeof File !== 'undefined' && value instanceof File)
    ),
    mask: z.custom<File | null>(
      (value) =>
        value === null || (typeof File !== 'undefined' && value instanceof File)
    ),
  })
  .superRefine((value, context) => {
    if (value.mode === 'edit' && value.image === null) {
      context.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['image'],
        message: 'Image is required for edit tasks',
      })
    }
  })

export type ImageTaskFormValues = z.infer<typeof imageTaskFormSchema>
