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

import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import {
  Button,
  Card,
  Empty,
  Form,
  Input,
  Modal,
  Pagination,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import React, { useCallback, useEffect, useMemo, useState } from 'react';

import {
  API,
  renderQuotaWithAmount,
  showError,
  showSuccess,
} from '../../helpers';

const { Text, Title } = Typography;
const PAGE_SIZE = 20;

const STATUS_OPTIONS = [
  { value: 'all', label: '全部状态' },
  { value: 'pending', label: '待支付' },
  { value: 'success', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'expired', label: '已过期' },
];

const ORDER_TYPE_OPTIONS = [
  { value: 'all', label: '全部订单' },
  { value: 'topup', label: '充值订单' },
  { value: 'subscription', label: '订阅订单' },
];

const ORPHAN_STATUS_OPTIONS = [
  { value: 'pending_review', label: '待人工确认' },
  { value: 'credited', label: '已入账' },
  { value: 'refunded', label: '已退款' },
  { value: 'dismissed', label: '已忽略' },
  { value: 'all', label: '全部状态' },
];

function formatMoney(value, currency = 'CNY') {
  const amount = Number(value || 0);
  const currentCurrency = String(currency || 'CNY').toUpperCase();
  if (currentCurrency === 'CNY') return `¥${amount.toFixed(2)}`;
  if (currentCurrency === 'USD') return `$${amount.toFixed(2)}`;
  return `${currentCurrency} ${amount.toFixed(2)}`;
}

function formatTime(timestamp) {
  if (!timestamp) return '-';
  return new Date(Number(timestamp) * 1000).toLocaleString('zh-CN');
}

function statusLabel(status) {
  switch (status) {
    case 'pending':
      return '待支付';
    case 'success':
      return '成功';
    case 'failed':
      return '失败';
    case 'expired':
      return '已过期';
    default:
      return status || '-';
  }
}

function statusColor(status) {
  switch (status) {
    case 'success':
      return 'green';
    case 'pending':
      return 'amber';
    case 'failed':
    case 'expired':
      return 'red';
    default:
      return 'grey';
  }
}

function orderTypeLabel(type) {
  switch (type) {
    case 'topup':
      return '充值订单';
    case 'subscription':
      return '订阅订单';
    default:
      return type || '-';
  }
}

function paymentProviderLabel(provider) {
  if (!provider) return '-';
  if (String(provider).toLowerCase() === 'bepusdt') return 'BEpusdt';
  return provider;
}

function orphanStatusLabel(status) {
  switch (status) {
    case 'pending_review':
      return '待人工确认';
    case 'credited':
      return '已入账';
    case 'refunded':
      return '已退款';
    case 'dismissed':
      return '已忽略';
    default:
      return status || '-';
  }
}

function orphanStatusColor(status) {
  switch (status) {
    case 'pending_review':
      return 'amber';
    case 'credited':
      return 'green';
    case 'refunded':
      return 'blue';
    case 'dismissed':
      return 'grey';
    default:
      return 'grey';
  }
}

function isPaymentOrphanCreditEligible(event) {
  return Boolean(event?.can_credit);
}

function paidCurrency(order) {
  const provider = String(order.payment_provider || '').toLowerCase();
  if (provider === 'epay' || provider === 'bepusdt') return 'CNY';
  return order.paid_currency || 'CNY';
}

function commissionStatusLabel(status) {
  switch (status) {
    case 'pending':
      return '待处理';
    case 'processing':
      return '处理中';
    case 'succeeded':
      return '已成功';
    case 'skipped':
      return '已跳过';
    case 'failed':
      return '失败';
    default:
      return status || '-';
  }
}

function SummaryCard({ label, value, description }) {
  return (
    <Card bodyStyle={{ padding: 16 }}>
      <Text type='tertiary'>{label}</Text>
      <div className='mt-2 text-2xl font-semibold'>{value}</div>
      {description ? (
        <Text type='secondary' size='small'>
          {description}
        </Text>
      ) : null}
    </Card>
  );
}

export default function RechargeAudit() {
  const [loading, setLoading] = useState(false);
  const [orders, setOrders] = useState([]);
  const [summary, setSummary] = useState(null);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [orphanLoading, setOrphanLoading] = useState(false);
  const [paymentOrphans, setPaymentOrphans] = useState([]);
  const [orphanTotal, setOrphanTotal] = useState(0);
  const [orphanPage, setOrphanPage] = useState(1);
  const [orphanStatus, setOrphanStatus] = useState('pending_review');
  const [keyword, setKeyword] = useState('');
  const [userId, setUserId] = useState('');
  const [provider, setProvider] = useState('');
  const [status, setStatus] = useState('all');
  const [orderType, setOrderType] = useState('all');
  const [filters, setFilters] = useState({
    keyword: '',
    userId: '',
    provider: '',
    status: 'all',
    orderType: 'all',
  });

  const params = useMemo(() => {
    const values = new URLSearchParams({
      p: String(page),
      page_size: String(PAGE_SIZE),
    });
    if (filters.keyword) values.set('keyword', filters.keyword);
    if (filters.userId) values.set('user_id', filters.userId);
    if (filters.provider) values.set('provider', filters.provider);
    if (filters.status !== 'all') values.set('status', filters.status);
    if (filters.orderType !== 'all') {
      values.set('order_type', filters.orderType);
    }
    return values;
  }, [filters, page]);

  const orphanParams = useMemo(
    () =>
      new URLSearchParams({
        p: String(orphanPage),
        page_size: String(PAGE_SIZE),
        status: orphanStatus,
      }),
    [orphanPage, orphanStatus],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [summaryRes, orderRes] = await Promise.all([
        API.get(
          `/api/user/admin/finance/recharge-audit/summary?${params.toString()}`,
        ),
        API.get(`/api/user/admin/finance/recharge-audit?${params.toString()}`),
      ]);
      if (!summaryRes.data.success) {
        showError(summaryRes.data.message || '加载订单统计失败');
      } else {
        setSummary(summaryRes.data.data || null);
      }
      if (!orderRes.data.success) {
        showError(orderRes.data.message || '加载订单列表失败');
      } else {
        setOrders(orderRes.data.data?.items || []);
        setTotal(orderRes.data.data?.total || 0);
      }
    } catch (error) {
      showError(error.message || '加载订单管理数据失败');
    } finally {
      setLoading(false);
    }
  }, [params]);

  const loadPaymentOrphans = useCallback(async () => {
    setOrphanLoading(true);
    try {
      const res = await API.get(
        `/api/user/admin/finance/payment-orphans?${orphanParams.toString()}`,
      );
      if (!res.data.success) {
        showError(res.data.message || '加载人工确认支付失败');
        return;
      }
      setPaymentOrphans(res.data.data?.items || []);
      setOrphanTotal(res.data.data?.total || 0);
    } catch (error) {
      showError(error.message || '加载人工确认支付失败');
    } finally {
      setOrphanLoading(false);
    }
  }, [orphanParams]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    void loadPaymentOrphans();
  }, [loadPaymentOrphans]);

  function applyFilters() {
    setFilters({
      keyword: keyword.trim(),
      userId: userId.trim(),
      provider: provider.trim(),
      status: status || 'all',
      orderType: orderType || 'all',
    });
    setPage(1);
  }

  function refreshAll() {
    void load();
    void loadPaymentOrphans();
  }

  function handleOrphanCredit(event) {
    if (!isPaymentOrphanCreditEligible(event)) return;
    Modal.confirm({
      title: '确认将该 Stripe 支付入账？',
      content:
        '系统会根据 Stripe 回调事实补建缺失的充值或订阅订单，并只入账一次。',
      okText: '确认入账',
      cancelText: '取消',
      onOk: async () => {
        try {
          const res = await API.post(
            `/api/user/admin/finance/payment-orphans/${event.id}/credit`,
          );
          if (!res.data.success) {
            showError(res.data.message || '入账失败');
            return;
          }
          showSuccess('已处理人工确认支付');
          await loadPaymentOrphans();
          await load();
        } catch (error) {
          showError(error.message || '入账失败');
        }
      },
    });
  }

  function handleOrphanResolve(event, status) {
    Modal.confirm({
      title: status === 'refunded' ? '确认标记为已退款？' : '确认忽略该记录？',
      content:
        status === 'refunded'
          ? '仅记录外部退款结果，不会改变用户余额或订阅。'
          : '仅关闭该人工确认记录，不会改变用户余额或订阅。',
      okText: '确认',
      cancelText: '取消',
      onOk: async () => {
        try {
          const res = await API.post(
            `/api/user/admin/finance/payment-orphans/${event.id}/resolve`,
            { status },
          );
          if (!res.data.success) {
            showError(res.data.message || '处理失败');
            return;
          }
          showSuccess('已处理人工确认支付');
          await loadPaymentOrphans();
        } catch (error) {
          showError(error.message || '处理失败');
        }
      },
    });
  }

  const totals = summary?.totals || {};
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const orphanTotalPages = Math.max(1, Math.ceil(orphanTotal / PAGE_SIZE));

  const columns = [
    {
      title: '订单',
      dataIndex: 'trade_no',
      width: 220,
      render: (value, record) => (
        <div>
          <Text strong copyable={value ? { content: value } : false}>
            {value || `#${record.id}`}
          </Text>
          <div className='mt-1'>
            <Tag>{orderTypeLabel(record.order_type)}</Tag>
          </div>
        </div>
      ),
    },
    {
      title: '用户',
      dataIndex: 'username',
      width: 150,
      render: (value, record) => (
        <div>
          <Text>{value || '-'}</Text>
          <div className='text-xs text-gray-500'>
            ID {record.user_id || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '支付',
      dataIndex: 'paid_amount',
      width: 180,
      render: (_, record) => (
        <div>
          <Text strong>
            {formatMoney(
              record.paid_amount || record.money,
              paidCurrency(record),
            )}
          </Text>
          <div className='text-xs text-gray-500'>
            {paymentProviderLabel(record.payment_provider)}
          </div>
        </div>
      ),
    },
    {
      title: '入账',
      dataIndex: 'credit_amount',
      width: 160,
      render: (_, record) => {
        if (record.order_type === 'subscription') {
          return record.product_name || '订阅服务';
        }
        return renderQuotaWithAmount(record.credit_amount ?? record.amount);
      },
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (value) => (
        <Tag color={statusColor(value)}>{statusLabel(value)}</Tag>
      ),
    },
    {
      title: '佣金',
      dataIndex: 'referral_commission_status',
      width: 140,
      render: (value, record) => (
        <div>
          {commissionStatusLabel(value)}
          {record.referral_commission_error ? (
            <div className='text-xs text-red-500'>
              {record.referral_commission_error}
            </div>
          ) : null}
        </div>
      ),
    },
    {
      title: '时间',
      dataIndex: 'create_time',
      width: 190,
      render: (_, record) => (
        <div>
          <div>创建：{formatTime(record.create_time)}</div>
          <div className='text-xs text-gray-500'>
            完成：{formatTime(record.complete_time)}
          </div>
        </div>
      ),
    },
  ];

  const orphanColumns = [
    {
      title: '支付记录',
      dataIndex: 'reference_id',
      width: 230,
      render: (value, record) => (
        <div>
          <Text strong copyable={value ? { content: value } : false}>
            {value || `#${record.id}`}
          </Text>
          <div className='mt-1 text-xs text-gray-500'>
            Session：{record.session_id || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '渠道 / 事件',
      dataIndex: 'provider',
      width: 160,
      render: (value, record) => (
        <div>
          <Tag>{paymentProviderLabel(value)}</Tag>
          <div className='mt-1 text-xs text-gray-500'>
            {record.event_type || '-'}
          </div>
        </div>
      ),
    },
    {
      title: '原因',
      dataIndex: 'reason',
      render: (value, record) => (
        <div>
          <Text>{value || '-'}</Text>
          {record.error ? (
            <div className='mt-1 text-xs text-red-500'>{record.error}</div>
          ) : null}
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 120,
      render: (value) => (
        <Tag color={orphanStatusColor(value)}>{orphanStatusLabel(value)}</Tag>
      ),
    },
    {
      title: '时间',
      dataIndex: 'create_time',
      width: 190,
      render: (_, record) => (
        <div>
          <div>创建：{formatTime(record.create_time)}</div>
          <div className='text-xs text-gray-500'>
            处理：{formatTime(record.resolved_at)}
          </div>
        </div>
      ),
    },
    {
      title: '操作',
      dataIndex: 'id',
      width: 210,
      render: (_, record) =>
        record.status === 'pending_review' ? (
          <Space wrap>
            <Button
              size='small'
              type='primary'
              disabled={!isPaymentOrphanCreditEligible(record)}
              onClick={() => handleOrphanCredit(record)}
            >
              入账
            </Button>
            <Button
              size='small'
              onClick={() => handleOrphanResolve(record, 'refunded')}
            >
              已退款
            </Button>
            <Button
              size='small'
              type='danger'
              onClick={() => handleOrphanResolve(record, 'dismissed')}
            >
              忽略
            </Button>
          </Space>
        ) : (
          <Text type='secondary'>已处理</Text>
        ),
    },
  ];

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2 pb-8'>
      <div className='space-y-4'>
        <div className='flex items-start justify-between gap-3'>
          <div>
            <Title heading={3} style={{ marginBottom: 4 }}>
              订单管理
            </Title>
            <Text type='secondary'>
              查看充值和订阅订单、支付状态以及返佣处理状态。
            </Text>
          </div>
          <Button icon={<IconRefresh />} onClick={refreshAll}>
            刷新
          </Button>
        </div>

        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-5'>
          <SummaryCard
            label='实付金额'
            value={formatMoney(totals.paid_amount_cny || 0, 'CNY')}
          />
          <SummaryCard
            label='成功订单'
            value={String(totals.success_count || 0)}
          />
          <SummaryCard
            label='待支付订单'
            value={String(totals.pending_count || 0)}
          />
          <SummaryCard
            label='失败订单'
            value={String(totals.failed_count || 0)}
          />
          <SummaryCard
            label='汇率缺失'
            value={String(totals.fx_missing_count || 0)}
          />
        </div>

        <Card>
          <Form layout='horizontal' onSubmit={applyFilters}>
            <Space wrap>
              <Input
                prefix={<IconSearch />}
                placeholder='搜索订单号、用户名或用户ID'
                value={keyword}
                onChange={setKeyword}
                onEnterPress={applyFilters}
                style={{ width: 240 }}
              />
              <Input
                placeholder='用户ID'
                value={userId}
                onChange={setUserId}
                onEnterPress={applyFilters}
                style={{ width: 120 }}
              />
              <Select
                value={status}
                onChange={setStatus}
                style={{ width: 140 }}
              >
                {STATUS_OPTIONS.map((item) => (
                  <Select.Option key={item.value} value={item.value}>
                    {item.label}
                  </Select.Option>
                ))}
              </Select>
              <Select
                value={orderType}
                onChange={setOrderType}
                style={{ width: 140 }}
              >
                {ORDER_TYPE_OPTIONS.map((item) => (
                  <Select.Option key={item.value} value={item.value}>
                    {item.label}
                  </Select.Option>
                ))}
              </Select>
              <Input
                placeholder='支付渠道'
                value={provider}
                onChange={setProvider}
                onEnterPress={applyFilters}
                style={{ width: 140 }}
              />
              <Button type='primary' onClick={applyFilters}>
                筛选
              </Button>
            </Space>
          </Form>
        </Card>

        <Card bodyStyle={{ padding: 0 }}>
          <div className='flex flex-wrap items-center justify-between gap-3 px-4 py-3'>
            <div>
              <Text strong>人工确认支付</Text>
              <div className='text-xs text-gray-500'>
                Stripe
                回调已成功但本地订单缺失或支付事实不一致时，会进入这里人工闭环处理。
              </div>
            </div>
            <Space>
              <Select
                value={orphanStatus}
                onChange={(value) => {
                  setOrphanStatus(value || 'pending_review');
                  setOrphanPage(1);
                }}
                style={{ width: 140 }}
              >
                {ORPHAN_STATUS_OPTIONS.map((item) => (
                  <Select.Option key={item.value} value={item.value}>
                    {item.label}
                  </Select.Option>
                ))}
              </Select>
              <Button
                icon={<IconRefresh />}
                loading={orphanLoading}
                onClick={loadPaymentOrphans}
              >
                刷新确认记录
              </Button>
            </Space>
          </div>
          <Spin spinning={orphanLoading}>
            {paymentOrphans.length === 0 ? (
              <div className='py-10'>
                <Empty description='暂无人工确认支付' />
              </div>
            ) : (
              <Table
                columns={orphanColumns}
                dataSource={paymentOrphans}
                rowKey={(record) => `payment-orphan-${record.id}`}
                pagination={false}
              />
            )}
          </Spin>
          <div className='flex items-center justify-between px-4 py-3'>
            <Text type='secondary'>
              第 {orphanPage} / {orphanTotalPages} 页，共 {orphanTotal}{' '}
              条确认记录
            </Text>
            <Pagination
              currentPage={orphanPage}
              total={orphanTotal}
              pageSize={PAGE_SIZE}
              showSizeChanger={false}
              onPageChange={setOrphanPage}
            />
          </div>
        </Card>

        <Card bodyStyle={{ padding: 0 }}>
          <Spin spinning={loading}>
            {orders.length === 0 ? (
              <div className='py-12'>
                <Empty description='暂无订单' />
              </div>
            ) : (
              <Table
                columns={columns}
                dataSource={orders}
                rowKey={(record) => `${record.order_type}-${record.id}`}
                pagination={false}
              />
            )}
          </Spin>
          <div className='flex items-center justify-between px-4 py-3'>
            <Text type='secondary'>
              第 {page} / {totalPages} 页，共 {total} 条订单
            </Text>
            <Pagination
              currentPage={page}
              total={total}
              pageSize={PAGE_SIZE}
              showSizeChanger={false}
              onPageChange={setPage}
            />
          </div>
        </Card>
      </div>
    </div>
  );
}
