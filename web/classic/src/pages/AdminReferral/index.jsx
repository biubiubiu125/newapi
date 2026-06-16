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
import { useNavigate, useParams } from 'react-router-dom';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';
import { convertUSDToCurrency } from '../../helpers/render';
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

const { Text, Title } = Typography;

const FIXED_REFERRAL_REDIRECT_PATH = '/sign-up';

const SECTION_META = {
  overview: {
    title: '推广管理',
    description: '查看推广员、佣金流水、提现、账户流水和审计日志',
  },
  settings: {
    title: '推广设置',
    description: '配置返佣比例、冻结期、提现规则和落地注册页',
  },
  pending: {
    title: '待审核',
    description: '审核推广员申请并设置单独返佣比例',
  },
  affiliates: {
    title: '推广员列表',
    description: '查看推广员状态并执行禁用、恢复、冻结、调账等操作',
  },
  commissions: {
    title: '佣金流水',
    description: '按真实订单查看推广佣金，不展示系统任务表',
  },
  withdrawals: {
    title: '提现记录',
    description: '查看提现申请并在操作弹窗中完成审核、拒绝和打款',
  },
  ledgers: {
    title: '账户流水',
    description: '查看推广账户每次账变的中文流水说明',
  },
  audit: {
    title: '审计日志',
    description: '查看管理员对推广系统的操作记录',
  },
};

const SECTION_ORDER = [
  'overview',
  'settings',
  'pending',
  'affiliates',
  'commissions',
  'withdrawals',
  'ledgers',
  'audit',
];

const COMMISSION_STATUS_OPTIONS = [
  { label: '全部状态', value: '' },
  { label: '待结算', value: 'pending' },
  { label: '可提现', value: 'available' },
  { label: '冻结中', value: 'frozen' },
  { label: '已打款', value: 'paid' },
];

const WITHDRAWAL_STATUS_OPTIONS = [
  { label: '全部状态', value: '' },
  { label: '待处理', value: 'pending' },
  { label: '已通过', value: 'approved' },
  { label: '已拒绝', value: 'rejected' },
  { label: '已打款', value: 'paid' },
];

function normalizeSection(section) {
  return SECTION_ORDER.includes(section) ? section : 'overview';
}

function formatMoney(value) {
  return convertUSDToCurrency(Number(value || 0));
}

function formatTime(value) {
  if (!value) {
    return '-';
  }
  return timestamp2string(value);
}

