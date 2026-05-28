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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Empty,
  Input,
  Modal,
  Pagination,
  Select,
  Space,
  Table,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';

const { Text, Title } = Typography;

const WINDOW_OPTIONS = [
  { label: '24 小时', value: 24 },
  { label: '7 天', value: 168 },
  { label: '30 天', value: 720 },
];

const STATUS_OPTIONS = [
  { label: '未处理', value: 'open' },
  { label: '已查看', value: 'viewed' },
  { label: '已处理', value: 'resolved' },
  { label: '已忽略', value: 'ignored' },
  { label: '全部', value: 'all' },
];

function formatTime(value) {
  return value ? timestamp2string(value) : '-';
}

function severityTag(severity) {
  const map = {
    high: { color: 'red', text: '高危' },
    warning: { color: 'orange', text: '预警' },
    info: { color: 'blue', text: '提示' },
  };
  const item = map[severity] || { color: 'grey', text: severity || '-' };
  return <Tag color={item.color}>{item.text}</Tag>;
}

function statusTag(status) {
  const map = {
    open: { color: 'red', text: '未处理' },
    viewed: { color: 'blue', text: '已查看' },
    resolved: { color: 'green', text: '已处理' },
    ignored: { color: 'grey', text: '已忽略' },
  };
  const item = map[status] || { color: 'grey', text: status || '-' };
  return <Tag color={item.color}>{item.text}</Tag>;
}

function riskTypeLabel(type) {
  const map = {
    shared_ip: '同 IP 多账号',
    high_error_count: '错误日志过多',
    high_topup_activity: '充值异常',
    new_user_high_consume: '新号高消费',
    token_rotation: 'Token 多 IP 使用',
    referral_anomaly: '推广异常',
    payment_anomaly: '支付/返佣异常',
  };
  return map[type] || type || '-';
}

function actionLabel(action) {
  const map = {
    viewed: '查看',
    resolved: '处理完成',
    ignored: '忽略',
    ban_user: '封禁用户',
    unban_user: '解除封禁',
    disable_token: '禁用 Token',
    whitelist: '加入白名单',
    remove_whitelist: '移除白名单',
    note: '备注',
  };
  return map[action] || action || '-';
}

function targetLabel(type, id) {
  if (!type && !id) return '-';
  return `${type || '-'}:${id || '-'}`;
}

function safeParams(params) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '' || value === 0) {
      return;
    }
    search.set(key, String(value));
  });
  return search;
}

function normalizeDetail(data) {
  return {
    users: [],
    logs: [],
    orders: [],
    tokens: [],
    ips: [],
    referrals: [],
    actions: [],
    whitelists: [],
    ...data,
  };
}

function signalMatchesEvent(signal, event) {
  if (signal.event_key) return signal.event_key === event.event_key;
  if (signal.type !== event.type) return false;
  if (signal.target_type && signal.target_type !== event.target_type) return false;
  if (signal.target_id && signal.target_id !== event.target_id) return false;
  if (signal.ip && signal.ip !== event.ip) return false;
  if (signal.user_id && signal.user_id !== event.user_id) return false;
  if (signal.token_id && signal.token_id !== event.token_id) return false;
  if (signal.trade_no && signal.trade_no !== event.trade_no) return false;
  return true;
}

