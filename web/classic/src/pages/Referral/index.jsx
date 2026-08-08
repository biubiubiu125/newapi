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

import {
  Banner,
  Button,
  Card,
  Empty,
  Input,
  Pagination,
  Select,
  Space,
  Table,
  Tabs,
  TabPane,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate, useParams } from 'react-router-dom';

import {
  API,
  copy,
  showError,
  showSuccess,
  timestamp2string,
} from '../../helpers';
import { convertUSDToCurrency } from '../../helpers/render';

const { Text, Title } = Typography;

const SECTION_META = {
  center: {
    title: '推广中心',
    description: '查看推广状态、邀请链接和返佣余额',
  },
  commissions: {
    title: '佣金明细',
    description: '查看每笔返佣来源、金额和结算状态',
  },
  withdraw: {
    title: '提现申请',
    description: '提交推广返佣提现申请',
  },
  withdrawals: {
    title: '提现记录',
    description: '查看提现审核、打款和取消状态',
  },
};

const SECTION_ORDER = new Set([
  'center',
  'commissions',
  'withdraw',
  'withdrawals',
]);

function normalizeSection(section) {
  return SECTION_ORDER.has(section) ? section : 'center';
}

function formatMoney(value) {
  return convertUSDToCurrency(Number(value || 0));
}

function openConfirm(message) {
  if (typeof window === 'undefined') {
    return true;
  }
  return window.confirm(message);
}

function formatTime(value) {
  if (!value) {
    return '-';
  }
  return timestamp2string(value);
}

function buildIdempotencyKey() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function renderStatusTag(status, t) {
  const tagMap = {
    pending: { color: 'orange', text: t('待处理') },
    approved: { color: 'green', text: t('已通过') },
    rejected: { color: 'red', text: t('已拒绝') },
    disabled: { color: 'grey', text: t('已禁用') },
    available: { color: 'green', text: t('可提现') },
    frozen: { color: 'blue', text: t('冻结中') },
    paid: { color: 'green', text: t('已打款') },
    canceled: { color: 'grey', text: t('已取消') },
  };
  const item = tagMap[status] || { color: 'grey', text: status || '-' };
  return <Tag color={item.color}>{item.text}</Tag>;
}

function MetricCard({ title, value }) {
  return (
    <div className='rounded-xl border border-[var(--semi-color-border)] p-4 bg-[var(--semi-color-bg-0)]'>
      <Text type='tertiary'>{title}</Text>
      <div className='mt-3 text-xl font-semibold'>{value}</div>
    </div>
  );
}

function AmountCard({ title, value }) {
  return (
    <div className='rounded-xl border border-[var(--semi-color-border)] p-4 bg-[var(--semi-color-bg-0)]'>
      <Text type='tertiary'>{title}</Text>
      <div className='mt-2 text-lg font-semibold'>{value}</div>
    </div>
  );
}