function parseOptionalNumber(value) {
  const trimmed = String(value ?? '').trim();
  if (trimmed === '') {
    return undefined;
  }
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function parseRequiredNumber(value, fallback) {
  const parsed = parseOptionalNumber(value);
  return parsed === undefined ? fallback : parsed;
}

function buildIdempotencyKey(prefix = 'idem') {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return `${prefix}-${crypto.randomUUID()}`;
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function renderStatusTag(status) {
  const tagMap = {
    pending: { color: 'orange', text: '待处理' },
    approved: { color: 'green', text: '已通过' },
    rejected: { color: 'red', text: '已拒绝' },
    disabled: { color: 'grey', text: '已禁用' },
    available: { color: 'green', text: '可提现' },
    frozen: { color: 'blue', text: '冻结中' },
    paid: { color: 'green', text: '已打款' },
    canceled: { color: 'grey', text: '已取消' },
    failed: { color: 'red', text: '失败' },
    skipped: { color: 'grey', text: '跳过' },
    succeeded: { color: 'green', text: '成功' },
    processing: { color: 'blue', text: '处理中' },
  };
  const item = tagMap[status] || { color: 'grey', text: status || '-' };
  return <Tag color={item.color}>{item.text}</Tag>;
}

function MetricCard({ title, value }) {
  return (
    <div
      style={{
        border: '1px solid var(--semi-color-border)',
        borderRadius: 16,
        padding: 16,
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <Text type='tertiary'>{title}</Text>
      <div style={{ marginTop: 12, fontSize: 28, fontWeight: 600 }}>
        {value}
      </div>
    </div>
  );
}

function getRedirectPathLabel(path) {
  switch (path) {
    case '/register':
      return '/register（旧链接兼容跳转）';
    case '/sign-up':
      return '/sign-up（注册页）';
    default:
      return path || '-';
  }
}

function getLedgerTypeLabel(type) {
  const map = {
    commission_accrue: '佣金入待结算',
    commission_settle: '佣金结算到可提现',
    withdrawal_freeze: '提现冻结',
    withdrawal_approve: '提现审核通过',
    withdrawal_paid: '提现打款完成',
    admin_adjust: '管理员调账',
    commission_adjust_increase: '管理员增加可提现佣金',
    commission_adjust_decrease: '管理员减少可提现佣金',
  };
  return map[type] || type || '-';
}

function getAuditActionLabel(action) {
  const map = {
    referral_settings_update: '修改推广设置',
    referral_affiliate_approve: '审核通过推广员',
    referral_affiliate_reject: '拒绝推广员申请',
    referral_affiliate_disable: '禁用推广员',
    referral_affiliate_restore: '恢复推广员',
    referral_affiliate_rate: '修改返佣比例',
    referral_affiliate_settlement_freeze: '冻结结算',
    referral_affiliate_settlement_restore: '恢复结算',
    referral_affiliate_withdrawal_freeze: '冻结提现',
    referral_affiliate_withdrawal_restore: '恢复提现',
    referral_affiliate_adjust: '管理员调账',
    referral_withdrawal_create: '推广员提交提现申请',
    referral_withdrawal_approve: '审核提现通过',
    referral_withdrawal_reject: '拒绝提现',
    referral_withdrawal_paid: '标记提现已打款',
  };
  return map[action] || action || '-';
}

function getSourceTypeLabel(type) {
  switch (type) {
    case 'topup':
      return '充值订单';
    case 'subscription':
      return '订阅订单';
    default:
      return type || '-';
  }
}

export default function AdminReferral() {
  const navigate = useNavigate();
  const params = useParams();
  const section = normalizeSection(params.section);
  const pageMeta = SECTION_META[section];

  const [loading, setLoading] = useState(true);
  const [overview, setOverview] = useState(null);
  const [settings, setSettings] = useState(null);
  const [pendingItems, setPendingItems] = useState([]);
  const [affiliates, setAffiliates] = useState([]);
  const [bindingItems, setBindingItems] = useState([]);
  const [commissions, setCommissions] = useState([]);
  const [withdrawals, setWithdrawals] = useState([]);
  const [affiliateWithdrawals, setAffiliateWithdrawals] = useState([]);
  const [ledgers, setLedgers] = useState([]);
  const [auditLogs, setAuditLogs] = useState([]);

  const [affiliateKeyword, setAffiliateKeyword] = useState('');
  const [ledgerKeyword, setLedgerKeyword] = useState('');
  const [auditKeyword, setAuditKeyword] = useState('');
  const [commissionStatus, setCommissionStatus] = useState('');
  const [withdrawalStatus, setWithdrawalStatus] = useState('');

  const [commissionPage, setCommissionPage] = useState(1);
  const [commissionPageSize, setCommissionPageSize] = useState(20);
  const [commissionTotal, setCommissionTotal] = useState(0);
  const [withdrawalPage, setWithdrawalPage] = useState(1);
  const [withdrawalPageSize, setWithdrawalPageSize] = useState(20);
  const [withdrawalTotal, setWithdrawalTotal] = useState(0);
  const [ledgerPage, setLedgerPage] = useState(1);
  const [ledgerPageSize, setLedgerPageSize] = useState(20);
  const [ledgerTotal, setLedgerTotal] = useState(0);
  const [auditPage, setAuditPage] = useState(1);
  const [auditPageSize, setAuditPageSize] = useState(20);
  const [auditTotal, setAuditTotal] = useState(0);

  const [reasonInput, setReasonInput] = useState('');
  const [rateOverrideInput, setRateOverrideInput] = useState('');
  const [adjustAmountInput, setAdjustAmountInput] = useState('');

  const [actionDialog, setActionDialog] = useState({
    visible: false,
    kind: '',
    item: null,
  });
  const [detailVisible, setDetailVisible] = useState(false);
  const [detailMode, setDetailMode] = useState('bindings');
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailTarget, setDetailTarget] = useState(null);
  const [withdrawalActionDialog, setWithdrawalActionDialog] = useState({
    visible: false,
    kind: '',
    item: null,
  });
  const [withdrawalAdminNote, setWithdrawalAdminNote] = useState('');
  const [withdrawalRejectReason, setWithdrawalRejectReason] = useState('');
  const [withdrawalPaymentTxnNo, setWithdrawalPaymentTxnNo] = useState('');
  const [withdrawalPaymentProofURL, setWithdrawalPaymentProofURL] =
    useState('');

  async function loadOverview() {
    const res = await API.get('/api/user/admin/referral/overview');
    if (!res.data.success) {
      throw new Error(res.data.message || 'load overview failed');
    }
    setOverview(res.data.data || null);
  }

  async function loadSettings() {
    const res = await API.get('/api/user/admin/referral/settings');
    if (!res.data.success) {
      throw new Error(res.data.message || 'load settings failed');
    }
    setSettings(res.data.data || null);
  }

  async function loadPending() {
    const res = await API.get('/api/user/admin/referral/pending', {
      params: { p: 1, page_size: 50 },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load pending failed');
    }
    setPendingItems(res.data.data?.items || []);
  }

  async function loadAffiliates() {
    const res = await API.get('/api/user/admin/referral/affiliates', {
      params: { p: 1, page_size: 50, keyword: affiliateKeyword || undefined },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load affiliates failed');
    }
    setAffiliates(res.data.data?.items || []);
  }

  async function loadCommissions(
    page = commissionPage,
    pageSize = commissionPageSize,
  ) {
    const res = await API.get('/api/user/admin/referral/commissions', {
      params: {
        p: page,
        page_size: pageSize,
        status: commissionStatus || undefined,
      },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load commissions failed');
    }
    setCommissions(res.data.data?.items || []);
    setCommissionTotal(res.data.data?.total || 0);
  }

  async function loadWithdrawals(
    page = withdrawalPage,
    pageSize = withdrawalPageSize,
  ) {
    const res = await API.get('/api/user/admin/referral/withdrawals', {
      params: {
        p: page,
        page_size: pageSize,
        status: withdrawalStatus || undefined,
      },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load withdrawals failed');
    }
    setWithdrawals(res.data.data?.items || []);
    setWithdrawalTotal(res.data.data?.total || 0);
  }

  async function loadLedgers(page = ledgerPage, pageSize = ledgerPageSize) {
    const res = await API.get('/api/user/admin/referral/ledgers', {
      params: {
        p: page,
        page_size: pageSize,
        keyword: ledgerKeyword || undefined,
      },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load ledgers failed');
    }
    setLedgers(res.data.data?.items || []);
    setLedgerTotal(res.data.data?.total || 0);
  }

  async function loadAuditLogs(page = auditPage, pageSize = auditPageSize) {
    const res = await API.get('/api/user/admin/referral/audit-logs', {
      params: {
        p: page,
        page_size: pageSize,
        keyword: auditKeyword || undefined,
      },
    });
    if (!res.data.success) {
      throw new Error(res.data.message || 'load audit logs failed');
    }
    setAuditLogs(res.data.data?.items || []);
    setAuditTotal(res.data.data?.total || 0);
  }

  async function openAffiliateDetail(item, mode) {
    setDetailVisible(true);
    setDetailMode(mode);
    setDetailTarget(item);
    setDetailLoading(true);
    try {
      if (mode === 'bindings') {
        const res = await API.get(
          `/api/user/admin/referral/affiliates/${item.user_id}/bindings`,
          { params: { p: 1, page_size: 100 } },
        );
        if (!res.data.success) {
          throw new Error(res.data.message || 'load bindings failed');
        }
        setBindingItems(res.data.data?.items || []);
        return;
      }
      const res = await API.get('/api/user/admin/referral/withdrawals', {
        params: { p: 1, page_size: 100, affiliate_user_id: item.user_id },
      });
      if (!res.data.success) {
        throw new Error(
          res.data.message || 'load affiliate withdrawals failed',
        );
      }
      setAffiliateWithdrawals(res.data.data?.items || []);
    } catch (error) {
      showError(error);
    } finally {
      setDetailLoading(false);
    }
  }

  async function loadCurrentSection() {
    setLoading(true);
    try {
      if (section === 'overview') await loadOverview();
      if (section === 'settings') await loadSettings();
      if (section === 'pending') await loadPending();
      if (section === 'affiliates') await loadAffiliates();
      if (section === 'commissions') await loadCommissions();
      if (section === 'withdrawals') await loadWithdrawals();
      if (section === 'ledgers') await loadLedgers();
      if (section === 'audit') await loadAuditLogs();
    } catch (error) {
      showError(error);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void loadCurrentSection();
  }, [section]);

  const overviewCards = useMemo(() => {
    if (!overview) {
      return <Empty description='暂无总览数据' />;
    }
    const cards = [
      ['推广员总数', String(overview.total_affiliates || 0)],
      ['已批准推广员', String(overview.approved_affiliates || 0)],
      ['推广点击', String(overview.referral_click_count || 0)],
      ['已绑定用户', String(overview.bound_user_count || 0)],
      ['已付费邀请用户', String(overview.effective_paid_user_count || 0)],
      ['待结算金额', formatMoney(overview.pending_amount)],
      ['可提现金额', formatMoney(overview.available_amount)],
      ['冻结中金额', formatMoney(overview.frozen_amount)],
      ['已提现金额', formatMoney(overview.withdrawn_amount)],
      ['失败任务数', String(overview.failed_commission_job_count || 0)],
    ];
    return (
      <div
        style={{
          width: '100%',
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
          gap: 16,
        }}
      >
        {cards.map(([title, value]) => (
          <MetricCard key={title} title={title} value={value} />
        ))}
      </div>
    );
  }, [overview]);

  async function runSettlement() {
    try {
      const res = await API.post('/api/user/admin/referral/settlements/run');
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess('结算完成');
      await loadOverview();
      if (section === 'commissions') {
        await loadCommissions();
      }
      if (section === 'ledgers') {
        await loadLedgers();
      }
    } catch (error) {
      showError(error);
    }
  }

  async function saveSettings() {
    try {
      const res = await API.put('/api/user/admin/referral/settings', {
        ...settings,
        redirect_path: FIXED_REFERRAL_REDIRECT_PATH,
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess('保存设置');
      setSettings(res.data.data || null);
    } catch (error) {
      showError(error);
    }
  }

  function openAffiliateAction(kind, item, options = {}) {
    setActionDialog({ visible: true, kind, item });
    setReasonInput(options.reason ?? '');
    setRateOverrideInput(options.rate ?? '');
    setAdjustAmountInput('');
  }

  async function submitAffiliateAction() {
    const item = actionDialog.item;
    if (!item) {
      return;
    }
    try {
      let res;
      switch (actionDialog.kind) {
        case 'approve': {
          const rate = parseOptionalNumber(rateOverrideInput);
          if (rateOverrideInput.trim() !== '' && rate === undefined) {
            showError('请输入有效返佣比例');
            return;
          }
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/approve`,
            { rate_override: rate, reason: reasonInput.trim() },
          );
          break;
        }
        case 'reject':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/reject`,
            { reason: reasonInput.trim() },
          );
          break;
        case 'disable':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/disable`,
            { reason: reasonInput.trim() },
          );
          break;
        case 'restore':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/restore`,
          );
          break;
        case 'freeze_settlement':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/settlement/freeze`,
            { reason: reasonInput.trim() },
          );
          break;
        case 'restore_settlement':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/settlement/restore`,
          );
          break;
        case 'freeze_withdrawal':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/withdrawal/freeze`,
            { reason: reasonInput.trim() },
          );
          break;
        case 'restore_withdrawal':
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/withdrawal/restore`,
          );
          break;
        case 'rate': {
          const rate = parseOptionalNumber(rateOverrideInput);
          if (rateOverrideInput.trim() !== '' && rate === undefined) {
            showError('请输入有效返佣比例');
            return;
          }
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/rate`,
            { rate_override: rate, reason: reasonInput.trim() },
          );
          break;
        }
        case 'adjust_increase':
        case 'adjust_decrease': {
          const amount = parseOptionalNumber(adjustAmountInput);
          if (amount === undefined || amount <= 0) {
            showError('请输入有效调账金额');
            return;
          }
          const key = buildIdempotencyKey('adjust');
          res = await API.post(
            `/api/user/admin/referral/affiliates/${item.user_id}/adjust`,
            {
              amount:
                actionDialog.kind === 'adjust_increase' ? amount : -amount,
              remark: reasonInput.trim(),
              idempotency_key: key,
            },
            { headers: { 'Idempotency-Key': key } },
          );
          break;
        }
        default:
          return;
      }
      if (!res?.data?.success) {
        showError(res?.data?.message || '操作失败');
        return;
      }
      showSuccess('操作成功');
      setActionDialog({ visible: false, kind: '', item: null });
      setReasonInput('');
      setRateOverrideInput('');
      setAdjustAmountInput('');
      await loadOverview();
      await loadPending();
      await loadAffiliates();
      await loadLedgers();
      await loadAuditLogs();
    } catch (error) {
      showError(error);
    }
  }

  function openWithdrawalAction(kind, item) {
    setWithdrawalActionDialog({ visible: true, kind, item });
    setWithdrawalAdminNote(item?.admin_note || '');
    setWithdrawalRejectReason('');
    setWithdrawalPaymentTxnNo('');
    setWithdrawalPaymentProofURL('');
  }

  async function submitWithdrawalAction() {
    const item = withdrawalActionDialog.item;
    if (!item) {
      return;
    }
    try {
      let res;
      if (withdrawalActionDialog.kind === 'approve') {
        res = await API.post(
          `/api/user/admin/referral/withdrawals/${item.id}/approve`,
          { admin_note: withdrawalAdminNote.trim() },
        );
      } else if (withdrawalActionDialog.kind === 'reject') {
        res = await API.post(
          `/api/user/admin/referral/withdrawals/${item.id}/reject`,
          {
            admin_note: withdrawalAdminNote.trim(),
            reject_reason: withdrawalRejectReason.trim(),
          },
        );
      } else {
        res = await API.post(
          `/api/user/admin/referral/withdrawals/${item.id}/pay`,
          {
            admin_note: withdrawalAdminNote.trim(),
            payment_txn_no: withdrawalPaymentTxnNo.trim(),
            payment_proof_url: withdrawalPaymentProofURL.trim(),
          },
        );
      }
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      showSuccess('操作成功');
      setWithdrawalActionDialog({ visible: false, kind: '', item: null });
      setWithdrawalAdminNote('');
      setWithdrawalRejectReason('');
      setWithdrawalPaymentTxnNo('');
      setWithdrawalPaymentProofURL('');
      await loadWithdrawals();
      await loadLedgers();
      await loadAuditLogs();
    } catch (error) {
      showError(error);
    }
  }

  async function handleUploadPaymentProof(event) {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    const formData = new FormData();
    formData.append('file', file);
    try {
      const res = await API.post('/api/user/admin/referral/upload', formData, {
        headers: { 'Content-Type': 'multipart/form-data' },
      });
      if (!res.data.success) {
        showError(res.data.message);
        return;
      }
      setWithdrawalPaymentProofURL(res.data.data?.url || '');
      showSuccess('打款凭证上传成功');
    } catch (error) {
      showError(error);
    } finally {
      event.target.value = '';
    }
  }

  const affiliateColumns = useMemo(
    () => [
      {
        title: '推广员',
        render: (_, row) => row.username || row.email || '-',
      },
      {
        title: '邀请码',
        dataIndex: 'invite_code',
      },
      {
        title: '推广点击',
        render: (_, row) => String(row.click_count || 0),
      },
      {
        title: '返佣比例',
        render: (_, row) =>
          row.rate_override != null
            ? `${row.rate_override}%`
            : row.rate != null
              ? `${row.rate}%`
              : '-',
      },
      {
        title: '可提现 / 待结算',
        render: (_, row) =>
          `${formatMoney(row.available_amount)} / ${formatMoney(row.pending_amount)}`,
      },
      {
        title: '状态',
        dataIndex: 'status',
        render: (value) => renderStatusTag(value),
      },
      {
        title: '操作',
        render: (_, row) => (
          <Space wrap>
            <Button
              size='small'
              onClick={() => void openAffiliateDetail(row, 'bindings')}
            >
              查看绑定
            </Button>
            <Button
              size='small'
              onClick={() =>
                openAffiliateAction('rate', row, {
                  reason: '',
                  rate:
                    row.rate_override == null ? '' : String(row.rate_override),
                })
              }
            >
              改比例
            </Button>
            <Button
              size='small'
              onClick={() =>
                openAffiliateAction(
                  row.status === 'approved' ? 'disable' : 'restore',
                  row,
                  { reason: row.risk_reason || '' },
                )
              }
            >
              {row.status === 'approved' ? '禁用' : '恢复'}
            </Button>
            <Button
              size='small'
              onClick={() =>
                openAffiliateAction(
                  row.settlement_enabled
                    ? 'freeze_settlement'
                    : 'restore_settlement',
                  row,
                  { reason: row.risk_reason || '' },
                )
              }
            >
              {row.settlement_enabled ? '冻结结算' : '恢复结算'}
            </Button>
            <Button
              size='small'
              onClick={() =>
                openAffiliateAction(
                  row.withdrawal_enabled
                    ? 'freeze_withdrawal'
                    : 'restore_withdrawal',
                  row,
                  { reason: row.risk_reason || '' },
                )
              }
            >
              {row.withdrawal_enabled ? '冻结提现' : '恢复提现'}
            </Button>
            <Button
              size='small'
              onClick={() => openAffiliateAction('adjust_increase', row)}
            >
              增加佣金
            </Button>
            <Button
              size='small'
              onClick={() => openAffiliateAction('adjust_decrease', row)}
            >
              减少佣金
            </Button>
          </Space>
        ),
      },
    ],
    [],
  );

  const commissionColumns = [
    {
      title: '订单号',
      dataIndex: 'source_trade_no',
    },
    {
      title: '推广员',
      render: (_, row) => row.affiliate_username || row.affiliate_email || '-',
    },
    {
      title: '被邀请用户',
      render: (_, row) => row.invitee_username || row.invitee_email || '-',
    },
    {
      title: '订单类型',
      render: (_, row) => getSourceTypeLabel(row.source_type),
    },
    {
      title: '实付金额',
      render: (_, row) =>
        `${formatMoney(row.paid_amount)} ${row.paid_currency || ''}`.trim(),
    },
    {
      title: '佣金金额',
      render: (_, row) => formatMoney(row.commission_amount),
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (value) => renderStatusTag(value),
    },
    {
      title: '下单时间',
      dataIndex: 'created_at',
      render: (value) => formatTime(value),
    },
  ];

  const withdrawalColumns = [
    {
      title: '推广员',
      render: (_, row) => row.username || row.email || '-',
    },
    {
      title: '申请金额',
      dataIndex: 'amount',
      render: (value) => formatMoney(value),
    },
    {
      title: '到账金额',
      dataIndex: 'net_amount',
      render: (value) => formatMoney(value),
    },
    {
      title: '收款方式',
      render: (_, row) =>
        row.account_type === 'usdt'
          ? `USDT ${row.account_network || ''}`.trim()
          : row.account_type || '-',
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (value) => renderStatusTag(value),
    },
    {
      title: '申请时间',
      dataIndex: 'submitted_at',
      render: (value) => formatTime(value),
    },
    {
      title: '操作',
      render: (_, row) => (
        <Space wrap>
          {row.status === 'pending' && (
            <>
              <Button
                size='small'
                onClick={() => openWithdrawalAction('approve', row)}
              >
                通过
              </Button>
              <Button
                size='small'
                type='danger'
                onClick={() => openWithdrawalAction('reject', row)}
              >
                拒绝
              </Button>
            </>
          )}
          {row.status === 'approved' && (
            <Button
              size='small'
              type='primary'
              onClick={() => openWithdrawalAction('pay', row)}
            >
              标记已打款
            </Button>
          )}
          {row.payment_txn_no && (
            <Text type='tertiary'>流水号：{row.payment_txn_no}</Text>
          )}
        </Space>
      ),
    },
  ];

  const ledgerColumns = [
    {
      title: '推广员',
      render: (_, row) => row.username || row.email || '-',
    },
    {
      title: '流水类型',
      render: (_, row) => getLedgerTypeLabel(row.type),
    },
    {
      title: '业务引用',
      render: (_, row) => row.ref_id || row.external_ref_id || '-',
    },
    {
      title: '变动说明',
      render: (_, row) => {
        const parts = [];
        if (row.delta_pending)
          parts.push(
            `待结算 ${row.delta_pending > 0 ? '+' : ''}${row.delta_pending}`,
          );
        if (row.delta_available)
          parts.push(
            `可提现 ${row.delta_available > 0 ? '+' : ''}${row.delta_available}`,
          );
        if (row.delta_frozen)
          parts.push(
            `冻结中 ${row.delta_frozen > 0 ? '+' : ''}${row.delta_frozen}`,
          );
        if (row.delta_withdrawn)
          parts.push(
            `已提现 ${row.delta_withdrawn > 0 ? '+' : ''}${row.delta_withdrawn}`,
          );
        return parts.length ? parts.join(' / ') : '无金额变化';
      },
    },
    {
      title: '备注',
      render: (_, row) => row.remark || '-',
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      render: (value) => formatTime(value),
    },
  ];

  const auditColumns = [
    {
      title: '动作',
      render: (_, row) => getAuditActionLabel(row.action),
    },
    {
      title: '原因',
      render: (_, row) => row.reason || '-',
    },
    {
      title: '管理员 ID',
      dataIndex: 'admin_user_id',
    },
    {
      title: '时间',
      dataIndex: 'created_at',
      render: (value) => formatTime(value),
    },
  ];

  const actionDialogTitleMap = {
    approve: '审核通过推广员',
    reject: '拒绝推广员申请',
    disable: '禁用推广员',
    restore: '恢复推广员',
    freeze_settlement: '冻结结算',
    restore_settlement: '恢复结算',
    freeze_withdrawal: '冻结提现',
    restore_withdrawal: '恢复提现',
    rate: '修改返佣比例',
    adjust_increase: '增加佣金',
    adjust_decrease: '减少佣金',
  };

  const showReasonField = [
    'approve',
    'reject',
    'disable',
    'freeze_settlement',
    'freeze_withdrawal',
    'rate',
    'adjust_increase',
    'adjust_decrease',
  ].includes(actionDialog.kind);
  const showRateField = ['approve', 'rate'].includes(actionDialog.kind);
  const showAdjustField = ['adjust_increase', 'adjust_decrease'].includes(
    actionDialog.kind,
  );

  return (
    <div className='w-full max-w-7xl mx-auto relative min-h-screen lg:min-h-0 mt-[60px] px-2'>
      <div className='space-y-6'>
        <div className='space-y-2'>
          <Title heading={3} style={{ marginBottom: 0 }}>
            {pageMeta.title}
          </Title>
          <Text type='tertiary'>{pageMeta.description}</Text>
        </div>

        <Tabs
          type='card'
          activeKey={section}
          onChange={(key) => {
            const next = normalizeSection(key);
            navigate(
              next === 'overview'
                ? '/console/admin-referral'
                : `/console/admin-referral/${next}`,
            );
          }}
        >
          <TabPane tab='总览' itemKey='overview' />
          <TabPane tab='设置' itemKey='settings' />
          <TabPane tab='待审核' itemKey='pending' />
          <TabPane tab='推广员列表' itemKey='affiliates' />
          <TabPane tab='佣金流水' itemKey='commissions' />
          <TabPane tab='提现' itemKey='withdrawals' />
          <TabPane tab='账户流水' itemKey='ledgers' />
          <TabPane tab='审计' itemKey='audit' />
        </Tabs>

        {loading ? (
          <Card>
            <Text type='tertiary'>加载中...</Text>
          </Card>
        ) : section === 'overview' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <div style={{ width: '100%' }}>
              {overviewCards}
              <div style={{ marginTop: 16 }}>
                <Button type='primary' theme='solid' onClick={runSettlement}>
                  手动触发结算
                </Button>
              </div>
            </div>
          </Card>
        ) : section === 'settings' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Banner
                type='info'
                description='这里配置的是新的独立推广返佣系统，不再是旧邀请码送额度逻辑。注册跳转表示推广链接落地后默认跳去哪个注册页。'
                fullMode={false}
              />
              {settings && (
                <>
                  <Input
                    value={String(settings.default_rate ?? '')}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              default_rate: parseRequiredNumber(
                                value,
                                prev.default_rate,
                              ),
                            }
                          : prev,
                      )
                    }
                    addonBefore='默认返佣比例'
                    addonAfter='%'
                  />
                  <Input
                    value={String(settings.cookie_ttl_days ?? '')}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              cookie_ttl_days: parseRequiredNumber(
                                value,
                                prev.cookie_ttl_days,
                              ),
                            }
                          : prev,
                      )
                    }
                    addonBefore='Cookie 天数'
                  />
                  <Input
                    value={String(settings.settle_freeze_days ?? '')}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              settle_freeze_days: parseRequiredNumber(
                                value,
                                prev.settle_freeze_days,
                              ),
                            }
                          : prev,
                      )
                    }
                    addonBefore='冻结期'
                    addonAfter='天'
                  />
                  <Input
                    value={String(settings.min_withdraw_amount ?? '')}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              min_withdraw_amount: parseRequiredNumber(
                                value,
                                prev.min_withdraw_amount,
                              ),
                            }
                          : prev,
                      )
                    }
                    addonBefore='最小提现金额'
                  />
                  <Input
                    value={String(settings.withdraw_fee ?? '')}
                    onChange={(value) =>
                      setSettings((prev) =>
                        prev
                          ? {
                              ...prev,
                              withdraw_fee: parseRequiredNumber(
                                value,
                                prev.withdraw_fee,
                              ),
                            }
                          : prev,
                      )
                    }
                    addonBefore='提现手续费'
                  />
                  <Input
                    value={FIXED_REFERRAL_REDIRECT_PATH}
                    readonly
                    addonBefore='注册跳转'
                  />
                  <Text type='tertiary'>
                    推荐：classic 和 default 均使用{' '}
                    {getRedirectPathLabel(FIXED_REFERRAL_REDIRECT_PATH)}
                    ，/register 仅保留旧链接兼容
                  </Text>
                  <Button type='primary' theme='solid' onClick={saveSettings}>
                    保存设置
                  </Button>
                </>
              )}
            </Space>
          </Card>
        ) : section === 'pending' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Table
              rowKey='user_id'
              dataSource={pendingItems}
              pagination={false}
              empty={<Empty description='暂无待审核推广员' />}
              columns={[
                {
                  title: '用户',
                  render: (_, row) => row.username || row.email || '-',
                },
                {
                  title: '申请备注',
                  render: (_, row) => row.applicant_note || '-',
                },
                {
                  title: '提交时间',
                  render: (_, row) => formatTime(row.created_at),
                },
                {
                  title: '操作',
                  render: (_, row) => (
                    <Space>
                      <Button
                        type='primary'
                        size='small'
                        onClick={() =>
                          openAffiliateAction('approve', row, {
                            reason: '',
                            rate:
                              row.rate_override == null
                                ? ''
                                : String(row.rate_override),
                          })
                        }
                      >
                        通过
                      </Button>
                      <Button
                        type='danger'
                        size='small'
                        onClick={() =>
                          openAffiliateAction('reject', row, {
                            reason: '',
                            rate: '',
                          })
                        }
                      >
                        拒绝
                      </Button>
                    </Space>
                  ),
                },
              ]}
            />
          </Card>
        ) : section === 'affiliates' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Input
                value={affiliateKeyword}
                onChange={setAffiliateKeyword}
                placeholder='搜索推广员用户名 / 邮箱 / 邀请码'
              />
              <Button onClick={() => void loadAffiliates()}>刷新列表</Button>
              <Table
                rowKey='user_id'
                dataSource={affiliates}
                pagination={false}
                empty={<Empty description='暂无推广员' />}
                columns={affiliateColumns}
              />
            </Space>
          </Card>
        ) : section === 'commissions' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Select
                value={commissionStatus}
                onChange={setCommissionStatus}
                optionList={COMMISSION_STATUS_OPTIONS}
              />
              <Button onClick={() => void loadCommissions()}>刷新列表</Button>
              <Table
                rowKey='id'
                columns={commissionColumns}
                dataSource={commissions}
                pagination={false}
                empty={<Empty description='暂无佣金流水' />}
              />
              <Pagination
                currentPage={commissionPage}
                pageSize={commissionPageSize}
                total={commissionTotal}
                onPageChange={(page) => {
                  setCommissionPage(page);
                  void loadCommissions(page, commissionPageSize);
                }}
                onPageSizeChange={(pageSize) => {
                  setCommissionPage(1);
                  setCommissionPageSize(pageSize);
                  void loadCommissions(1, pageSize);
                }}
              />
            </Space>
          </Card>
        ) : section === 'withdrawals' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Select
                value={withdrawalStatus}
                onChange={setWithdrawalStatus}
                optionList={WITHDRAWAL_STATUS_OPTIONS}
              />
              <Button onClick={() => void loadWithdrawals()}>刷新列表</Button>
              <Table
                rowKey='id'
                columns={withdrawalColumns}
                dataSource={withdrawals}
                pagination={false}
                empty={<Empty description='暂无提现记录' />}
              />
              <Pagination
                currentPage={withdrawalPage}
                pageSize={withdrawalPageSize}
                total={withdrawalTotal}
                onPageChange={(page) => {
                  setWithdrawalPage(page);
                  void loadWithdrawals(page, withdrawalPageSize);
                }}
                onPageSizeChange={(pageSize) => {
                  setWithdrawalPage(1);
                  setWithdrawalPageSize(pageSize);
                  void loadWithdrawals(1, pageSize);
                }}
              />
            </Space>
          </Card>
        ) : section === 'ledgers' ? (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Input
                value={ledgerKeyword}
                onChange={setLedgerKeyword}
                placeholder='搜索账户流水备注 / 业务引用'
              />
              <Button onClick={() => void loadLedgers()}>刷新列表</Button>
              <Table
                rowKey='id'
                columns={ledgerColumns}
                dataSource={ledgers}
                pagination={false}
                empty={<Empty description='暂无账户流水' />}
              />
              <Pagination
                currentPage={ledgerPage}
                pageSize={ledgerPageSize}
                total={ledgerTotal}
                onPageChange={(page) => {
                  setLedgerPage(page);
                  void loadLedgers(page, ledgerPageSize);
                }}
                onPageSizeChange={(pageSize) => {
                  setLedgerPage(1);
                  setLedgerPageSize(pageSize);
                  void loadLedgers(1, pageSize);
                }}
              />
            </Space>
          </Card>
        ) : (
          <Card className='!rounded-2xl shadow-sm border-0'>
            <Space vertical style={{ width: '100%' }} spacing='loose'>
              <Input
                value={auditKeyword}
                onChange={setAuditKeyword}
                placeholder='搜索审计动作 / 原因'
              />
              <Button onClick={() => void loadAuditLogs()}>刷新列表</Button>
              <Table
                rowKey='id'
                columns={auditColumns}
                dataSource={auditLogs}
                pagination={false}
                empty={<Empty description='暂无审计日志' />}
              />
              <Pagination
                currentPage={auditPage}
                pageSize={auditPageSize}
                total={auditTotal}
                onPageChange={(page) => {
                  setAuditPage(page);
                  void loadAuditLogs(page, auditPageSize);
                }}
                onPageSizeChange={(pageSize) => {
                  setAuditPage(1);
                  setAuditPageSize(pageSize);
                  void loadAuditLogs(1, pageSize);
                }}
              />
            </Space>
          </Card>
        )}

        <Modal
          title={actionDialogTitleMap[actionDialog.kind] || '推广员操作'}
          visible={actionDialog.visible}
          onCancel={() => {
            setActionDialog({ visible: false, kind: '', item: null });
            setReasonInput('');
            setRateOverrideInput('');
            setAdjustAmountInput('');
          }}
          onOk={submitAffiliateAction}
          okText='确认'
          cancelText='取消'
        >
          <Space vertical style={{ width: '100%' }} spacing='loose'>
            {showReasonField && (
              <Input
                value={reasonInput}
                onChange={setReasonInput}
                placeholder='原因 / 备注'
              />
            )}
            {showRateField && (
              <Input
                value={rateOverrideInput}
                onChange={setRateOverrideInput}
                placeholder='返佣比例，可留空表示使用默认比例'
              />
            )}
            {showAdjustField && (
              <Input
                value={adjustAmountInput}
                onChange={setAdjustAmountInput}
                placeholder='调账金额'
              />
            )}
          </Space>
        </Modal>

        <Modal
          title={
            withdrawalActionDialog.kind === 'approve'
              ? '审核通过提现'
              : withdrawalActionDialog.kind === 'reject'
                ? '拒绝提现'
                : '标记已打款'
          }
          visible={withdrawalActionDialog.visible}
          onCancel={() => {
            setWithdrawalActionDialog({ visible: false, kind: '', item: null });
            setWithdrawalAdminNote('');
            setWithdrawalRejectReason('');
            setWithdrawalPaymentTxnNo('');
            setWithdrawalPaymentProofURL('');
          }}
          onOk={submitWithdrawalAction}
          okText='确认'
          cancelText='取消'
        >
          <Space vertical style={{ width: '100%' }} spacing='loose'>
            <Input
              value={withdrawalAdminNote}
              onChange={setWithdrawalAdminNote}
              placeholder='管理员备注'
            />
            {withdrawalActionDialog.kind === 'reject' && (
              <Input
                value={withdrawalRejectReason}
                onChange={setWithdrawalRejectReason}
                placeholder='拒绝原因'
              />
            )}
            {withdrawalActionDialog.kind === 'pay' && (
              <>
                <Input
                  value={withdrawalPaymentTxnNo}
                  onChange={setWithdrawalPaymentTxnNo}
                  placeholder='打款流水号'
                />
                <Input
                  value={withdrawalPaymentProofURL}
                  onChange={setWithdrawalPaymentProofURL}
                  placeholder='打款凭证 URL'
                />
                <input
                  type='file'
                  accept='image/*'
                  onChange={handleUploadPaymentProof}
                />
              </>
            )}
          </Space>
        </Modal>

        <Modal
          title={
            detailTarget
              ? `推广员详情：${detailTarget.username || detailTarget.email || '-'}`
              : '推广员详情'
          }
          visible={detailVisible}
          onCancel={() => {
            setDetailVisible(false);
            setDetailTarget(null);
            setBindingItems([]);
            setAffiliateWithdrawals([]);
          }}
          footer={null}
          width={960}
        >
          {detailLoading ? (
            <Text type='tertiary'>加载中...</Text>
          ) : detailMode === 'bindings' ? (
            <Table
              rowKey='id'
              pagination={false}
              dataSource={bindingItems}
              empty={<Empty description='暂无绑定记录' />}
              columns={[
                { title: '被邀请用户 ID', dataIndex: 'invitee_user_id' },
                { title: '被邀请用户名', dataIndex: 'invitee_username' },
                {
                  title: '邮箱',
                  dataIndex: 'invitee_email',
                  render: (value) => value || '-',
                },
                {
                  title: '绑定时间',
                  dataIndex: 'bound_at',
                  render: (value) => formatTime(value),
                },
              ]}
            />
          ) : (
            <Table
              rowKey='id'
              pagination={false}
              dataSource={affiliateWithdrawals}
              empty={<Empty description='暂无提现记录' />}
              columns={withdrawalColumns}
            />
          )}
        </Modal>
      </div>
    </div>
  );
}