function MetricCard({ title, value }) {
  return (
    <div
      style={{
        border: '1px solid var(--semi-color-border)',
        borderRadius: 12,
        padding: 16,
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <Text type='tertiary'>{title}</Text>
      <div style={{ marginTop: 10, fontSize: 24, fontWeight: 600 }}>
        {value}
      </div>
    </div>
  );
}

export default function RiskCenter() {
  const [windowHours, setWindowHours] = useState(24);
  const [keyword, setKeyword] = useState('');
  const [status, setStatus] = useState('open');
  const [eventPage, setEventPage] = useState(1);
  const [overview, setOverview] = useState(null);
  const [eventsPage, setEventsPage] = useState({ items: [], total: 0, page_size: 20 });
  const [usersPage, setUsersPage] = useState({ items: [], total: 0, page_size: 20 });
  const [actionsPage, setActionsPage] = useState({ items: [], total: 0, page_size: 20 });
  const [loading, setLoading] = useState(false);
  const [scanLoading, setScanLoading] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const [detail, setDetail] = useState(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [selectedEvent, setSelectedEvent] = useState(null);
  const [reason, setReason] = useState('');

  const eventParams = useMemo(
    () =>
      safeParams({
        window_hours: windowHours,
        p: eventPage,
        page_size: 20,
        status,
        keyword: keyword.trim(),
      }),
    [windowHours, eventPage, status, keyword],
  );

  async function requestApi(promise) {
    const res = await promise;
    if (!res.data.success) {
      throw new Error(res.data.message || '请求失败');
    }
    return res.data.data;
  }

  async function load() {
    setLoading(true);
    try {
      const baseParams = safeParams({ window_hours: windowHours });
      const listParams = safeParams({
        window_hours: windowHours,
        page_size: 20,
        keyword: keyword.trim(),
      });
      const [overviewData, eventData, userData, actionData] = await Promise.all([
        requestApi(API.get(`/api/user/admin/risk/overview?${baseParams.toString()}`)),
        requestApi(API.get(`/api/user/admin/risk/events?${eventParams.toString()}`)),
        requestApi(API.get(`/api/user/admin/risk/users?${listParams.toString()}`)),
        requestApi(API.get('/api/user/admin/risk/actions?page_size=20')),
      ]);
      setOverview(overviewData);
      setEventsPage(eventData || { items: [], total: 0, page_size: 20 });
      setUsersPage(userData || { items: [], total: 0, page_size: 20 });
      setActionsPage(actionData || { items: [], total: 0, page_size: 20 });
    } catch (error) {
      showError(error.message || error);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [eventParams]);

  async function scan() {
    setScanLoading(true);
    try {
      const params = safeParams({ window_hours: windowHours });
      const data = await requestApi(
        API.post(`/api/user/admin/risk/scan?${params.toString()}`),
      );
      showSuccess(`扫描完成，发现 ${data.count || 0} 个风险事件`);
      await load();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setScanLoading(false);
    }
  }

  async function processSignal(signal) {
    setScanLoading(true);
    try {
      const params = safeParams({ window_hours: windowHours });
      const data = await requestApi(
        API.post(`/api/user/admin/risk/scan?${params.toString()}`),
      );
      const event = (data.events || []).find((item) =>
        signalMatchesEvent(signal, item),
      );
      await load();
      if (!event) {
        showError('没有找到可处理的风险事件，请刷新后重试');
        return;
      }
      await openEvent(event);
    } catch (error) {
      showError(error.message || error);
    } finally {
      setScanLoading(false);
    }
  }

  async function openEvent(event) {
    setSelectedEvent(event);
    setDetailOpen(true);
    setDetailLoading(true);
    setDetail(null);
    setReason('');
    try {
      await requestApi(API.post(`/api/user/admin/risk/events/${event.id}/view`));
      const params = safeParams({
        window_hours: windowHours,
        type: event.type,
        event_id: event.id,
        ip: event.ip,
        user_id: event.user_id,
        token_id: event.token_id,
        trade_no: event.trade_no,
      });
      const detailData = await requestApi(
        API.get(`/api/user/admin/risk/detail?${params.toString()}`),
      );
      setDetail(normalizeDetail(detailData));
      void load();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function eventAction(action) {
    if (!selectedEvent?.id) return;
    const text = reason.trim();
    if (!text) {
      showError('请填写处理原因');
      return;
    }
    setDetailLoading(true);
    try {
      await requestApi(
        API.post(`/api/user/admin/risk/events/${selectedEvent.id}/${action}`, {
          reason: text,
        }),
      );
      showSuccess('操作成功');
      setDetailOpen(false);
      await load();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function whitelistTarget(targetType, targetId) {
    if (!targetType || !targetId) {
      showError('缺少白名单对象');
      return;
    }
    const text = reason.trim();
    if (!text) {
      showError('请填写处理原因');
      return;
    }
    setDetailLoading(true);
    try {
      await requestApi(
        API.post('/api/user/admin/risk/whitelist', {
          event_id: selectedEvent?.id,
          target_type: targetType,
          target_id: targetId,
          reason: text,
        }),
      );
      showSuccess('已加入白名单');
      if (selectedEvent) {
        await openEvent(selectedEvent);
      } else {
        await load();
      }
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function deleteWhitelist(id) {
    if (!id) return;
    const text = reason.trim();
    if (!text) {
      showError('请填写处理原因');
      return;
    }
    setDetailLoading(true);
    try {
      await requestApi(API.delete(`/api/user/admin/risk/whitelist/${id}`, { data: { reason: text } }));
      showSuccess('已移除白名单');
      if (selectedEvent) {
        await openEvent(selectedEvent);
      } else {
        await load();
      }
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function banUser(userId) {
    if (!userId) return;
    const text = reason.trim();
    if (!text) {
      showError('请填写处理原因');
      return;
    }
    setDetailLoading(true);
    try {
      await requestApi(
        API.post(`/api/user/admin/risk/users/${userId}/ban`, {
          event_id: selectedEvent?.id,
          reason: text,
        }),
      );
      showSuccess('已封禁用户');
      await load();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function disableToken(tokenId) {
    if (!tokenId) return;
    const text = reason.trim();
    if (!text) {
      showError('请填写处理原因');
      return;
    }
    setDetailLoading(true);
    try {
      await requestApi(
        API.post(`/api/user/admin/risk/tokens/${tokenId}/disable`, {
          event_id: selectedEvent?.id,
          reason: text,
        }),
      );
      showSuccess('已禁用 Token');
      await load();
    } catch (error) {
      showError(error.message || error);
    } finally {
      setDetailLoading(false);
    }
  }

  const eventColumns = [
    {
      title: '风险',
      dataIndex: 'title',
      render: (_, row) => (
        <Space vertical spacing={2}>
          <Text strong>{row.title || riskTypeLabel(row.type)}</Text>
          <Text type='tertiary' size='small'>
            {row.summary}
          </Text>
        </Space>
      ),
    },
    { title: '类型', dataIndex: 'type', render: (value) => riskTypeLabel(value) },
    { title: '级别', dataIndex: 'severity', render: (value) => severityTag(value) },
    { title: '状态', dataIndex: 'status', render: (value) => statusTag(value) },
    {
      title: '对象',
      dataIndex: 'target_id',
      render: (_, row) => targetLabel(row.target_type, row.target_id),
    },
    { title: '次数', dataIndex: 'hit_count' },
    { title: '最近出现', dataIndex: 'last_seen_at', render: formatTime },
    {
      title: '操作',
      render: (_, row) => (
        <Button size='small' onClick={() => void openEvent(row)}>
          查看处理
        </Button>
      ),
    },
  ];

  const userColumns = [
    { title: '用户', dataIndex: 'username', render: (_, row) => row.username || `#${row.user_id}` },
    { title: '级别', dataIndex: 'severity', render: (value) => severityTag(value) },
    { title: '信号', dataIndex: 'signal_count' },
    { title: '错误', dataIndex: 'error_count' },
    { title: '消费额度', dataIndex: 'consume_quota' },
    { title: '充值金额', dataIndex: 'topup_paid_amount' },
    { title: 'IP 数', dataIndex: 'unique_ip_count' },
  ];

  const actionColumns = [
    { title: '操作', dataIndex: 'action', render: actionLabel },
    { title: '对象', render: (_, row) => targetLabel(row.target_type, row.target_id) },
    { title: '管理员', dataIndex: 'operator_name' },
    { title: '原因', dataIndex: 'reason' },
    { title: '时间', dataIndex: 'created_at', render: formatTime },
  ];

  const detailOrders = detail?.orders || [];
  const detailTokens = detail?.tokens || [];
  const detailUsers = detail?.users || [];
  const detailWhitelists = detail?.whitelists || [];

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2'>
      <Space vertical spacing='loose' style={{ width: '100%' }}>
        <div className='flex flex-wrap items-center justify-between gap-4'>
          <div>
            <Title heading={3} style={{ marginBottom: 4 }}>
              风控中心
            </Title>
            <Text type='tertiary'>
              只做异常提示和人工处理，不自动封禁、不自动退款、不自动扣款。
            </Text>
          </div>
          <Space wrap>
            <Select
              value={windowHours}
              optionList={WINDOW_OPTIONS}
              onChange={(value) => {
                setWindowHours(Number(value));
                setEventPage(1);
              }}
              style={{ width: 120 }}
            />
            <Select
              value={status}
              optionList={STATUS_OPTIONS}
              onChange={(value) => {
                setStatus(value);
                setEventPage(1);
              }}
              style={{ width: 120 }}
            />
            <Input
              value={keyword}
              onChange={setKeyword}
              placeholder='搜索用户、IP、订单'
              style={{ width: 220 }}
              onEnterPress={() => {
                setEventPage(1);
                void load();
              }}
            />
            <Button onClick={() => void load()} loading={loading}>
              刷新
            </Button>
            <Button type='primary' theme='solid' onClick={scan} loading={scanLoading}>
              扫描风险
            </Button>
          </Space>
        </div>

        <Banner
          type='warning'
          description='支付、订阅、推广返佣、同 IP、多 Token 等异常只进入风控事件和审计记录，后续需要管理员人工判断。'
          fullMode={false}
        />

        <div className='grid grid-cols-1 md:grid-cols-5 gap-4'>
          <MetricCard title='实时信号' value={overview?.signal_count || 0} />
          <MetricCard title='未处理事件' value={overview?.open_event_count || 0} />
          <MetricCard title='高危事件' value={overview?.high_event_count || 0} />
          <MetricCard title='禁用用户' value={overview?.disabled_users || 0} />
          <MetricCard title='新用户' value={overview?.new_user_count || 0} />
        </div>

        <Tabs type='line'>
          <TabPane tab='实时信号' itemKey='signals'>
            <Card>
              {(overview?.signals || []).length === 0 ? (
                <Empty title='暂无实时风险信号' />
              ) : (
                <div className='grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4'>
                  {(overview?.signals || []).map((signal, index) => (
                    <div
                      key={`${signal.event_key || signal.type}-${signal.target_id || index}`}
                      style={{
                        border: '1px solid var(--semi-color-border)',
                        borderRadius: 12,
                        padding: 14,
                      }}
                    >
                      <Space vertical align='start' spacing='tight' style={{ width: '100%' }}>
                        <Space>
                          {severityTag(signal.severity)}
                          <Text strong>{riskTypeLabel(signal.type)}</Text>
                        </Space>
                        <Text type='tertiary'>{signal.message}</Text>
                        <Text size='small'>
                          {targetLabel(signal.target_type, signal.target_id)}
                        </Text>
                        <Button
                          size='small'
                          type='primary'
                          theme='light'
                          onClick={() => void processSignal(signal)}
                        >
                          生成并处理
                        </Button>
                      </Space>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </TabPane>

          <TabPane tab='风险事件' itemKey='events'>
            <Card>
              <Table
                rowKey='id'
                loading={loading}
                columns={eventColumns}
                dataSource={eventsPage.items || []}
                pagination={false}
              />
              <div className='mt-4 flex justify-end'>
                <Pagination
                  currentPage={eventPage}
                  pageSize={eventsPage.page_size || 20}
                  total={eventsPage.total || 0}
                  onPageChange={setEventPage}
                />
              </div>
            </Card>
          </TabPane>

          <TabPane tab='风险用户' itemKey='users'>
            <Card>
              <Table
                rowKey='user_id'
                loading={loading}
                columns={userColumns}
                dataSource={usersPage.items || []}
                pagination={false}
              />
            </Card>
          </TabPane>

          <TabPane tab='处理记录' itemKey='actions'>
            <Card>
              <Table
                rowKey='id'
                loading={loading}
                columns={actionColumns}
                dataSource={actionsPage.items || []}
                pagination={false}
              />
            </Card>
          </TabPane>
        </Tabs>
      </Space>

      <Modal
        title={selectedEvent?.title || '风险详情'}
        visible={detailOpen}
        onCancel={() => setDetailOpen(false)}
        footer={null}
        width={900}
      >
        {detailLoading && !detail ? (
          <Text>加载中...</Text>
        ) : (
          <Space vertical spacing='loose' style={{ width: '100%' }}>
            {selectedEvent && (
              <Card>
                <Space vertical align='start' spacing='tight'>
                  <Space>
                    {severityTag(selectedEvent.severity)}
                    {statusTag(selectedEvent.status)}
                    <Tag>{riskTypeLabel(selectedEvent.type)}</Tag>
                  </Space>
                  <Text>{selectedEvent.summary}</Text>
                  <Text type='tertiary'>
                    {targetLabel(selectedEvent.target_type, selectedEvent.target_id)}
                  </Text>
                </Space>
              </Card>
            )}

            <Input.TextArea
              value={reason}
              onChange={setReason}
              placeholder='处理备注，人工判断原因'
              autosize={{ minRows: 2, maxRows: 4 }}
            />

            <Space wrap>
              <Button type='primary' theme='solid' onClick={() => void eventAction('resolve')}>
                标记已处理
              </Button>
              <Button onClick={() => void eventAction('ignore')}>忽略</Button>
              {selectedEvent?.target_type && selectedEvent?.target_id && (
                <Button
                  type='warning'
                  onClick={() =>
                    void whitelistTarget(selectedEvent.target_type, selectedEvent.target_id)
                  }
                >
                  加入白名单
                </Button>
              )}
              {detailUsers[0]?.user_id && (
                <Button type='danger' onClick={() => void banUser(detailUsers[0].user_id)}>
                  封禁用户
                </Button>
              )}
              {detailTokens[0]?.token_id && (
                <Button type='danger' onClick={() => void disableToken(detailTokens[0].token_id)}>
                  禁用 Token
                </Button>
              )}
            </Space>

            <Tabs type='line'>
              <TabPane tab='关联用户' itemKey='users'>
                <Table
                  rowKey='user_id'
                  columns={userColumns}
                  dataSource={detailUsers}
                  pagination={false}
                />
              </TabPane>
              <TabPane tab='订单' itemKey='orders'>
                <Table
                  rowKey='trade_no'
                  dataSource={detailOrders}
                  pagination={false}
                  columns={[
                    { title: '订单', dataIndex: 'trade_no' },
                    { title: '类型', dataIndex: 'order_type' },
                    { title: '状态', dataIndex: 'status' },
                    { title: '实付', dataIndex: 'paid_amount' },
                    { title: '币种', dataIndex: 'paid_currency' },
                    { title: '返佣状态', dataIndex: 'referral_commission_status' },
                    { title: '返佣错误', dataIndex: 'referral_commission_error' },
                    { title: '创建时间', dataIndex: 'created_at', render: formatTime },
                  ]}
                />
              </TabPane>
              <TabPane tab='Token/IP' itemKey='tokens'>
                <Table
                  rowKey='token_id'
                  dataSource={detailTokens}
                  pagination={false}
                  columns={[
                    { title: 'Token', dataIndex: 'token_name' },
                    { title: '用户', dataIndex: 'username' },
                    { title: '请求数', dataIndex: 'request_count' },
                    { title: '错误数', dataIndex: 'error_count' },
                    { title: 'IP 数', dataIndex: 'unique_ip_count' },
                  ]}
                />
                <Table
                  rowKey='ip'
                  dataSource={detail?.ips || []}
                  pagination={false}
                  columns={[
                    { title: 'IP', dataIndex: 'ip' },
                    { title: '用户数', dataIndex: 'user_count' },
                    { title: '请求数', dataIndex: 'request_count' },
                    { title: '错误数', dataIndex: 'error_count' },
                    { title: '白名单', dataIndex: 'whitelisted', render: (value) => (value ? '是' : '否') },
                  ]}
                />
              </TabPane>
              <TabPane tab='白名单' itemKey='whitelists'>
                <Table
                  rowKey='id'
                  dataSource={detailWhitelists}
                  pagination={false}
                  columns={[
                    { title: '对象', render: (_, row) => targetLabel(row.target_type, row.target_id) },
                    { title: '原因', dataIndex: 'reason' },
                    { title: '操作人', dataIndex: 'operator_name' },
                    { title: '过期时间', dataIndex: 'expires_at', render: formatTime },
                    {
                      title: '操作',
                      render: (_, row) => (
                        <Button size='small' type='danger' onClick={() => void deleteWhitelist(row.id)}>
                          移除
                        </Button>
                      ),
                    },
                  ]}
                />
              </TabPane>
              <TabPane tab='操作记录' itemKey='actions'>
                <Table
                  rowKey='id'
                  columns={actionColumns}
                  dataSource={detail?.actions || []}
                  pagination={false}
                />
              </TabPane>
            </Tabs>
          </Space>
        )}
      </Modal>
    </div>
  );
}
