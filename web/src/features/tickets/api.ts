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
import { api } from '@/lib/api'

import type {
  TicketCategory,
  TicketDetail,
  TicketListResponse,
  TicketPriority,
  TicketStatus,
} from './types'

type ApiResponse<T = unknown> = {
  success?: boolean
  message?: string
  data?: T
}

type ListTicketsParams = {
  admin?: boolean
  page?: number
  pageSize?: number
  status?: string
  category?: string
  priority?: string
  assigneeId?: number
  startTime?: number
  endTime?: number
  keyword?: string
}

type TicketCreatePayload = {
  title: string
  category: TicketCategory
  priority: TicketPriority
  content: string
  attachments?: File[]
}

type TicketReplyPayload = {
  content: string
  attachments?: File[]
}

type TicketUpdatePayload = {
  category?: TicketCategory
  priority?: TicketPriority
  status?: TicketStatus
  assignee_id?: number
  assignee_name?: string
}

function ticketBase(admin?: boolean) {
  return admin ? '/api/user/admin/tickets' : '/api/user/tickets'
}

function appendFiles(form: FormData, files?: File[]) {
  files?.forEach((file) => form.append('attachments', file))
}

function unwrapTicketResponse<T>(res: ApiResponse<T>): T {
  if (!res?.success) {
    throw new Error(res?.message || '工单请求失败')
  }
  return res.data as T
}

export async function listTickets(params: ListTicketsParams) {
  const query = {
    p: params.page ?? 1,
    page_size: params.pageSize ?? 20,
    status: params.status || undefined,
    category: params.category || undefined,
    priority: params.priority || undefined,
    assignee_id:
      params.assigneeId !== undefined ? String(params.assigneeId) : undefined,
    start_time:
      params.startTime !== undefined ? String(params.startTime) : undefined,
    end_time: params.endTime !== undefined ? String(params.endTime) : undefined,
    keyword: params.keyword || undefined,
  }
  const res = await api.get(ticketBase(params.admin), { params: query })
  return unwrapTicketResponse<TicketListResponse>(res.data)
}

export async function getTicket(ticketId: number, admin?: boolean) {
  const res = await api.get(`${ticketBase(admin)}/${ticketId}`)
  return unwrapTicketResponse<TicketDetail>(res.data)
}

export async function createTicket(payload: TicketCreatePayload) {
  const form = new FormData()
  form.append('title', payload.title)
  form.append('category', payload.category)
  form.append('priority', payload.priority)
  form.append('content', payload.content)
  appendFiles(form, payload.attachments)
  const res = await api.post('/api/user/tickets', form)
  return unwrapTicketResponse<TicketDetail['ticket']>(res.data)
}

export async function replyTicket(
  ticketId: number,
  payload: TicketReplyPayload,
  admin?: boolean
) {
  const form = new FormData()
  form.append('content', payload.content)
  appendFiles(form, payload.attachments)
  const res = await api.post(`${ticketBase(admin)}/${ticketId}/reply`, form)
  return unwrapTicketResponse(res.data)
}

export async function closeTicket(ticketId: number, admin?: boolean) {
  const res = await api.post(`${ticketBase(admin)}/${ticketId}/close`)
  return unwrapTicketResponse(res.data)
}

export async function reopenTicket(ticketId: number, admin?: boolean) {
  const res = await api.post(`${ticketBase(admin)}/${ticketId}/reopen`)
  return unwrapTicketResponse(res.data)
}

export async function updateTicket(
  ticketId: number,
  payload: TicketUpdatePayload
) {
  const res = await api.put(`/api/user/admin/tickets/${ticketId}`, payload)
  return unwrapTicketResponse(res.data)
}

export function ticketAttachmentUrl(
  ticketId: number,
  attachmentId: number,
  admin?: boolean
) {
  return `${ticketBase(admin)}/${ticketId}/attachments/${attachmentId}`
}

export async function fetchTicketAttachmentBlob(
  ticketId: number,
  attachmentId: number,
  admin?: boolean
) {
  const res = await api.get(
    ticketAttachmentUrl(ticketId, attachmentId, admin),
    {
      responseType: 'blob',
      disableDuplicate: true,
    }
  )
  return res.data as Blob
}

export async function getTicketBadge(
  admin?: boolean,
  params?: URLSearchParams
) {
  const query = params?.toString()
  const res = await api.get(
    `${ticketBase(admin)}/badge${query ? `?${query}` : ''}`
  )
  return unwrapTicketResponse<{
    count: number
    new_count?: number
    latest_cursor?: string
  }>(res.data)
}
