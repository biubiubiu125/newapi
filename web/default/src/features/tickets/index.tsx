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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import dayjs from '@/lib/dayjs'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useDebounce } from '@/hooks'
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
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
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
import { searchUsers } from '@/features/users/api'
import type { User } from '@/features/users/types'
import {
  TICKET_CATEGORIES,
  TICKET_PRIORITIES,
  TICKET_STATUSES,
  type Ticket,
  type TicketAttachment,
  type TicketAttachmentInput,
  type TicketCategory,
  type TicketPriority,
  type TicketStatus,
} from './types'

const MAX_IMAGE_SIZE = 5 * 1024 * 1024
const MAX_REPLY_IMAGES = 5
const TICKET_LIST_PAGE_SIZE = 50
const ACCEPTED_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp']

type BadgeVariant = 'default' | 'secondary' | 'outline'

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
  if (!ACCEPTED_IMAGE_TYPES.includes(file.type)) {
    return '只支持 png、jpg、jpeg、webp 图片'
  }
  if (file.size > MAX_IMAGE_SIZE) {
    return '单张图片不能超过 5MB'
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
            {option}
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
  const inputRef = useRef<HTMLInputElement | null>(null)

  const addFiles = (incoming: File[]) => {
    const accepted: TicketAttachmentInput[] = []
    for (const file of incoming) {
      const error = validateImageFile(file)
      if (error) {
        toast.error(error)
        continue
      }
      if (files.length + accepted.length >= MAX_REPLY_IMAGES) {
        toast.error(`单次最多上传 ${MAX_REPLY_IMAGES} 张图片`)
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
            addFiles(Array.from(event.target.files ?? []))
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
          添加图片
        </Button>
        <span className='text-muted-foreground text-xs'>
          支持粘贴图片；单张 5MB，单次最多 5 张。
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
}: {
  ticket: Ticket
  selected: boolean
  onSelect: () => void
}) {
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
            {ticket.number} · {ticket.category}
          </div>
        </div>
        <Badge variant={statusVariants[ticket.status]}>{ticket.status}</Badge>
      </div>
      <div className='text-muted-foreground flex flex-wrap items-center gap-2 text-xs'>
        <Badge variant={priorityVariants[ticket.priority]}>
          {ticket.priority}
        </Badge>
        <span>{formatTime(ticket.updated_at)}</span>
        {ticket.username && <span>{ticket.username}</span>}
      </div>
    </button>
  )
}

function CreateTicketPanel({
  onCreated,
}: {
  onCreated: (ticket: Ticket) => void
}) {
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
      toast.success('工单已创建')
      revokePreviews(files)
      setTitle('')
      setCategory('客服部门')
      setPriority('普通')
      setContent('')
      setFiles([])
      filesRef.current = []
      onCreated(ticket)
    },
    onError: (error: Error) => toast.error(error.message || '工单创建失败'),
  })

  const submit = () => {
    if (!title.trim()) {
      toast.error('请输入工单标题')
      return
    }
    if (!content.trim()) {
      toast.error('请输入工单内容')
      return
    }
    createMutation.mutate()
  }

  return (
    <div className='border-border flex h-full min-h-0 flex-col rounded-md border'>
      <div className='border-b px-4 py-3'>
        <div className='flex items-center gap-2 font-medium'>
          <Plus className='h-4 w-4' />
          创建工单
        </div>
      </div>
      <div className='flex-1 space-y-4 overflow-y-auto p-4'>
        <div className='grid gap-2'>
          <Label>标题</Label>
          <Input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            maxLength={200}
            placeholder='请简要描述问题'
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='grid gap-2'>
            <Label>分类</Label>
            <TicketSelect
              value={category}
              options={TICKET_CATEGORIES}
              onValueChange={setCategory}
            />
          </div>
          <div className='grid gap-2'>
            <Label>优先级</Label>
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
            const pasted = Array.from(event.clipboardData.files)
            if (pasted.length > 0) {
              event.preventDefault()
              const accepted = pasted.filter((file) =>
                file.type.startsWith('image/')
              )
              if (accepted.length === 0) {
                toast.error('粘贴内容不是图片')
                return
              }
              const next = [...files]
              accepted.forEach((file) => {
                const error = validateImageFile(file)
                if (error) {
                  toast.error(error)
                  return
                }
                if (next.length >= MAX_REPLY_IMAGES) {
                  toast.error(`单次最多上传 ${MAX_REPLY_IMAGES} 张图片`)
                  return
                }
                next.push({ file, previewUrl: URL.createObjectURL(file) })
              })
              setFiles(next)
            }
          }}
        >
          <Label>内容</Label>
          <Textarea
            value={content}
            onChange={(event) => setContent(event.target.value)}
            className='min-h-40 resize-none'
            placeholder='请输入问题详情，可直接粘贴图片'
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
          {createMutation.isPending ? '提交中...' : '提交工单'}
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
  const items = attachments.filter((item) => item.message_id === messageId)
  const queryClient = useQueryClient()
  const [blobUrls, setBlobUrls] = useState<Record<number, string>>({})
  const itemIds = useMemo(() => items.map((item) => item.id).join(','), [items])

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
      toast.error('附件加载失败')
    })

    return () => {
      alive = false
      Object.values(urls).forEach((url) => URL.revokeObjectURL(url))
    }
  }, [adminMode, itemIds, queryClient, ticketId])

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
            '加载中'
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
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const debouncedKeyword = useDebounce(keyword, 300)
  const currentLabel =
    ticket.assignee_id && ticket.assignee_name
      ? `#${ticket.assignee_id} ${ticket.assignee_name}`
      : ticket.assignee_name || '未指派'

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
      return Array.from(map.values()).sort((a, b) => b.role - a.role)
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
            placeholder='搜索管理员'
          />
          <CommandList>
            <CommandEmpty>
              {assigneesQuery.isFetching ? '正在搜索...' : '没有可指派用户'}
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
                <span>未指派</span>
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
                    {user.role >= ROLE.SUPER_ADMIN ? '超级管理员' : '管理员'}
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
      toast.success('回复已发送')
      revokePreviews(files)
      setReply('')
      setFiles([])
      filesRef.current = []
      refreshDetail()
    },
    onError: (error: Error) => toast.error(error.message || '回复发送失败'),
  })

  const closeMutation = useMutation({
    mutationFn: () => closeTicket(ticketId!, adminMode),
    onSuccess: () => {
      toast.success('工单已关闭')
      refreshDetail()
    },
    onError: (error: Error) => toast.error(error.message || '工单关闭失败'),
  })

  const reopenMutation = useMutation({
    mutationFn: () => reopenTicket(ticketId!, adminMode),
    onSuccess: () => {
      toast.success('工单已重新打开')
      refreshDetail()
    },
    onError: (error: Error) => toast.error(error.message || '工单重新打开失败'),
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
      toast.success('工单已更新')
      refreshDetail()
    },
    onError: (error: Error) => toast.error(error.message || '工单更新失败'),
  })

  if (!ticketId) {
    return (
      <div className='border-border text-muted-foreground flex h-full min-h-[360px] items-center justify-center rounded-md border'>
        请选择左侧工单
      </div>
    )
  }

  if (detailQuery.isLoading || !ticket) {
    return (
      <div className='border-border text-muted-foreground flex h-full min-h-[360px] items-center justify-center rounded-md border'>
        {detailQuery.isError ? '工单不存在或无权访问' : '正在加载工单...'}
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
                {ticket.status}
              </Badge>
              <Badge variant={priorityVariants[ticket.priority]}>
                {ticket.priority}
              </Badge>
            </div>
            <div className='text-muted-foreground mt-1 text-xs'>
              {ticket.number} · {ticket.category} · 创建于{' '}
              {formatTime(ticket.created_at)}
              {adminMode && ` · 用户 ${ticket.username || ticket.user_id}`}
            </div>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              size='sm'
              variant='outline'
              onClick={() => detailQuery.refetch()}
            >
              <RefreshCw className='h-4 w-4' />
              刷新
            </Button>
            {isClosed ? (
              <Button
                size='sm'
                variant='secondary'
                onClick={() => reopenMutation.mutate()}
                disabled={reopenMutation.isPending}
              >
                <RotateCcw className='h-4 w-4' />
                重新打开
              </Button>
            ) : (
              <Button
                size='sm'
                variant='outline'
                onClick={() => closeMutation.mutate()}
                disabled={closeMutation.isPending}
              >
                <Lock className='h-4 w-4' />
                关闭工单
              </Button>
            )}
          </div>
        </div>
        {adminMode && (
          <div className='mt-4 grid gap-3 lg:grid-cols-4'>
            <div className='grid gap-1'>
              <Label>分类</Label>
              <TicketSelect
                value={ticket.category}
                options={TICKET_CATEGORIES}
                onValueChange={(category) =>
                  updateMutation.mutate({ category })
                }
              />
            </div>
            <div className='grid gap-1'>
              <Label>优先级</Label>
              <TicketSelect
                value={ticket.priority}
                options={TICKET_PRIORITIES}
                onValueChange={(priority) =>
                  updateMutation.mutate({ priority })
                }
              />
            </div>
            <div className='grid gap-1'>
              <Label>状态</Label>
              <TicketSelect
                value={ticket.status}
                options={TICKET_STATUSES}
                onValueChange={(status) => updateMutation.mutate({ status })}
              />
            </div>
            <div className='grid gap-1'>
              <Label>指派处理人</Label>
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
                  {message.sender === 'admin' ? '管理员' : '用户'}
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
          const pasted = Array.from(event.clipboardData.files)
          if (pasted.length === 0) return
          event.preventDefault()
          const next = [...files]
          pasted.forEach((file) => {
            if (!file.type.startsWith('image/')) return
            const error = validateImageFile(file)
            if (error) {
              toast.error(error)
              return
            }
            if (next.length >= MAX_REPLY_IMAGES) {
              toast.error(`单次最多上传 ${MAX_REPLY_IMAGES} 张图片`)
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
            工单已关闭，重新打开后可以继续回复。
          </div>
        ) : (
          <div className='space-y-3'>
            <Textarea
              value={reply}
              onChange={(event) => setReply(event.target.value)}
              className='bg-background min-h-24 resize-none'
              placeholder='输入回复内容，可直接粘贴图片'
            />
            <AttachmentPicker files={files} setFiles={setFiles} />
            <Button
              disabled={replyMutation.isPending}
              onClick={() => {
                if (!reply.trim()) {
                  toast.error('请输入回复内容')
                  return
                }
                replyMutation.mutate()
              }}
            >
              <MessageCircleReply className='h-4 w-4' />
              {replyMutation.isPending ? '发送中...' : '发送回复'}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

export function TicketsPage() {
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = Boolean(user?.role && user.role >= ROLE.ADMIN)
  const initialSearchParams = useMemo(() => {
    if (typeof window === 'undefined') return new URLSearchParams()
    return new URLSearchParams(window.location.search)
  }, [])
  const requestedAdminMode = initialSearchParams.get('admin') === '1'
  const requestedTicketId = useMemo(() => {
    const id = Number(initialSearchParams.get('ticket_id'))
    return Number.isFinite(id) && id > 0 ? id : undefined
  }, [initialSearchParams])
  const urlSelectedIdRef = useRef<number | undefined>(requestedTicketId)
  const [adminMode, setAdminMode] = useState(() =>
    Boolean(isAdmin && requestedAdminMode)
  )
  const [selectedId, setSelectedId] = useState<number | undefined>(
    requestedTicketId
  )
  const [statusFilter, setStatusFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [ticketPage, setTicketPage] = useState(1)

  useEffect(() => {
    if (!isAdmin) {
      setAdminMode(false)
      return
    }
    if (requestedAdminMode) setAdminMode(true)
  }, [isAdmin, requestedAdminMode])

  const listQuery = useQuery({
    queryKey: ['tickets', adminMode, statusFilter, categoryFilter, ticketPage],
    queryFn: () =>
      listTickets({
        admin: adminMode,
        status: statusFilter,
        category: categoryFilter,
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
    const keepUrlSelected = Boolean(
      selectedId && urlSelectedIdRef.current === selectedId
    )
    if (tickets.length === 0) {
      if (!keepUrlSelected) setSelectedId(undefined)
      return
    }
    const selectedInList = tickets.some((ticket) => ticket.id === selectedId)
    if (!selectedId || (!selectedInList && !keepUrlSelected)) {
      setSelectedId(tickets[0].id)
    }
  }, [selectedId, tickets])

  const switchAdminMode = (nextAdminMode: boolean) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setTicketPage(1)
    setAdminMode(nextAdminMode)
  }

  const selectTicket = (id: number) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(id)
  }

  const updateStatusFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setTicketPage(1)
    setStatusFilter(value)
  }

  const updateCategoryFilter = (value: string) => {
    urlSelectedIdRef.current = undefined
    setSelectedId(undefined)
    setTicketPage(1)
    setCategoryFilter(value)
  }

  const refreshList = () => {
    queryClient.invalidateQueries({ queryKey: ['tickets'] })
  }

  const goPrevTicketPage = () => setTicketPage((page) => Math.max(1, page - 1))
  const goNextTicketPage = () =>
    setTicketPage((page) => Math.min(ticketTotalPages, page + 1))

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>工单中心</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        创建、回复和跟踪 API 调用、账户与财务问题。
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <div className='flex h-[calc(100vh-9rem)] min-h-[620px] flex-col gap-4 lg:flex-row'>
          <div className='border-border flex min-h-[320px] w-full flex-col rounded-md border lg:w-[360px]'>
            <div className='border-b p-3'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <div className='flex items-center gap-2 font-medium'>
                  <LifeBuoy className='h-4 w-4' />
                  工单列表
                </div>
                <Button size='sm' variant='outline' onClick={refreshList}>
                  <RefreshCw className='h-4 w-4' />
                  刷新
                </Button>
              </div>
              {isAdmin && (
                <div className='mt-3 grid grid-cols-2 gap-2'>
                  <Button
                    size='sm'
                    variant={!adminMode ? 'default' : 'outline'}
                    onClick={() => switchAdminMode(false)}
                  >
                    我的工单
                  </Button>
                  <Button
                    size='sm'
                    variant={adminMode ? 'default' : 'outline'}
                    onClick={() => switchAdminMode(true)}
                  >
                    全部工单
                  </Button>
                </div>
              )}
              <div className='mt-3 grid grid-cols-2 gap-2'>
                <Select
                  value={statusFilter || 'all'}
                  onValueChange={(value) => {
                    if (value) updateStatusFilter(value === 'all' ? '' : value)
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder='全部状态' />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>全部状态</SelectItem>
                    {TICKET_STATUSES.map((status) => (
                      <SelectItem key={status} value={status}>
                        {status}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select
                  value={categoryFilter || 'all'}
                  onValueChange={(value) => {
                    if (value)
                      updateCategoryFilter(value === 'all' ? '' : value)
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder='全部分类' />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='all'>全部分类</SelectItem>
                    {TICKET_CATEGORIES.map((category) => (
                      <SelectItem key={category} value={category}>
                        {category}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className='flex-1 overflow-y-auto'>
              {listQuery.isLoading ? (
                <div className='text-muted-foreground p-4 text-sm'>
                  正在加载工单...
                </div>
              ) : listQuery.isError ? (
                <div className='text-destructive flex h-full min-h-[180px] items-center justify-center p-4 text-sm'>
                  工单列表加载失败，请稍后重试
                </div>
              ) : tickets.length === 0 ? (
                <div className='text-muted-foreground flex h-full min-h-[180px] items-center justify-center p-4 text-sm'>
                  暂无工单
                </div>
              ) : (
                <>
                  {tickets.map((ticket) => (
                    <TicketListItem
                      key={ticket.id}
                      ticket={ticket}
                      selected={ticket.id === selectedId}
                      onSelect={() => selectTicket(ticket.id)}
                    />
                  ))}
                  <div className='border-t p-3'>
                    <div className='text-muted-foreground mb-2 text-center text-xs'>
                      第 {ticketPage} / {ticketTotalPages} 页，共 {ticketTotal}{' '}
                      个工单
                    </div>
                    <div className='grid grid-cols-2 gap-2'>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='w-full'
                        onClick={goPrevTicketPage}
                        disabled={!hasPrevTicketPage || listQuery.isFetching}
                      >
                        上一页
                      </Button>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='w-full'
                        onClick={goNextTicketPage}
                        disabled={!hasNextTicketPage || listQuery.isFetching}
                      >
                        下一页
                      </Button>
                    </div>
                  </div>
                </>
              )}
            </div>
          </div>
          <div className='grid min-h-0 flex-1 gap-4 xl:grid-cols-[minmax(0,1fr)_380px]'>
            <TicketDetailPanel
              ticketId={selectedId}
              adminMode={adminMode}
              onChanged={refreshList}
            />
            {!adminMode && (
              <CreateTicketPanel
                onCreated={(ticket) => {
                  refreshList()
                  setSelectedId(ticket.id)
                }}
              />
            )}
            {adminMode && (
              <div className='border-border bg-muted/20 flex min-h-[280px] flex-col justify-center rounded-md border p-6'>
                <ImagePlus className='text-muted-foreground mb-3 h-8 w-8' />
                <div className='font-medium'>附件安全规则</div>
                <div className='text-muted-foreground mt-2 text-sm leading-6'>
                  工单仅接受 png、jpg、jpeg、webp 图片。单张图片不超过
                  5MB，单次回复最多 5 张；不接受压缩包、文档、脚本或可执行文件。
                </div>
              </div>
            )}
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
