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
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'
import {
  CheckCircle2,
  Check,
  ChevronsUpDown,
  ImagePlus,
  LifeBuoy,
  Lock,
  MessageCircleReply,
  Plus,
  RefreshCw,
  RotateCcw,
  Send,
  Upload,
  X,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import { searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import { useDebounce } from '@/hooks'
import dayjs from '@/lib/dayjs'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'

import {
  closeTicket,
  createTicket,
  fetchTicketAttachmentBlob,
  getTicket,
  listTickets,
  reopenTicket,
  replyTicket,
  updateTicket,
} from './api'
import {
  TICKET_CATEGORIES,
  TICKET_PRIORITIES,
  TICKET_STATUSES,
  type Ticket,
  type TicketAttachment,
  type TicketAttachmentInput,
  type TicketCategory,
  type TicketListResponse,
  type TicketPriority,
  type TicketStatus,
} from './types'

const MAX_IMAGE_SIZE = 5 * 1024 * 1024
const MAX_REPLY_IMAGES = 5
const TICKET_LIST_PAGE_SIZE = 50
const ACCEPTED_IMAGE_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp'])

type BadgeVariant = 'default' | 'secondary' | 'outline'
type TicketsPageMode = 'user' | 'admin'
type TicketSheet = 'detail' | 'create' | null

const statusVariants: Record<TicketStatus, BadgeVariant> = {
  待处理: 'default',
  处理中: 'default',
  等待用户回复: 'secondary',
  管理员已回复: 'secondary',
  已解决: 'outline',
  已关闭: 'outline',
}

const priorityVariants: Record<TicketPriority, BadgeVariant> = {
  低: 'outline',
  普通: 'secondary',
  高: 'default',
  紧急: 'default',
}

function formatTime(timestamp?: number) {
  if (!timestamp) return '-'
  return dayjs(timestamp * 1000).format('YYYY-MM-DD HH:mm')
}

function validateImageFile(file: File) {
  if (!ACCEPTED_IMAGE_TYPES.has(file.type)) {
    return 'Only png, jpg, jpeg, and webp images are supported'
  }
  if (file.size > MAX_IMAGE_SIZE) {
    return 'Each image must be 5MB or smaller'
  }
  return ''
}

function revokePreviews(files: TicketAttachmentInput[]) {
  files.forEach((item) => URL.revokeObjectURL(item.previewUrl))
}

function TicketSelect<T extends string>({
  value,
  options,
  onValueChange,
  placeholder,
}: {
  value: T
  options: readonly T[]
  onValueChange: (value: T) => void
  placeholder?: string
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={value}
      onValueChange={(next) => {
        if (next) onValueChange(next as T)
      }}
    >
      <SelectTrigger className='w-full'>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent>
        {options.map((option) => (
          <SelectItem key={option} value={option}>
            {t(option)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function AttachmentPicker({
  files,
  setFiles,
  disabled,
}: {
  files: TicketAttachmentInput[]
  setFiles: (files: TicketAttachmentInput[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLInputElement | null>(null)

  const addFiles = (incoming: File[]) => {
    const accepted: TicketAttachmentInput[] = []
    for (const file of incoming) {
      const error = validateImageFile(file)
      if (error) {
        toast.error(t(error))
        continue
      }
      if (files.length + accepted.length >= MAX_REPLY_IMAGES) {
        toast.error(
          t('You can upload at most {{count}} images at a time', {
            count: MAX_REPLY_IMAGES,
          })
        )
        break
      }
      accepted.push({ file, previewUrl: URL.createObjectURL(file) })
    }
    if (accepted.length > 0) setFiles([...files, ...accepted])
  }

  const removeFile = (index: number) => {
    const next = [...files]
    const [removed] = next.splice(index, 1)
    if (removed) URL.revokeObjectURL(removed.previewUrl)
    setFiles(next)
  }

  return (
    <div className='space-y-2'>
      <div className='flex flex-wrap items-center gap-2'>
        <input
          ref={inputRef}
          className='hidden'
          type='file'
          accept='image/png,image/jpeg,image/webp'
          multiple
          onChange={(event) => {
            addFiles([...(event.target.files ?? [])])
            event.target.value = ''
          }}
        />
        <Button
          type='button'
          variant='outline'
          size='sm'
          disabled={disabled}
          onClick={() => inputRef.current?.click()}
        >
          <Upload className='h-4 w-4' />
          {t('Add images')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t(
            'You can paste images. Each image must be 5MB or smaller, and you can upload at most 5 at a time.'
          )}
        </span>
      </div>
      {files.length > 0 && (
        <div className='grid grid-cols-2 gap-2 sm:grid-cols-5'>
          {files.map((item, index) => (
            <div
              key={`${item.file.name}-${index}`}
              className='border-border relative aspect-square overflow-hidden rounded-md border'
            >
              <img
                src={item.previewUrl}
                alt={item.file.name}
                className='size-full object-cover'
              />
              <Button
                type='button'
                size='icon-xs'
                variant='secondary'
                className='absolute top-1 right-1'
                onClick={() => removeFile(index)}
              >
                <X className='h-3 w-3' />
              </Button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function TicketListItem({
  ticket,
  selected,
  onSelect,
  showUser,
}: {
  ticket: Ticket
  selected: boolean
  onSelect: () => void
  showUser?: boolean
}) {
  const { t } = useTranslation()
  return (
    <button
      type='button'
      className={`hover:bg-muted/70 flex w-full flex-col gap-2 border-b px-3 py-3 text-left transition ${
        selected ? 'bg-muted' : ''
      }`}
      onClick={onSelect}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0'>
          <div className='truncate text-sm font-medium'>{ticket.title}</div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {ticket.number} · {t(ticket.category)}
          </div>
        </div>
        <Badge variant={statusVariants[ticket.status]}>
          {t(ticket.status)}
        </Badge>
      </div>
      <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
        <Badge variant={priorityVariants[ticket.priority]}>
          {t(ticket.priority)}
        </Badge>
        <span>{formatTime(ticket.updated_at)}</span>
        {showUser && ticket.username && <span>{ticket.username}</span>}
      </div>
    </button>
  )
}

function CreateTicketPanel({
  onCreated,
}: {
  onCreated: (ticket: Ticket) => void
}) {
  const { t } = useTranslation()
  const [title, setTitle] = useState('')
  const [category, setCategory] = useState<TicketCategory>('客服部门')
  const [priority, setPriority] = useState<TicketPriority>('普通')
  const [content, setContent] = useState('')
  const [files, setFiles] = useState<TicketAttachmentInput[]>([])
  const filesRef = useRef<TicketAttachmentInput[]>([])

  useEffect(() => {
    filesRef.current = files
  }, [files])

  useEffect(() => () => revokePreviews(filesRef.current), [])

  const createMutation = useMutation({
    mutationFn: () =>
      createTicket({
        title: title.trim(),
        category,
        priority,
        content: content.trim(),
        attachments: files.map((item) => item.file),
      }),
    onSuccess: (ticket) => {
      toast.success(t('Ticket created'))
      revokePreviews(files)
      setTitle('')
      setCategory('客服部门')
      setPriority('普通')
      setContent('')
      setFiles([])
      filesRef.current = []
      onCreated(ticket)
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to create ticket')),
  })

  const submit = () => {
    if (!title.trim()) {
      toast.error(t('Please enter the ticket title'))
      return
    }
    if (!content.trim()) {
      toast.error(t('Please enter the ticket content'))
      return
    }
    createMutation.mutate()
  }

  return (
    <div className='border-border flex h-full min-h-0 flex-col rounded-md border'>
      <div className='border-b px-4 py-3'>
        <div className='flex items-center gap-2 font-medium'>
          <Plus className='h-4 w-4' />
          {t('Create Ticket')}
        </div>
      </div>
      <div className='flex-1 space-y-4 overflow-y-auto p-4'>
        <div className='grid gap-2'>
          <Label>{t('Title')}</Label>
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            maxLength={200}
            placeholder={t('Briefly describe the issue')}
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-2'>
            <Label>{t('Category')}</Label>
            <TicketSelect
              value={category}
              options={TICKET_CATEGORIES}
              onValueChange={setCategory}
            />
          </div>
          <div className='grid gap-2'>
            <Label>{t('Priority')}</Label>
            <TicketSelect
              value={priority}
              options={TICKET_PRIORITIES}
              onValueChange={setPriority}
            />
          </div>
        </div>
        <div
          className='grid gap-2'
          onPaste={(event) => {
            const pasted = [...event.clipboardData.files]
            if (pasted.length > 0) {
              event.preventDefault()
              const accepted = pasted.filter((file) =>
                file.type.startsWith('image/')
              )
              if (accepted.length === 0) {
                toast.error(t('Pasted content is not an image'))
                return
              }
              const next = [...files]
              accepted.forEach((file) => {
                const error = validateImageFile(file)
                if (error) {
                  toast.error(t(error))
                  return
                }
                if (next.length >= MAX_REPLY_IMAGES) {
                  toast.error(
                    t('You can upload at most {{count}} images at a time', {
                      count: MAX_REPLY_IMAGES,
                    })
                  )
                  return
                }
                next.push({ file, previewUrl: URL.createObjectURL(file) })
              })
              setFiles(next)
            }
          }}
        >
          <Label>{t('Content')}</Label>
          <Textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            className='min-h-40 resize-none'
            placeholder={t(
              'Enter the issue details. You can paste images directly.'
            )}
          />
          <AttachmentPicker files={files} setFiles={setFiles} />
        </div>
      </div>
      <div className='bg-muted/40 border-t p-4'>
        <Button
          className='w-full sm:w-auto'
          disabled={createMutation.isPending}
          onClick={submit}
        >
          <Send className='h-4 w-4' />
          {createMutation.isPending ? t('Submitting...') : t('Submit Ticket')}
        </Button>
      </div>
    </div>
  )
}

function TicketAttachments({
  ticketId,
  messageId,
  attachments,
  adminMode,
}: {
  ticketId: number
  messageId: number
  attachments: TicketAttachment[]
  adminMode: boolean
}) {
  const { t } = useTranslation()
  const items = useMemo(
    () => attachments.filter((item) => item.message_id === messageId),
    [attachments, messageId]
  )
  const queryClient = useQueryClient()
  const [blobUrls, setBlobUrls] = useState<Record<number, string>>({})

  useEffect(() => {
    let alive = true
    const urls: Record<number, string> = {}

    async function loadAttachments() {
      await Promise.all(
        items.map(async (item) => {
          const blob = await queryClient.fetchQuery({
            queryKey: ['ticket-attachment-blob', adminMode, ticketId, item.id],
            queryFn: () =>
              fetchTicketAttachmentBlob(ticketId, item.id, adminMode),
            staleTime: 5 * 60 * 1000,
          })
          if (!alive) return
          urls[item.id] = URL.createObjectURL(blob)
        })
      )
      if (alive) setBlobUrls(urls)
    }

    void loadAttachments().catch(() => {
      if (alive) setBlobUrls({})
      toast.error(t('Failed to load attachments'))
    })

    return () => {
      alive = false
      Object.values(urls).forEach((url) => URL.revokeObjectURL(url))
    }
  }, [adminMode, items, queryClient, ticketId])

  if (items.length === 0) return null
  return (
    <div className='mt-3 grid grid-cols-2 gap-2 sm:grid-cols-4'>
      {items.map((item) => (
        <a
          key={item.id}
          href={blobUrls[item.id] || undefined}
          target='_blank'
          rel='noreferrer'
          className='border-border bg-background text-muted-foreground flex aspect-square items-center justify-center overflow-hidden rounded-md border text-xs'
        >
          {blobUrls[item.id] ? (
            <img
              src={blobUrls[item.id]}
              alt={item.file_name}
              className='size-full object-cover'
            />
          ) : (
            t('Loading')
          )}
        </a>
      ))}
    </div>
  )
}

function AssigneeSelect({
  ticket,
  disabled,
  onSelect,
}: {
  ticket: Ticket
  disabled?: boolean
  onSelect: (payload: { assignee_id: number; assignee_name: string }) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const currentLabel =
    ticket.assignee_id && ticket.assignee_name
      ? `#${ticket.assignee_id} ${ticket.assignee_name}`
      : ticket.assignee_name || t('Unassigned')

  const assigneesQuery = useQuery({
    queryKey: ['ticket-assignees', debouncedKeyword],
    enabled: open,
    queryFn: async () => {
      const params = {
        keyword: debouncedKeyword.trim(),
        status: '1',
        page_size: 20,
      }
      const [admins, roots] = await Promise.all([
        searchUsers({ ...params, role: String(ROLE.ADMIN) }),
        searchUsers({ ...params, role: String(ROLE.SUPER_ADMIN) }),
      ])
      const map = new Map<number, User>()
      ;[...(admins.data?.items ?? []), ...(roots.data?.items ?? [])].forEach(
        (user) => map.set(user.id, user)
      )
      return [...map.values()].sort((a, b) => b.role - a.role)
    },
  })

  const users = assigneesQuery.data ?? []

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button
            type='button'
            variant='outline'
            role='combobox'
            disabled={disabled}
            className='w-full justify-between'
          />
        }
      >
        <span className='min-w-0 truncate'>{currentLabel}</span>
        <ChevronsUpDown className='h-4 w-4 shrink-0 opacity-50' />
      </PopoverTrigger>
      <PopoverContent
        className='w-[var(--anchor-width)] overflow-hidden p-0'
        onWheel={(event) => event.stopPropagation()}
        onTouchMove={(event) => event.stopPropagation()}
      >
        <Command shouldFilter={false}>
          <CommandInput
            value={keyword}
            onValueChange={setKeyword}
            placeholder={t('Search admins')}
          />
          <CommandList>
            <CommandEmpty>
              {assigneesQuery.isFetching
                ? t('Searching...')
                : t('No assignable users')}
            </CommandEmpty>
            <CommandGroup>
              <CommandItem
                value='0'
                onSelect={() => {
                  onSelect({ assignee_id: 0, assignee_name: '' })
                  setOpen(false)
                  setKeyword('')
                }}
              >
                <Check
                  className={cn(
                    'h-4 w-4',
                    !ticket.assignee_id ? 'opacity-100' : 'opacity-0'
                  )}
                />
                <span>{t('Unassigned')}</span>
              </CommandItem>
              {users.map((user) => (
                <CommandItem
                  key={user.id}
                  value={`${user.id}-${user.username}`}
                  onSelect={() => {
                    onSelect({
                      assignee_id: user.id,
                      assignee_name: user.username,
                    })
                    setOpen(false)
                    setKeyword('')
                  }}
                >
                  <Check
                    className={cn(
                      'h-4 w-4',
                      ticket.assignee_id === user.id
                        ? 'opacity-100'
                        : 'opacity-0'
                    )}
                  />
                  <span className='min-w-0 flex-1 truncate'>
                    #{user.id} {user.username}
                  </span>
                  <Badge variant='outline'>
                    {user.role >= ROLE.SUPER_ADMIN
                      ? t('Super Admin')
                      : t('Admin')}
                  </Badge>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function TicketDetailPanel({
  ticketId,
  adminMode,
  onChanged,
}: {
  ticketId?: number
  adminMode: boolean
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [reply, setReply] = useState('')
  const [files, setFiles] = useState<TicketAttachmentInput[]>([])
  const filesRef = useRef<TicketAttachmentInput[]>([])

  useEffect(() => {
    filesRef.current = files
  }, [files])

  useEffect(() => () => revokePreviews(filesRef.current), [])

  const detailQuery = useQuery({
    queryKey: ['ticket-detail', adminMode, ticketId],
    enabled: Boolean(ticketId),
    queryFn: () => getTicket(ticketId!, adminMode),
  })

  const ticket = detailQuery.data?.ticket
  const messages = detailQuery.data?.messages ?? []
  const attachments = detailQuery.data?.attachments ?? []
  const isClosed = ticket?.status === '已关闭'

  const refreshDetail = () => {
    queryClient.invalidateQueries({ queryKey: ['tickets'] })
    queryClient.invalidateQueries({
      queryKey: ['ticket-detail', adminMode, ticketId],
    })
    onChanged()
  }

  const replyMutation = useMutation({
    mutationFn: () =>
      replyTicket(
        ticketId!,
        { content: reply.trim(), attachments: files.map((item) => item.file) },
        adminMode
      ),
    onSuccess: () => {
      toast.success(t('Reply sent'))
      revokePreviews(files)
      setReply('')
      setFiles([])
      filesRef.current = []
      refreshDetail()
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to send reply')),
  })

  const closeMutation = useMutation({
    mutationFn: () => closeTicket(ticketId!, adminMode),
    onSuccess: () => {
      toast.success(t('Ticket closed'))
      refreshDetail()
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to close ticket')),
  })

  const reopenMutation = useMutation({
    mutationFn: () => reopenTicket(ticketId!, adminMode),
    onSuccess: () => {
      toast.success(t('Ticket reopened'))
      refreshDetail()
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to reopen ticket')),
  })

  const updateMutation = useMutation({
    mutationFn: (payload: {
      category?: TicketCategory
      priority?: TicketPriority
      status?: TicketStatus
      assignee_id?: number
      assignee_name?: string
    }) => updateTicket(ticketId!, payload),
    onSuccess: () => {
      toast.success(t('Ticket updated'))
      refreshDetail()
    },
    onError: (error: Error) =>
      toast.error(error.message || t('Failed to update ticket')),
  })

  if (!ticketId) {
    return (
      <div className='border-border text-muted-foreground flex h-full min-h-[360px] items-center justify-center rounded-md border'>
        {t('Select a ticket')}
      </div>
    )
  }

  if (detailQuery.isLoading || !ticket) {
    return (
      <div className='border-border text-muted-foreground flex h-full min-h-[360px] items-center justify-center rounded-md border'>
        {detailQuery.isError
          ? t('Ticket does not exist or you do not have access')
          : t('Loading ticket...')}
      </div>
    )
  }

  return (
    <div className='border-border flex h-full min-h-0 flex-col rounded-md border'>
      <div className='border-b p-4'>
        <div className='flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between'>
          <div className='min-w-0'>
            <div className='flex flex-wrap items-center gap-2'>
              <h2 className='truncate text-base font-semibold'>
                {ticket.title}
              </h2>
              <Badge variant={statusVariants[ticket.status]}>
                {t(ticket.status)}
              </Badge>
              <Badge variant={priorityVariants[ticket.priority]}>
                {t(ticket.priority)}
              </Badge>
            </div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {ticket.number} · {t(ticket.category)} ·{' '}
              {t('Created at {{time}}', {
                time: formatTime(ticket.created_at),
              })}
              {adminMode &&
                ` · ${t('User')} ${ticket.username || ticket.user_id}`}
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() => detailQuery.refetch()}
            >
              <RefreshCw className='h-4 w-4' />
              {t('Refresh')}
            </Button>
            {isClosed ? (
              <Button
                size='sm'
                variant='secondary'
                onClick={() => reopenMutation.mutate()}
                disabled={reopenMutation.isPending}
              >
                <RotateCcw className='h-4 w-4' />
                {t('Reopen')}
              </Button>
            ) : (
              <Button
                size='sm'
                variant='outline'
                onClick={() => closeMutation.mutate()}
                disabled={closeMutation.isPending}
              >
                <Lock className='h-4 w-4' />
                {t('Close Ticket')}
              </Button>
            )}
          </div>
        </div>
        {adminMode && (
          <div className='mt-4 grid gap-3 lg:grid-cols-4'>
            <div className='grid gap-1'>
              <Label>{t('Category')}</Label>
              <TicketSelect
                value={ticket.category}
                options={TICKET_CATEGORIES}
                onValueChange={(category) =>
                  updateMutation.mutate({ category })
                }
              />
            </div>
            <div className='grid gap-1'>
              <Label>{t('Priority')}</Label>
              <TicketSelect
                value={ticket.priority}
                options={TICKET_PRIORITIES}
                onValueChange={(priority) =>
                  updateMutation.mutate({ priority })
                }
              />
            </div>
            <div className='grid gap-1'>
              <Label>{t('Status')}</Label>
              <TicketSelect
                value={ticket.status}
                options={TICKET_STATUSES}
                onValueChange={(status) => updateMutation.mutate({ status })}
              />
            </div>
            <div className='grid gap-1'>
              <Label>{t('Assignee')}</Label>
              <AssigneeSelect
                ticket={ticket}
                disabled={updateMutation.isPending}
                onSelect={(payload) => updateMutation.mutate(payload)}
              />
            </div>
          </div>
        )}
      </div>
      <div className='flex-1 space-y-4 overflow-y-auto p-4'>
        {messages.map((message) => (
          <div
            key={message.id}
            className={`max-w-[92%] rounded-md border p-3 ${
              message.sender === 'admin'
                ? 'bg-muted/60 ml-auto'
                : 'bg-background'
            }`}
          >
            <div className='mb-2 flex flex-wrap items-center justify-between gap-2 text-xs'>
              <div className='flex items-center gap-2'>
                <Badge
                  variant={message.sender === 'admin' ? 'default' : 'secondary'}
                >
                  {message.sender === 'admin' ? t('Admin') : t('User')}
                </Badge>
                <span>{message.username}</span>
              </div>
              <span className='text-muted-foreground'>
                {formatTime(message.created_at)}
              </span>
            </div>
            <div className='text-sm leading-6 break-words whitespace-pre-wrap'>
              {message.content}
            </div>
            <TicketAttachments
              ticketId={ticket.id}
              messageId={message.id}
              attachments={attachments}
              adminMode={adminMode}
            />
          </div>
        ))}
      </div>
      <div
        className='bg-muted/30 border-t p-4'
        onPaste={(event) => {
          const pasted = [...event.clipboardData.files]
          if (pasted.length === 0) return
          event.preventDefault()
          const next = [...files]
          pasted.forEach((file) => {
            if (!file.type.startsWith('image/')) return
            const error = validateImageFile(file)
            if (error) {
              toast.error(t(error))
              return
            }
            if (next.length >= MAX_REPLY_IMAGES) {
              toast.error(
                t('You can upload at most {{count}} images at a time', {
                  count: MAX_REPLY_IMAGES,
                })
              )
              return
            }
            next.push({ file, previewUrl: URL.createObjectURL(file) })
          })
          setFiles(next)
        }}
      >
        {isClosed ? (
          <div className='text-muted-foreground flex items-center gap-2 text-sm'>
            <CheckCircle2 className='h-4 w-4' />
            {t('This ticket is closed. Reopen it to continue replying.')}
          </div>
        ) : (
          <div className='space-y-3'>
            <Textarea
              value={reply}
              onChange={(event) => setReply(event.target.value)}
              className='bg-background min-h-24 resize-none'
              placeholder={t('Enter a reply. You can paste images directly.')}
            />
            <AttachmentPicker files={files} setFiles={setFiles} />
            <Button
              disabled={replyMutation.isPending}
              onClick={() => {
                if (!reply.trim()) {
                  toast.error(t('Please enter a reply'))
                  return
                }
                replyMutation.mutate()
              }}
            >
              <MessageCircleReply className='h-4 w-4' />
              {replyMutation.isPending ? t('Sending...') : t('Send Reply')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

function TicketListPanel({
  title,
  tickets,
  selectedId,
  listQuery,
  statusFilter,
  categoryFilter,
  priorityFilter,
  assigneeFilter,
  startDate,
  endDate,
  keyword,
  ticketPage,
  ticketTotalPages,
  ticketTotal,
  hasPrevTicketPage,
  hasNextTicketPage,
  showUser,
  showFilters,
  showPriorityFilter,
  onCreate,
  onRefresh,
  onSelect,
  onStatusFilterChange,
  onCategoryFilterChange,
  onPriorityFilterChange,
  onAssigneeFilterChange,
  onStartDateChange,
  onEndDateChange,
  onKeywordChange,
  onPrevPage,
  onNextPage,
}: {
  title: string
  tickets: Ticket[]
  selectedId?: number
  listQuery: UseQueryResult<TicketListResponse, Error>
  statusFilter: string
  categoryFilter: string
  priorityFilter: string
  assigneeFilter: string
  startDate: string
  endDate: string
  keyword: string
  ticketPage: number
  ticketTotalPages: number
  ticketTotal: number
  hasPrevTicketPage: boolean
  hasNextTicketPage: boolean
  showUser?: boolean
  showFilters?: boolean
  showPriorityFilter?: boolean
  onCreate?: () => void
  onRefresh: () => void
  onSelect: (id: number) => void
  onStatusFilterChange: (value: string) => void
  onCategoryFilterChange: (value: string) => void
  onPriorityFilterChange: (value: string) => void
  onAssigneeFilterChange: (value: string) => void
  onStartDateChange: (value: string) => void
  onEndDateChange: (value: string) => void
  onKeywordChange: (value: string) => void
  onPrevPage: () => void
  onNextPage: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border flex min-h-[320px] w-full flex-col rounded-md border'>
      <div className='border-b p-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2 font-medium'>
            <LifeBuoy className='h-4 w-4' />
            {title}
          </div>
          <div className='flex items-center gap-2'>
            {onCreate && (
              <Button size='sm' onClick={onCreate}>
                <Plus className='h-4 w-4' />
                {t('Create Ticket')}
              </Button>
            )}
            <Button size='sm' variant='outline' onClick={onRefresh}>
              <RefreshCw className='h-4 w-4' />
              {t('Refresh')}
            </Button>
          </div>
        </div>
        {showFilters && (
          <div
            className={cn(
              'mt-3 grid gap-2',
              showPriorityFilter
                ? 'md:grid-cols-3 xl:grid-cols-[minmax(0,1.3fr)_repeat(6,minmax(0,1fr))]'
                : 'sm:grid-cols-2'
            )}
          >
            {showPriorityFilter && (
              <Input
                value={keyword}
                onChange={(event) => onKeywordChange(event.target.value)}
                placeholder={t('Search number, title, or username')}
              />
            )}
            <Select
              value={statusFilter || 'all'}
              onValueChange={(value) => {
                if (value) onStatusFilterChange(value === 'all' ? '' : value)
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder={t('All statuses')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All statuses')}</SelectItem>
                {TICKET_STATUSES.map((status) => (
                  <SelectItem key={status} value={status}>
                    {t(status)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select
              value={categoryFilter || 'all'}
              onValueChange={(value) => {
                if (value) onCategoryFilterChange(value === 'all' ? '' : value)
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder={t('All categories')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('All categories')}</SelectItem>
                {TICKET_CATEGORIES.map((category) => (
                  <SelectItem key={category} value={category}>
                    {t(category)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {showPriorityFilter && (
              <>
                <Select
                  value={priorityFilter || 'all'}
                  onValueChange={(value) => {
                    if (value) {
                      onPriorityFilterChange(value === 'all' ? '' : value)
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder={t('All priorities')} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>{t('All priorities')}</SelectItem>
                    {TICKET_PRIORITIES.map((priority) => (
                      <SelectItem key={priority} value={priority}>
                        {t(priority)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Input
                  value={assigneeFilter}
                  onChange={(event) =>
                    onAssigneeFilterChange(event.target.value)
                  }
                  inputMode='numeric'
                  placeholder={t('Assignee ID')}
                />
                <Input
                  value={startDate}
                  onChange={(event) => onStartDateChange(event.target.value)}
                  type='date'
                  aria-label={t('Start date')}
                />
                <Input
                  value={endDate}
                  onChange={(event) => onEndDateChange(event.target.value)}
                  type='date'
                  aria-label={t('End date')}
                />
              </>
            )}
          </div>
        )}
      </div>
      <div className='flex-1 overflow-y-auto'>
        {listQuery.isLoading ? (
          <div className='text-muted-foreground p-4 text-sm'>
            {t('Loading ticket...')}
          </div>
        ) : listQuery.isError ? (
          <div className='text-destructive flex h-full min-h-[180px] items-center justify-center p-4 text-sm'>
            {t('Ticket list failed to load. Please try again later.')}
          </div>
        ) : tickets.length === 0 ? (
          <div className='text-muted-foreground flex h-full min-h-[180px] items-center justify-center p-4 text-sm'>
            {t('No tickets yet')}
          </div>
        ) : (
          <>
            {tickets.map((ticket) => (
              <TicketListItem
                key={ticket.id}
                ticket={ticket}
                selected={ticket.id === selectedId}
                showUser={showUser}
                onSelect={() => onSelect(ticket.id)}
              />
            ))}
            <div className='border-t p-3'>
              <div className='text-muted-foreground mb-2 text-center text-xs'>
                {t('Page {{page}} / {{totalPages}}, {{total}} tickets', {
                  page: ticketPage,
                  totalPages: ticketTotalPages,
                  total: ticketTotal,
                })}
              </div>
              <div className='grid grid-cols-2 gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='w-full'
                  onClick={onPrevPage}
                  disabled={!hasPrevTicketPage || listQuery.isFetching}
                >
                  {t('Previous')}
                </Button>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='w-full'
                  onClick={onNextPage}
                  disabled={!hasNextTicketPage || listQuery.isFetching}
                >
                  {t('Next')}
                </Button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

function TicketSecurityNote() {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-muted/20 rounded-md border p-4'>
      <ImagePlus className='text-muted-foreground mb-3 h-6 w-6' />
      <div className='font-medium'>{t('Attachment security rules')}</div>
      <div className='text-muted-foreground mt-2 text-sm leading-6'>
        {t(
          'Tickets only accept png, jpg, jpeg, and webp images. Each image must be 5MB or smaller, and each reply can include at most 5 images. Archives, documents, scripts, and executables are not accepted.'
        )}
      </div>
    </div>
  )
}

export function TicketsPage({ mode = 'user' }: { mode?: TicketsPageMode }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initialSearchParams = useMemo(() => {
    if (typeof window === 'undefined') return new URLSearchParams()
    return new URLSearchParams(window.location.search)
  }, [])
  const adminMode = mode === 'admin'
  const requestedTicketId = useMemo(() => {
    const id = Number(initialSearchParams.get('ticket_id'))
    return Number.isFinite(id) && id > 0 ? id : undefined
  }, [initialSearchParams])
  const urlSelectedIdRef = useRef<number | undefined>(requestedTicketId)
  const [selectedId, setSelectedId] = useState<number | undefined>(
    requestedTicketId
  )
  const [activeSheet, setActiveSheet] = useState<TicketSheet>(
    requestedTicketId ? 'detail' : null
  )
  const pendingCreatedTicketIdRef = useRef<number | undefined>(undefined)
  const [statusFilter, setStatusFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [priorityFilter, setPriorityFilter] = useState('')
  const [assigneeFilter, setAssigneeFilter] = useState('')
  const [startDate, setStartDate] = useState('')
  const [endDate, setEndDate] = useState('')
  const [keyword, setKeyword] = useState('')
  const [ticketPage, setTicketPage] = useState(1)
  const debouncedKeyword = useDebounce(keyword, 300)
  const debouncedAssigneeFilter = useDebounce(assigneeFilter, 300)
  const assigneeId = useMemo(() => {
    const parsed = Number(debouncedAssigneeFilter.trim())
    return Number.isInteger(parsed) && parsed >= 0 ? parsed : undefined
  }, [debouncedAssigneeFilter])
  const startTime = startDate
    ? dayjs(startDate).startOf('day').unix()
    : undefined
  const endTime = endDate ? dayjs(endDate).endOf('day').unix() : undefined

  const listQuery = useQuery({
    queryKey: [
      'tickets',
      adminMode,
      statusFilter,
      categoryFilter,
      priorityFilter,
      assigneeId,
      startTime,
      endTime,
      debouncedKeyword,
      ticketPage,
    ],
    queryFn: () =>
      listTickets({
        admin: adminMode,
        status: statusFilter,
        category: categoryFilter,
        priority: priorityFilter,
        assigneeId,
        startTime,
        endTime,
        keyword: debouncedKeyword.trim(),
        page: ticketPage,
        pageSize: TICKET_LIST_PAGE_SIZE,
      }),
  })

  const tickets = useMemo(() => listQuery.data?.items ?? [], [listQuery.data])
  const ticketTotal = listQuery.data?.total ?? tickets.length
  const ticketTotalPages = Math.max(
    1,
    Math.ceil(ticketTotal / TICKET_LIST_PAGE_SIZE)
  )
  const hasPrevTicketPage = ticketPage > 1
  const hasNextTicketPage = ticketPage < ticketTotalPages

  useEffect(() => {
    if (ticketPage > ticketTotalPages) {
      setTicketPage(ticketTotalPages)
    }
  }, [ticketPage, ticketTotalPages])

  useEffect(() => {
    if (!selectedId) return
    const keepUrlSelected = urlSelectedIdRef.current === selectedId
    const selectedInList = tickets.some((ticket) => ticket.id === selectedId)
    if (!selectedInList && !keepUrlSelected) {
      setSelectedId(undefined)
      setActiveSheet((current) => (current === 'detail' ? null : current))
    }
  }, [selectedId, tickets])

  const selectTicket = (id: number) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(id)
    setActiveSheet('detail')
  }

  const updateStatusFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setStatusFilter(value)
  }

  const updateCategoryFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setCategoryFilter(value)
  }

  const updatePriorityFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setPriorityFilter(value)
  }

  const updateAssigneeFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setAssigneeFilter(value.replaceAll(/[^\d]/g, ''))
  }

  const updateStartDate = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setStartDate(value)
  }

  const updateEndDate = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setEndDate(value)
  }

  const updateKeyword = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setActiveSheet((current) => (current === 'detail' ? null : current))
    setTicketPage(1)
    setKeyword(value)
  }

  const refreshList = () => {
    queryClient.invalidateQueries({ queryKey: ['tickets'] })
  }

  const openPendingCreatedTicketDetail = () => {
    if (pendingCreatedTicketIdRef.current === undefined) return
    pendingCreatedTicketIdRef.current = undefined
    setActiveSheet('detail')
  }

  const openCreatedTicketDetail = (ticket: Ticket) => {
    refreshList()
    urlSelectedIdRef.current = ticket.id
    pendingCreatedTicketIdRef.current = ticket.id
    setSelectedId(ticket.id)
    setActiveSheet((current) => {
      if (current === 'create') return null
      pendingCreatedTicketIdRef.current = undefined
      return 'detail'
    })
  }

  const handleCreateOpenChange = (open: boolean) => {
    if (open) {
      pendingCreatedTicketIdRef.current = undefined
      setActiveSheet('create')
      return
    }
    setActiveSheet((current) => (current === 'create' ? null : current))
  }

  const handleCreateOpenChangeComplete = (open: boolean) => {
    if (!open) openPendingCreatedTicketDetail()
  }

  const handleDetailOpenChange = (open: boolean) => {
    if (open) {
      setActiveSheet('detail')
      return
    }
    setActiveSheet((current) => (current === 'detail' ? null : current))
  }

  const goPrevTicketPage = () => setTicketPage((page) => Math.max(1, page - 1))
  const goNextTicketPage = () =>
    setTicketPage((page) => Math.min(ticketTotalPages, page + 1))
  const title = adminMode ? t('Ticket Management') : t('Ticket Center')
  const description = adminMode
    ? t('Review and handle tickets submitted by users.')
    : t('Create, view, and reply to your own tickets.')

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{title}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {description}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='grid gap-4'>
          <TicketListPanel
            title={t('Ticket List')}
            tickets={tickets}
            selectedId={selectedId}
            listQuery={listQuery}
            statusFilter={statusFilter}
            categoryFilter={categoryFilter}
            priorityFilter={priorityFilter}
            assigneeFilter={assigneeFilter}
            startDate={startDate}
            endDate={endDate}
            keyword={keyword}
            ticketPage={ticketPage}
            ticketTotalPages={ticketTotalPages}
            ticketTotal={ticketTotal}
            hasPrevTicketPage={hasPrevTicketPage}
            hasNextTicketPage={hasNextTicketPage}
            showUser={adminMode}
            showFilters={adminMode}
            showPriorityFilter={adminMode}
            onCreate={adminMode ? undefined : () => setActiveSheet('create')}
            onRefresh={refreshList}
            onSelect={selectTicket}
            onStatusFilterChange={updateStatusFilter}
            onCategoryFilterChange={updateCategoryFilter}
            onPriorityFilterChange={updatePriorityFilter}
            onAssigneeFilterChange={updateAssigneeFilter}
            onStartDateChange={updateStartDate}
            onEndDateChange={updateEndDate}
            onKeywordChange={updateKeyword}
            onPrevPage={goPrevTicketPage}
            onNextPage={goNextTicketPage}
          />
          <TicketSecurityNote />
        </div>
        <Sheet
          open={activeSheet === 'detail'}
          onOpenChange={handleDetailOpenChange}
        >
          <SheetContent className='w-full sm:max-w-5xl'>
            <SheetHeader>
              <SheetTitle>
                {adminMode ? t('Handle Ticket') : t('Ticket Details')}
              </SheetTitle>
              <SheetDescription>
                {adminMode
                  ? t(
                      'View replies, continue the conversation, or update the ticket status.'
                    )
                  : t('View replies and continue the conversation.')}
              </SheetDescription>
            </SheetHeader>
            <div className='min-h-0 flex-1 px-4 pb-4'>
              <TicketDetailPanel
                ticketId={selectedId}
                adminMode={adminMode}
                onChanged={refreshList}
              />
            </div>
          </SheetContent>
        </Sheet>
        {!adminMode && (
          <Sheet
            open={activeSheet === 'create'}
            onOpenChange={handleCreateOpenChange}
            onOpenChangeComplete={handleCreateOpenChangeComplete}
          >
            <SheetContent className='w-full sm:max-w-xl'>
              <SheetHeader>
                <SheetTitle>{t('Create Ticket')}</SheetTitle>
                <SheetDescription>
                  {t(
                    'Submit a problem description and image attachments. Support or finance staff will reply in the ticket.'
                  )}
                </SheetDescription>
              </SheetHeader>
              <div className='min-h-0 flex-1 px-4 pb-4'>
                <CreateTicketPanel
                  onCreated={(ticket) => {
                    openCreatedTicketDetail(ticket)
                  }}
                />
              </div>
            </SheetContent>
          </Sheet>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
