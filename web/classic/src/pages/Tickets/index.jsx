/*
Copyright (C) 2025 QuantumNous

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

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  Banner,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  Modal,
  Pagination,
  Row,
  Select,
  Space,
  Spin,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSend,
} from '@douyinfe/semi-icons';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';

const { Text, Title } = Typography;

const CATEGORY_OPTIONS = ['客服部门', '财务部门'];
const PRIORITY_OPTIONS = ['低', '普通', '高', '紧急'];
const STATUS_OPTIONS = [
  '待处理',
  '处理中',
  '等待用户回复',
  '管理员已回复',
  '已解决',
  '已关闭',
];
const PAGE_SIZE = 10;
const MAX_IMAGE_SIZE = 5 * 1024 * 1024;
const MAX_REPLY_IMAGES = 5;
const ACCEPTED_IMAGE_TYPES = ['image/png', 'image/jpeg', 'image/webp'];

function ticketBase(adminMode) {
  return adminMode ? '/api/user/admin/tickets' : '/api/user/tickets';
}

function formatTime(value) {
  return value ? timestamp2string(value) : '-';
}

function ticketAttachmentUrl(ticketId, attachmentId, adminMode) {
  return `${ticketBase(adminMode)}/${ticketId}/attachments/${attachmentId}`;
}

function statusColor(status) {
  const map = {
    待处理: 'orange',
    处理中: 'blue',
    等待用户回复: 'amber',
    管理员已回复: 'green',
    已解决: 'green',
    已关闭: 'grey',
  };
  return map[status] || 'grey';
}

function validateImageFiles(files, currentCount = 0) {
  const valid = [];
  for (const file of Array.from(files || [])) {
    if (!ACCEPTED_IMAGE_TYPES.includes(file.type)) {
      showError('仅支持 png、jpg、jpeg、webp 图片');
      continue;
    }
    if (file.size > MAX_IMAGE_SIZE) {
      showError('单张图片不能超过 5MB');
      continue;
    }
    if (currentCount + valid.length >= MAX_REPLY_IMAGES) {
      showError('单次最多上传 5 张图片');
      break;
    }
    valid.push(file);
  }
  return valid;
}

function buildForm(data, files) {
  const form = new FormData();
  Object.entries(data).forEach(([key, value]) => {
    form.append(key, value ?? '');
  });
  files.forEach((file) => form.append('attachments', file));
  return form;
}

function unwrapResponse(res) {
  if (!res?.data?.success) {
    throw new Error(res?.data?.message || '请求失败');
  }
  return res.data.data;
}

function TicketSecurityNote() {
  return (
    <Banner
      type='info'
      closeIcon={null}
      fullMode={false}
      description='附件仅接受 png、jpg、jpeg、webp 图片；单张图片不超过 5MB，单次最多 5 张。不接受压缩包、文档、脚本或可执行文件。'
    />
  );
}

function TicketAttachmentImage({ ticketId, attachment, adminMode }) {
  const [blobUrl, setBlobUrl] = useState('');
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    let objectUrl = '';
    const url = ticketAttachmentUrl(ticketId, attachment.id, adminMode);

    setLoading(true);
    setFailed(false);
    API.get(url, { responseType: 'blob', disableDuplicate: true })
      .then((res) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(res.data);
        setBlobUrl(objectUrl);
      })
      .catch(() => {
        if (active) setFailed(true);
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [adminMode, attachment.id, ticketId]);

  if (loading) {
    return (
      <div
        className='flex items-center justify-center text-xs text-gray-500'
        style={{
          width: 96,
          height: 96,
          borderRadius: 8,
          border: '1px solid var(--semi-color-border)',
        }}
      >
        加载中
      </div>
    );
  }

  if (failed || !blobUrl) {
    return (
      <div
        className='flex items-center justify-center text-xs text-red-500'
        style={{
          width: 96,
          height: 96,
          borderRadius: 8,
          border: '1px solid var(--semi-color-border)',
        }}
      >
        加载失败
      </div>
    );
  }

  return (
    <a href={blobUrl} target='_blank' rel='noopener noreferrer'>
      <img
        src={blobUrl}
        alt={attachment.file_name || '工单图片'}
        style={{
          width: 96,
          height: 96,
          objectFit: 'cover',
          borderRadius: 8,
          border: '1px solid var(--semi-color-border)',
        }}
      />
    </a>
  );
}

export default function Tickets({ adminMode = false }) {
  const pageTitle = adminMode ? '工单管理' : '工单中心';
  const initialTicketId = useMemo(() => {
    const id = Number(
      new URLSearchParams(window.location.search).get('ticket_id'),
    );
    return Number.isFinite(id) && id > 0 ? id : 0;
  }, []);
  const requestedTicketIdRef = useRef(initialTicketId);
  const [tickets, setTickets] = useState([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [detailLoading, setDetailLoading] = useState(false);
  const [selectedId, setSelectedId] = useState(initialTicketId);
  const [detail, setDetail] = useState(null);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('');
  const [category, setCategory] = useState('');
  const [priority, setPriority] = useState('');
  const [createForm, setCreateForm] = useState({
    title: '',
    category: '客服部门',
    priority: '普通',
    content: '',
  });
  const [createFiles, setCreateFiles] = useState([]);
  const [replyContent, setReplyContent] = useState('');
  const [replyFiles, setReplyFiles] = useState([]);
  const [detailVisible, setDetailVisible] = useState(
    Boolean(!adminMode && initialTicketId),
  );
  const [adminForm, setAdminForm] = useState({
    category: '',
    priority: '',
    status: '',
    assignee_id: '',
  });
  const createFileInputRef = useRef(null);
  const replyFileInputRef = useRef(null);

  const base = ticketBase(adminMode);
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const isDetailForSelectedTicket = detail?.ticket?.id === selectedId;
  const messages = isDetailForSelectedTicket ? detail?.messages || [] : [];
  const attachments = isDetailForSelectedTicket
    ? detail?.attachments || []
    : [];
  const selectedTicket = isDetailForSelectedTicket
    ? detail.ticket
    : tickets.find((item) => item.id === selectedId);
  const closed = selectedTicket?.status === '已关闭';

  const loadTickets = useCallback(async () => {
    setLoading(true);
    try {
      const params = {
        p: page,
        page_size: PAGE_SIZE,
        keyword: keyword.trim() || undefined,
        status: status || undefined,
        category: category || undefined,
        priority: priority || undefined,
      };
      const data = unwrapResponse(await API.get(base, { params }));
      const items = data.items || [];
      setTickets(items);
      setTotal(data.total || 0);
      if (adminMode && !selectedId && items.length > 0) {
        setSelectedId(items[0].id);
      }
      if (
        selectedId &&
        requestedTicketIdRef.current !== selectedId &&
        !items.some((item) => item.id === selectedId)
      ) {
        setSelectedId(adminMode && items.length > 0 ? items[0].id : 0);
      }
    } catch (error) {
      showError(error.message || '加载工单失败');
    } finally {
      setLoading(false);
    }
  }, [adminMode, base, category, keyword, page, priority, selectedId, status]);

  const loadDetail = useCallback(
    async (ticketId) => {
      if (!ticketId) {
        setDetail(null);
        return;
      }
      setDetailLoading(true);
      try {
        const data = unwrapResponse(await API.get(`${base}/${ticketId}`));
        setDetail(data);
        setAdminForm({
          category: data.ticket?.category || '',
          priority: data.ticket?.priority || '',
          status: data.ticket?.status || '',
          assignee_id: data.ticket?.assignee_id
            ? String(data.ticket.assignee_id)
            : '',
        });
      } catch (error) {
        showError(error.message || '加载工单详情失败');
      } finally {
        setDetailLoading(false);
      }
    },
    [base],
  );

  useEffect(() => {
    loadTickets();
  }, [loadTickets]);

  useEffect(() => {
    loadDetail(selectedId);
  }, [loadDetail, selectedId]);

  const resetAndSearch = () => {
    if (page === 1) {
      loadTickets();
      return;
    }
    setPage(1);
  };

  const selectTicket = (ticketId) => {
    requestedTicketIdRef.current = 0;
    setSelectedId(ticketId);
    if (!adminMode) {
      setDetailVisible(true);
    }
  };

  const addFiles = (target, files) => {
    if (target === 'create') {
      setCreateFiles((prev) => [
        ...prev,
        ...validateImageFiles(files, prev.length),
      ]);
      return;
    }
    setReplyFiles((prev) => [
      ...prev,
      ...validateImageFiles(files, prev.length),
    ]);
  };

  const handlePaste = (target) => (event) => {
    const files = Array.from(event.clipboardData?.files || []);
    if (files.length > 0) {
      event.preventDefault();
      addFiles(target, files);
    }
  };

  const removeFile = (target, index) => {
    if (target === 'create') {
      setCreateFiles((prev) =>
        prev.filter((_, itemIndex) => itemIndex !== index),
      );
      return;
    }
    setReplyFiles((prev) => prev.filter((_, itemIndex) => itemIndex !== index));
  };

  const createTicket = async () => {
    const title = createForm.title.trim();
    const content = createForm.content.trim();
    if (!title || !content) {
      showError('请填写工单标题和内容');
      return;
    }
    try {
      const ticket = unwrapResponse(
        await API.post(
          '/api/user/tickets',
          buildForm({ ...createForm, title, content }, createFiles),
          { skipErrorHandler: true },
        ),
      );
      showSuccess('工单已提交');
      setCreateForm({
        title: '',
        category: '客服部门',
        priority: '普通',
        content: '',
      });
      setCreateFiles([]);
      setSelectedId(ticket.id);
      await loadTickets();
    } catch (error) {
      showError(error.message || '提交工单失败');
    }
  };

  const replyTicket = async () => {
    if (!selectedId) return;
    const content = replyContent.trim();
    if (!content) {
      showError('请填写回复内容');
      return;
    }
    try {
      unwrapResponse(
        await API.post(
          `${base}/${selectedId}/reply`,
          buildForm({ content }, replyFiles),
          { skipErrorHandler: true },
        ),
      );
      showSuccess('回复已提交');
      setReplyContent('');
      setReplyFiles([]);
      await loadDetail(selectedId);
      await loadTickets();
    } catch (error) {
      showError(error.message || '回复失败');
    }
  };

  const updateTicket = async (payload) => {
    if (!adminMode || !selectedId) return;
    try {
      unwrapResponse(await API.put(`${base}/${selectedId}`, payload));
      showSuccess('工单已更新');
      await loadDetail(selectedId);
      await loadTickets();
    } catch (error) {
      showError(error.message || '更新失败');
    }
  };

  const closeOrReopen = async () => {
    if (!selectedId) return;
    try {
      unwrapResponse(
        await API.post(`${base}/${selectedId}/${closed ? 'reopen' : 'close'}`),
      );
      showSuccess(closed ? '工单已重新打开' : '工单已关闭');
      await loadDetail(selectedId);
      await loadTickets();
    } catch (error) {
      showError(error.message || '操作失败');
    }
  };

  const attachmentMap = useMemo(() => {
    const map = {};
    attachments.forEach((item) => {
      const messageId = item.message_id || 0;
      map[messageId] = map[messageId] || [];
      map[messageId].push(item);
    });
    return map;
  }, [attachments]);

  const renderTicketDetail = () => (
    <Spin spinning={detailLoading}>
      {!selectedTicket ? (
        <Empty description='请选择工单' />
      ) : (
        <>
          <div className='mb-4 text-sm text-gray-600'>
            {selectedTicket.number} · {selectedTicket.category} ·{' '}
            {selectedTicket.priority} · 创建于{' '}
            {formatTime(selectedTicket.created_at)}
            {adminMode && ` · 用户 ${selectedTicket.username || '-'}`}
          </div>
          {adminMode && (
            <Card className='mb-4' bodyStyle={{ padding: 12 }}>
              <Space wrap>
                <Select
                  value={adminForm.category}
                  onChange={(value) =>
                    setAdminForm((prev) => ({ ...prev, category: value }))
                  }
                  style={{ width: 130 }}
                >
                  {CATEGORY_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Select
                  value={adminForm.priority}
                  onChange={(value) =>
                    setAdminForm((prev) => ({ ...prev, priority: value }))
                  }
                  style={{ width: 110 }}
                >
                  {PRIORITY_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Select
                  value={adminForm.status}
                  onChange={(value) =>
                    setAdminForm((prev) => ({ ...prev, status: value }))
                  }
                  style={{ width: 150 }}
                >
                  {STATUS_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Input
                  placeholder='处理人 ID，留空不变，0 为取消'
                  value={adminForm.assignee_id}
                  onChange={(value) =>
                    setAdminForm((prev) => ({ ...prev, assignee_id: value }))
                  }
                  style={{ width: 220 }}
                />
                <Button
                  onClick={() => {
                    const payload = {
                      category: adminForm.category,
                      priority: adminForm.priority,
                      status: adminForm.status,
                    };
                    if (adminForm.assignee_id !== '') {
                      payload.assignee_id = Number(adminForm.assignee_id);
                    }
                    updateTicket(payload);
                  }}
                >
                  保存处理信息
                </Button>
              </Space>
            </Card>
          )}

          <div
            className='space-y-3'
            style={{ minHeight: adminMode ? 280 : 180 }}
          >
            {messages.map((message) => (
              <div
                key={message.id}
                style={{
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 10,
                  padding: 12,
                  background:
                    message.sender === 'admin'
                      ? 'var(--semi-color-fill-0)'
                      : 'var(--semi-color-bg-0)',
                }}
              >
                <div className='flex justify-between mb-2'>
                  <Space>
                    <Tag color={message.sender === 'admin' ? 'blue' : 'green'}>
                      {message.sender === 'admin' ? '管理员' : '用户'}
                    </Tag>
                    <Text strong>{message.username || '-'}</Text>
                  </Space>
                  <Text type='secondary' size='small'>
                    {formatTime(message.created_at)}
                  </Text>
                </div>
                <div style={{ whiteSpace: 'pre-wrap' }}>{message.content}</div>
                {(attachmentMap[message.id] || []).length > 0 && (
                  <div className='flex flex-wrap gap-2 mt-3'>
                    {(attachmentMap[message.id] || []).map((item) => (
                      <TicketAttachmentImage
                        key={item.id}
                        ticketId={selectedTicket.id}
                        attachment={item}
                        adminMode={adminMode}
                      />
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>

          <div className='mt-4'>
            <TextArea
              rows={5}
              value={replyContent}
              disabled={closed}
              onChange={setReplyContent}
              onPaste={handlePaste('reply')}
              placeholder={
                closed
                  ? '工单已关闭，重新打开后可继续回复'
                  : '输入回复内容，可直接粘贴图片'
              }
            />
            <div className='flex items-center justify-between mt-2'>
              <Space wrap>
                <Button
                  onClick={() => replyFileInputRef.current?.click()}
                  disabled={closed}
                >
                  添加图片
                </Button>
                <input
                  ref={replyFileInputRef}
                  type='file'
                  accept='image/png,image/jpeg,image/webp'
                  multiple
                  hidden
                  onChange={(event) => {
                    addFiles('reply', event.target.files);
                    event.target.value = '';
                  }}
                />
                {replyFiles.map((file, index) => (
                  <Tag
                    key={`${file.name}-${index}`}
                    closable
                    onClose={() => removeFile('reply', index)}
                  >
                    {file.name}
                  </Tag>
                ))}
              </Space>
              <Button
                type='primary'
                icon={<IconSend />}
                disabled={closed}
                onClick={replyTicket}
              >
                提交回复
              </Button>
            </div>
          </div>
        </>
      )}
    </Spin>
  );

  return (
    <div className='mt-[60px] px-2 pb-8'>
      <div className='flex items-center justify-between mb-4'>
        <div>
          <Title heading={3}>{pageTitle}</Title>
          <Text type='secondary'>
            {adminMode
              ? '查看、回复和处理用户提交的工单。'
              : '创建工单、查看处理状态并继续回复。'}
          </Text>
        </div>
        <Button icon={<IconRefresh />} onClick={loadTickets}>
          刷新
        </Button>
      </div>

      <TicketSecurityNote />

      <Row gutter={[16, 16]} className='mt-4'>
        <Col xs={24} lg={adminMode ? 8 : 18}>
          <Card
            title='工单列表'
            headerExtraContent={
              !adminMode ? (
                <Button
                  type='primary'
                  icon={<IconPlus />}
                  onClick={() => {
                    document
                      .querySelector('#user-ticket-create-card input')
                      ?.focus();
                  }}
                >
                  创建工单
                </Button>
              ) : null
            }
          >
            {adminMode && (
              <Space wrap className='mb-3'>
                <Input
                  prefix={<IconSearch />}
                  placeholder='搜索标题、编号或用户名'
                  value={keyword}
                  onChange={setKeyword}
                  onEnterPress={resetAndSearch}
                  style={{ width: 220 }}
                />
                <Select
                  placeholder='状态'
                  value={status}
                  onChange={setStatus}
                  style={{ width: 150 }}
                  showClear
                >
                  {STATUS_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Select
                  placeholder='分类'
                  value={category}
                  onChange={setCategory}
                  style={{ width: 120 }}
                  showClear
                >
                  {CATEGORY_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Select
                  placeholder='优先级'
                  value={priority}
                  onChange={setPriority}
                  style={{ width: 120 }}
                  showClear
                >
                  {PRIORITY_OPTIONS.map((item) => (
                    <Select.Option key={item} value={item}>
                      {item}
                    </Select.Option>
                  ))}
                </Select>
                <Button onClick={resetAndSearch}>筛选</Button>
              </Space>
            )}
            <Spin spinning={loading}>
              {tickets.length === 0 ? (
                <Empty description='暂无工单' />
              ) : (
                <div className='space-y-2'>
                  {tickets.map((ticket) => (
                    <div
                      key={ticket.id}
                      onClick={() => selectTicket(ticket.id)}
                      style={{
                        cursor: 'pointer',
                        padding: 12,
                        borderRadius: 10,
                        border:
                          selectedId === ticket.id
                            ? '1px solid var(--semi-color-primary)'
                            : '1px solid var(--semi-color-border)',
                        background:
                          selectedId === ticket.id
                            ? 'var(--semi-color-primary-light-default)'
                            : 'var(--semi-color-bg-0)',
                      }}
                    >
                      <div className='flex items-center justify-between gap-2'>
                        <Text strong ellipsis={{ showTooltip: true }}>
                          {ticket.title || ticket.number}
                        </Text>
                        <Tag color={statusColor(ticket.status)}>
                          {ticket.status}
                        </Tag>
                      </div>
                      <div className='mt-1 text-xs text-gray-500'>
                        {ticket.number} · {ticket.category} · {ticket.priority}
                      </div>
                      <div className='mt-1 text-xs text-gray-500'>
                        {adminMode ? `${ticket.username || '-'} · ` : ''}
                        {formatTime(ticket.updated_at || ticket.created_at)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Spin>
            <div className='mt-4 flex justify-center'>
              <Pagination
                currentPage={page}
                total={total}
                pageSize={PAGE_SIZE}
                onPageChange={setPage}
                showSizeChanger={false}
              />
            </div>
            <Text type='secondary' size='small'>
              第 {page} / {totalPages} 页，共 {total} 个工单
            </Text>
          </Card>
        </Col>

        {adminMode && (
          <Col xs={24} lg={adminMode ? 16 : 10}>
            <Card
              title={selectedTicket ? selectedTicket.title : '工单详情'}
              headerExtraContent={
                selectedTicket ? (
                  <Space>
                    <Tag color={statusColor(selectedTicket.status)}>
                      {selectedTicket.status}
                    </Tag>
                    <Button size='small' onClick={closeOrReopen}>
                      {closed ? '重新打开' : '关闭工单'}
                    </Button>
                  </Space>
                ) : null
              }
            >
              {renderTicketDetail()}
            </Card>
          </Col>
        )}

        {!adminMode && (
          <Col xs={24} lg={6}>
            <Card
              id='user-ticket-create-card'
              title={
                <>
                  <IconPlus /> 创建工单
                </>
              }
            >
              <Form>
                <Form.Input
                  field='title'
                  label='标题'
                  placeholder='请简要描述问题'
                  value={createForm.title}
                  onChange={(value) =>
                    setCreateForm((prev) => ({ ...prev, title: value }))
                  }
                />
                <Form.Select
                  field='category'
                  label='分类'
                  value={createForm.category}
                  onChange={(value) =>
                    setCreateForm((prev) => ({ ...prev, category: value }))
                  }
                >
                  {CATEGORY_OPTIONS.map((item) => (
                    <Form.Select.Option key={item} value={item}>
                      {item}
                    </Form.Select.Option>
                  ))}
                </Form.Select>
                <Form.Select
                  field='priority'
                  label='优先级'
                  value={createForm.priority}
                  onChange={(value) =>
                    setCreateForm((prev) => ({ ...prev, priority: value }))
                  }
                >
                  {PRIORITY_OPTIONS.map((item) => (
                    <Form.Select.Option key={item} value={item}>
                      {item}
                    </Form.Select.Option>
                  ))}
                </Form.Select>
                <Form.TextArea
                  field='content'
                  label='内容'
                  rows={8}
                  placeholder='请输入问题详情，可直接粘贴图片'
                  value={createForm.content}
                  onChange={(value) =>
                    setCreateForm((prev) => ({ ...prev, content: value }))
                  }
                  onPaste={handlePaste('create')}
                />
              </Form>
              <Space wrap className='mt-2'>
                <Button onClick={() => createFileInputRef.current?.click()}>
                  添加图片
                </Button>
                <input
                  ref={createFileInputRef}
                  type='file'
                  accept='image/png,image/jpeg,image/webp'
                  multiple
                  hidden
                  onChange={(event) => {
                    addFiles('create', event.target.files);
                    event.target.value = '';
                  }}
                />
                {createFiles.map((file, index) => (
                  <Tag
                    key={`${file.name}-${index}`}
                    closable
                    onClose={() => removeFile('create', index)}
                  >
                    {file.name}
                  </Tag>
                ))}
              </Space>
              <Button
                type='primary'
                className='mt-4 w-full'
                onClick={createTicket}
              >
                提交工单
              </Button>
            </Card>
          </Col>
        )}
      </Row>

      {!adminMode && (
        <Modal
          title={selectedTicket ? selectedTicket.title : '工单详情'}
          visible={detailVisible}
          onCancel={() => setDetailVisible(false)}
          footer={null}
          width={900}
          bodyStyle={{ maxHeight: '70vh', overflowY: 'auto' }}
        >
          {selectedTicket && (
            <div className='mb-3 flex items-center justify-between gap-2'>
              <Tag color={statusColor(selectedTicket.status)}>
                {selectedTicket.status}
              </Tag>
              <Button size='small' onClick={closeOrReopen}>
                {closed ? '重新打开' : '关闭工单'}
              </Button>
            </div>
          )}
          {renderTicketDetail()}
        </Modal>
      )}
    </div>
  );
}
