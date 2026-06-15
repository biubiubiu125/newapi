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
  Button,
  Card,
  Empty,
  Form,
  Input,
  Pagination,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { IconRefresh, IconSearch } from '@douyinfe/semi-icons';
import { API, renderQuotaWithAmount, showError } from '../../helpers';

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
    if (filters.orderType !== 'all') values.set('order_type', filters.orderType);
    return values;
  }, [filters, page]);

  async function load() {
    setLoading(true);
    try {
      const [summaryRes, orderRes] = await Promise.all([
        API.get(`/api/user/admin/finance/recharge-audit/summary?${params.toString()}`),
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
  }

  useEffect(() => {
    void load();
  }, [params]);

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

  const totals = summary?.totals || {};
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

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
          <div className='text-xs text-gray-500'>ID {record.user_id || '-'}</div>
        </div>
      ),
    },
    {
      title: '支付',
      dataIndex: 'paid_amount',
      width: 180,
      render: (_, record) => (
        <div>
          <Text strong>{formatMoney(record.paid_amount || record.money, paidCurrency(record))}</Text>
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
      render: (value) => <Tag color={statusColor(value)}>{statusLabel(value)}</Tag>,
    },
    {
      title: '佣金',
      dataIndex: 'referral_commission_status',
      width: 140,
      render: (value, record) => (
        <div>
          {commissionStatusLabel(value)}
          {record.referral_commission_error ? (
            <div className='text-xs text-red-500'>{record.referral_commission_error}</div>
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
          <div className='text-xs text-gray-500'>完成：{formatTime(record.complete_time)}</div>
        </div>
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
            <Text type='secondary'>查看充值和订阅订单、支付状态以及返佣处理状态。</Text>
          </div>
          <Button icon={<IconRefresh />} onClick={load}>
            刷新
          </Button>
        </div>

        <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-5'>
          <SummaryCard label='实付金额' value={formatMoney(totals.paid_amount_cny || 0, 'CNY')} />
          <SummaryCard label='成功订单' value={String(totals.success_count || 0)} />
          <SummaryCard label='待支付订单' value={String(totals.pending_count || 0)} />
          <SummaryCard label='失败订单' value={String(totals.failed_count || 0)} />
          <SummaryCard label='汇率缺失' value={String(totals.fx_missing_count || 0)} />
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
              <Select value={status} onChange={setStatus} style={{ width: 140 }}>
                {STATUS_OPTIONS.map((item) => (
                  <Select.Option key={item.value} value={item.value}>
                    {item.label}
                  </Select.Option>
                ))}
              </Select>
              <Select value={orderType} onChange={setOrderType} style={{ width: 140 }}>
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
