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

export const TICKET_CATEGORIES = ['客服部门', '财务部门'] as const

export const TICKET_PRIORITIES = ['低', '普通', '高', '紧急'] as const

export const TICKET_STATUSES = [
  '待处理',
  '处理中',
  '等待用户回复',
  '管理员已回复',
  '已解决',
  '已关闭',
] as const

export type TicketCategory = (typeof TICKET_CATEGORIES)[number]
export type TicketPriority = (typeof TICKET_PRIORITIES)[number]
export type TicketStatus = (typeof TICKET_STATUSES)[number]

export type Ticket = {
  id: number
  number: string
  user_id: number
  username: string
  title: string
  category: TicketCategory
  priority: TicketPriority
  status: TicketStatus
  assignee_id?: number
  assignee_name?: string
  last_reply_at: number
  last_reply_by: 'user' | 'admin'
  closed_at?: number
  created_at: number
  updated_at: number
}

export type TicketMessage = {
  id: number
  ticket_id: number
  user_id: number
  username: string
  sender: 'user' | 'admin'
  content: string
  created_at: number
}

export type TicketAttachment = {
  id: number
  ticket_id: number
  message_id: number
  user_id: number
  file_name: string
  storage_name: string
  mime_type: string
  size: number
  width: number
  height: number
  created_at: number
}

export type TicketDetail = {
  ticket: Ticket
  messages: TicketMessage[]
  attachments: TicketAttachment[]
}

export type TicketListResponse = {
  items?: Ticket[]
  total?: number
  page?: number
  page_size?: number
}

export type TicketAttachmentInput = {
  file: File
  previewUrl: string
}