export default function Referral() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const params = useParams();
  const section = normalizeSection(params.section);

  const [loading, setLoading] = useState(true);
  const [profile, setProfile] = useState(null);
  const [summary, setSummary] = useState(null);
  const [commissions, setCommissions] = useState([]);
  const [commissionsTotal, setCommissionsTotal] = useState(0);
  const [commissionPage, setCommissionPage] = useState(1);
  const [commissionPageSize, setCommissionPageSize] = useState(20);
  const [withdrawals, setWithdrawals] = useState([]);
  const [withdrawalsTotal, setWithdrawalsTotal] = useState(0);
  const [withdrawalPage, setWithdrawalPage] = useState(1);
  const [withdrawalPageSize, setWithdrawalPageSize] = useState(20);
  const [applicantNote, setApplicantNote] = useState('');
  const [applyLoading, setApplyLoading] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [withdrawForm, setWithdrawForm] = useState({
    amount: '',
    account_type: 'alipay',
    account_name: '',
    account_no: '',
    account_network: 'TRC20',
    qr_image_url: '',
    applicant_note: '',
  });
  const fileInputRef = useRef(null);

  const pageMeta = SECTION_META[section];
  const canViewDashboard =
    profile?.status === 'approved' || profile?.status === 'disabled';
  const canWithdraw =
    profile?.status === 'approved' && profile?.withdrawal_enabled;
  const inviteLink = useMemo(() => {
    if (!summary?.invite_code) {
      return '';
    }
    return `${window.location.origin}/r/${encodeURIComponent(summary.invite_code)}`;
  }, [summary?.invite_code]);

  const loadBase = async () => {
    setLoading(true);
    try {
      const [profileRes, summaryRes] = await Promise.all([
        API.get('/api/user/referral/profile'),
        API.get('/api/user/referral/summary').catch(() => ({
          data: { success: false, data: null },
        })),
      ]);
      setProfile(profileRes?.data?.data || null);
      setSummary(summaryRes?.data?.data || null);
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  };

  const loadCommissions = async (
    page = commissionPage,
    pageSize = commissionPageSize,
  ) => {
    try {
      const res = await API.get('/api/user/referral/commissions', {
        params: { p: page, page_size: pageSize },
      });
      if (res.data.success) {
        setCommissions(res.data.data.items || []);
        setCommissionsTotal(res.data.data.total || 0);
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    }
  };

  const loadWithdrawals = async (
    page = withdrawalPage,
    pageSize = withdrawalPageSize,
  ) => {
    try {
      const res = await API.get('/api/user/referral/withdrawals', {
        params: { p: page, page_size: pageSize },
      });
      if (res.data.success) {
        setWithdrawals(res.data.data.items || []);
        setWithdrawalsTotal(res.data.data.total || 0);
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    }
  };

  useEffect(() => {
    loadBase().then();
  }, []);

  useEffect(() => {
    if (section === 'commissions') {
      loadCommissions().then();
    }
    if (section === 'withdrawals') {
      loadWithdrawals().then();
    }
  }, [section]);

  const handleTabChange = (nextSection) => {
    const target = normalizeSection(nextSection);
    navigate(
      target === 'center' ? '/console/referral' : `/console/referral/${target}`,
    );
  };

  const handleCopyInviteLink = async () => {
    if (!inviteLink) {
      showError(t('当前还没有可用的推广链接'));
      return;
    }
    const copied = await copy(inviteLink);
    if (!copied) {
      showError(t('复制失败，请手动复制'));
      return;
    }
    showSuccess(t('推广链接已复制到剪切板'));
  };

  const handleApply = async () => {
    setApplyLoading(true);
    try {
      const res = await API.post('/api/user/referral/apply', {
        applicant_note: applicantNote.trim(),
      });
      if (res.data.success) {
        showSuccess(t('推广申请已提交'));
        setApplicantNote('');
        await loadBase();
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    } finally {
      setApplyLoading(false);
    }
  };

  const updateWithdrawForm = (key, value) => {
    setWithdrawForm((prev) => ({ ...prev, [key]: value }));
  };

  const handleUpload = async (event) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    const formData = new FormData();
    formData.append('file', file);
    setUploading(true);
    try {
      const res = await API.post('/api/user/referral/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
      if (res.data.success) {
        updateWithdrawForm('qr_image_url', res.data.data?.url || '');
        showSuccess(t('收款二维码上传成功'));
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    } finally {
      setUploading(false);
      if (fileInputRef.current) {
        fileInputRef.current.value = '';
      }
    }
  };

  const handleSubmitWithdrawal = async () => {
    const amount = Number(withdrawForm.amount);
    if (!Number.isFinite(amount) || amount <= 0) {
      showError(t('请输入有效的提现金额'));
      return;
    }
    if (!withdrawForm.account_no.trim()) {
      showError(t('请输入收款账号'));
      return;
    }
    if (
      withdrawForm.account_type !== 'usdt' &&
      !withdrawForm.account_name.trim()
    ) {
      showError(t('请输入收款人名称'));
      return;
    }
    setSubmitLoading(true);
    try {
      const idempotencyKey = buildIdempotencyKey();
      const res = await API.post(
        '/api/user/referral/withdrawals',
        {
          amount,
          account_type: withdrawForm.account_type,
          account_name:
            withdrawForm.account_type === 'usdt'
              ? ''
              : withdrawForm.account_name.trim(),
          account_no: withdrawForm.account_no.trim(),
          account_network:
            withdrawForm.account_type === 'usdt'
              ? withdrawForm.account_network.trim()
              : '',
          qr_image_url: withdrawForm.qr_image_url.trim(),
          applicant_note: withdrawForm.applicant_note.trim(),
          idempotency_key: idempotencyKey,
        },
        {
          headers: {
            'Idempotency-Key': idempotencyKey,
          },
        },
      );
      if (res.data.success) {
        showSuccess(t('提现申请已提交'));
        setWithdrawForm({
          amount: '',
          account_type: 'alipay',
          account_name: '',
          account_no: '',
          account_network: 'TRC20',
          qr_image_url: '',
          applicant_note: '',
        });
        await loadBase();
        setWithdrawalPage(1);
        await loadWithdrawals(1, withdrawalPageSize);
        navigate('/console/referral/withdrawals');
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    } finally {
      setSubmitLoading(false);
    }
  };

  const handleCancelWithdrawal = async (row) => {
    if (!openConfirm(t('确认取消提现吗？'))) {
      return;
    }
    try {
      const res = await API.post(
        `/api/user/referral/withdrawals/${row.id}/cancel`,
      );
      if (res.data.success) {
        showSuccess(t('提现申请已取消'));
        await loadBase();
        await loadWithdrawals(withdrawalPage, withdrawalPageSize);
      } else {
        showError(res.data.message);
      }
    } catch (error) {
      showError(error);
    }
  };

  const commissionColumns = [
    {
      title: t('订单类型'),
      dataIndex: 'order_type',
      render: (value) => (value === 'subscription' ? t('订阅') : t('充值')),
    },
    {
      title: t('被邀请用户'),
      render: (_, row) => row.invitee_username || row.invitee_email || '-',
    },
    {
      title: t('返佣金额'),
      dataIndex: 'commission_amount',
      render: (value) => formatMoney(value),
    },
    {
      title: t('返佣状态'),
      dataIndex: 'status',
      render: (value) => renderStatusTag(value, t),
    },
    {
      title: t('结算时间'),
      render: (_, row) =>
        formatTime(row.available_at || row.settle_at || row.created_at),
    },
  ];

  const withdrawalColumns = [
    {
      title: t('申请金额'),
      dataIndex: 'amount',
      render: (value) => formatMoney(value),
    },
    {
      title: t('到账金额'),
      dataIndex: 'net_amount',
      render: (value) => formatMoney(value),
    },
    {
      title: t('收款方式'),
      render: (_, row) =>
        row.account_type === 'usdt'
          ? `USDT ${row.account_network || ''}`.trim()
          : row.account_type || '-',
    },
    {
      title: t('收款账号'),
      dataIndex: 'account_no_masked',
      render: (value) => value || '-',
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      render: (value) => renderStatusTag(value, t),
    },
    {
      title: t('申请时间'),
      dataIndex: 'submitted_at',
      render: (value) => formatTime(value),
    },
    {
      title: t('操作'),
      render: (_, row) =>
        row.status === 'pending' ? (
          <Button
            theme='borderless'
            type='danger'
            onClick={() => handleCancelWithdrawal(row)}
          >
            {t('取消申请')}
          </Button>
        ) : (
          <Text type='tertiary'>-</Text>
        ),
    },
  ];

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2'>
      <div className='space-y-6'>
        <div className='space-y-2'>
          <Title heading={3} style={{ marginBottom: 0 }}>
            {t(pageMeta.title)}
          </Title>
          <Text type='tertiary'>{t(pageMeta.description)}</Text>
        </div>

        <Tabs type='card' activeKey={section} onChange={handleTabChange}>
          <TabPane tab={t('推广中心')} itemKey='center' />
          <TabPane tab={t('佣金明细')} itemKey='commissions' />
          <TabPane tab={t('提现申请')} itemKey='withdraw' />
          <TabPane tab={t('提现记录')} itemKey='withdrawals' />
        </Tabs>

        {loading ? (
          <Card>
            <Text type='tertiary'>{t('加载中...')}</Text>
          </Card>
        ) : section === 'center' ? (
          canViewDashboard && summary ? (
            <div className='grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_320px] gap-6'>
              <div className='space-y-6'>
                <Card className='!rounded-2xl shadow-sm border-0'>
                  <div className='grid grid-cols-2 lg:grid-cols-4 gap-4'>
                    <MetricCard
                      title={t('推广状态')}
                      value={renderStatusTag(summary.status, t)}
                    />
                    <MetricCard
                      title={t('返佣比例')}
                      value={
                        summary.rate !== null && summary.rate !== undefined
                          ? `${summary.rate}%`
                          : '-'
                      }
                    />
                    <MetricCard
                      title={t('邀请人数')}
                      value={summary.bound_user_count || 0}
                    />
                    <MetricCard
                      title={t('已付费用户')}
                      value={summary.paid_user_count || 0}
                    />
                  </div>
                </Card>

                <Card className='!rounded-2xl shadow-sm border-0'>
                  <Space vertical style={{ width: '100%' }} spacing='loose'>
                    <div>
                      <Text strong>{t('推广邀请码')}</Text>
                      <Input value={summary.invite_code || ''} readonly />
                    </div>
                    <div>
                      <Text strong>{t('推广链接')}</Text>
                      <Input
                        value={inviteLink}
                        readonly
                        suffix={
                          <Button
                            theme='solid'
                            type='primary'
                            onClick={handleCopyInviteLink}
                          >
                            {t('复制')}
                          </Button>
                        }
                      />
                    </div>
                  </Space>
                </Card>

                <Card className='!rounded-2xl shadow-sm border-0'>
                  <div className='grid grid-cols-2 lg:grid-cols-4 gap-4'>
                    <AmountCard
                      title={t('待结算')}
                      value={formatMoney(summary.pending_amount)}
                    />
                    <AmountCard
                      title={t('可提现')}
                      value={formatMoney(summary.available_amount)}
                    />
                    <AmountCard
                      title={t('冻结中')}
                      value={formatMoney(summary.frozen_amount)}
                    />
                    <AmountCard
                      title={t('已提现')}
                      value={formatMoney(summary.withdrawn_amount)}
                    />
                  </div>
                </Card>
              </div>

              <Card className='!rounded-2xl shadow-sm border-0 h-fit'>
                <Space vertical style={{ width: '100%' }} spacing='loose'>
                  <div>
                    <Text strong>{t('推广说明')}</Text>
                    <div className='mt-2 space-y-2'>
                      <Text type='tertiary'>
                        {t(
                          '邀请好友注册并完成充值或订阅后，系统会按照返佣比例计算推广佣金。',
                        )}
                      </Text>
                      <Text type='tertiary'>
                        {t(
                          '推广佣金会先进入待结算，达到结算时间后转为可提现余额。',
                        )}
                      </Text>
                      <Text type='tertiary'>
                        {t('提现会进入审核流程，审核通过后由管理员手工打款。')}
                      </Text>
                    </div>
                  </div>
                  {profile?.status === 'disabled' && (
                    <Banner
                      type='warning'
                      description={
                        profile?.risk_reason || t('当前推广账号已被禁用')
                      }
                      fullMode={false}
                    />
                  )}
                  <Button
                    type='primary'
                    theme='solid'
                    onClick={() => navigate('/console/referral/withdraw')}
                    disabled={!canWithdraw}
                  >
                    {t('发起提现')}
                  </Button>
                </Space>
              </Card>
            </div>
          ) : (
            <Card className='!rounded-2xl shadow-sm border-0'>
              <Space vertical style={{ width: '100%' }} spacing='loose'>
                {profile?.status === 'pending' && (
                  <Banner
                    type='info'
                    description={t('推广申请已提交，正在等待管理员审核。')}
                    fullMode={false}
                  />
                )}
                {profile?.status === 'rejected' && (
                  <Banner
                    type='danger'
                    description={
                      profile?.risk_reason ||
                      t('推广申请未通过，请调整后重新提交。')
                    }
                    fullMode={false}
                  />
                )}
                {profile?.status === 'rejected' && profile?.risk_reason && (
                  <Text type='danger'>{profile.risk_reason}</Text>
                )}
                {!profile && (
                  <Banner
                    type='info'
                    description={t(
                      '提交推广申请后，审核通过即可获得专属推广链接和返佣账户。',
                    )}
                    fullMode={false}
                  />
                )}
                {(profile?.status === 'rejected' || !profile) && (
                  <>
                    <Text strong>{t('申请备注')}</Text>
                    <TextArea
                      value={applicantNote}
                      onChange={setApplicantNote}
                      autosize={{ minRows: 5 }}
                      placeholder={t('填写推广渠道、目标用户群体或运营计划')}
                    />
                    <Button
                      type='primary'
                      loading={applyLoading}
                      onClick={handleApply}
                    >
                      {t('提交推广申请')}
                    </Button>
                  </>
                )}
              </Space>
            </Card>
          )
        ) : section === 'commissions' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Table
              columns={commissionColumns}
              dataSource={commissions}
              pagination={false}
              rowKey='id'
              empty={<Empty description={t('暂无佣金记录')} />}
            />
            <div className='mt-4 flex justify-end'>
              <Pagination
                currentPage={commissionPage}
                pageSize={commissionPageSize}
                total={commissionsTotal}
                onPageChange={(page) => {
                  setCommissionPage(page);
                  loadCommissions(page, commissionPageSize).then();
                }}
                onPageSizeChange={(pageSize) => {
                  setCommissionPageSize(pageSize);
                  setCommissionPage(1);
                  loadCommissions(1, pageSize).then();
                }}
              />
            </div>
          </Card>
        ) : section === 'withdraw' ? (
          <div className='grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_320px] gap-6'>
            <Card className='!rounded-2xl shadow-sm border-0'>
              <Space vertical style={{ width: '100%' }} spacing='loose'>
                {!canWithdraw && (
                  <Banner
                    type='warning'
                    description={t(
                      '当前账号暂不可提现，请先完成推广员审核或联系管理员。',
                    )}
                    fullMode={false}
                  />
                )}
                <div>
                  <Text strong>{t('提现金额')}</Text>
                  <Input
                    value={withdrawForm.amount}
                    onChange={(value) => updateWithdrawForm('amount', value)}
                    placeholder={t('请输入提现金额')}
                  />
                </div>
                <div>
                  <Text strong>{t('收款方式')}</Text>
                  <Select
                    value={withdrawForm.account_type}
                    onChange={(value) =>
                      updateWithdrawForm('account_type', value)
                    }
                    optionList={[
                      { label: 'Alipay', value: 'alipay' },
                      { label: 'WeChat', value: 'wechat' },
                      { label: 'USDT', value: 'usdt' },
                    ]}
                  />
                </div>
                {withdrawForm.account_type === 'usdt' ? (
                  <div>
                    <Text strong>{t('链类型')}</Text>
                    <Select
                      value={withdrawForm.account_network}
                      onChange={(value) =>
                        updateWithdrawForm('account_network', value)
                      }
                      optionList={[
                        { label: 'TRC20', value: 'TRC20' },
                        { label: 'BEP20', value: 'BEP20' },
                        { label: 'POLYGON', value: 'POLYGON' },
                      ]}
                    />
                  </div>
                ) : (
                  <div>
                    <Text strong>{t('收款人姓名')}</Text>
                    <Input
                      value={withdrawForm.account_name}
                      onChange={(value) =>
                        updateWithdrawForm('account_name', value)
                      }
                      placeholder={t('请输入收款人姓名')}
                    />
                  </div>
                )}
                <div>
                  <Text strong>{t('收款账号')}</Text>
                  <Input
                    value={withdrawForm.account_no}
                    onChange={(value) =>
                      updateWithdrawForm('account_no', value)
                    }
                    placeholder={t('请输入收款账号')}
                  />
                </div>
                <div>
                  <Text strong>{t('收款二维码')}</Text>
                  <Space>
                    <Button
                      onClick={() => fileInputRef.current?.click()}
                      loading={uploading}
                    >
                      {t('上传图片')}
                    </Button>
                    <input
                      ref={fileInputRef}
                      type='file'
                      accept='image/*'
                      className='hidden'
                      onChange={handleUpload}
                    />
                    <Input
                      value={withdrawForm.qr_image_url}
                      onChange={(value) =>
                        updateWithdrawForm('qr_image_url', value)
                      }
                      placeholder={t('或直接填写图片链接')}
                    />
                  </Space>
                </div>
                <div>
                  <Text strong>{t('申请备注')}</Text>
                  <TextArea
                    value={withdrawForm.applicant_note}
                    onChange={(value) =>
                      updateWithdrawForm('applicant_note', value)
                    }
                    autosize={{ minRows: 4 }}
                    placeholder={t('可填写补充说明，例如收款偏好或打款提醒')}
                  />
                </div>
                <Button
                  type='primary'
                  theme='solid'
                  loading={submitLoading}
                  disabled={!canWithdraw}
                  onClick={handleSubmitWithdrawal}
                >
                  {t('提交提现申请')}
                </Button>
              </Space>
            </Card>

            <Card className='!rounded-2xl shadow-sm border-0 h-fit'>
              <Space vertical style={{ width: '100%' }} spacing='loose'>
                <AmountCard
                  title={t('当前可提现余额')}
                  value={formatMoney(summary?.available_amount || 0)}
                />
                <AmountCard
                  title={t('最低提现金额')}
                  value={formatMoney(summary?.min_withdraw_amount || 0)}
                />
                <AmountCard
                  title={t('审核中金额')}
                  value={formatMoney(summary?.frozen_amount || 0)}
                />
              </Space>
            </Card>
          </div>
        ) : (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Table
              columns={withdrawalColumns}
              dataSource={withdrawals}
              pagination={false}
              rowKey='id'
              empty={<Empty description={t('暂无提现记录')} />}
            />
            <div className='mt-4 flex justify-end'>
              <Pagination
                currentPage={withdrawalPage}
                pageSize={withdrawalPageSize}
                total={withdrawalsTotal}
                onPageChange={(page) => {
                  setWithdrawalPage(page);
                  loadWithdrawals(page, withdrawalPageSize).then();
                }}
                onPageSizeChange={(pageSize) => {
                  setWithdrawalPageSize(pageSize);
                  setWithdrawalPage(1);
                  loadWithdrawals(1, pageSize).then();
                }}
              />
            </div>
          </Card>
        )}
      </div>
    </div>
  );
}
