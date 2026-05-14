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

import React from 'react';
import {
  Avatar,
  Button,
  Card,
  Input,
  Space,
  Tag,
  Typography,
} from '@douyinfe/semi-ui';
import { ArrowRight, Copy, Gift, Users, Wallet } from 'lucide-react';

const { Text } = Typography;

function buildStatusTag(status, t) {
  const map = {
    pending: { color: 'orange', text: t('待审核') },
    approved: { color: 'green', text: t('已通过') },
    rejected: { color: 'red', text: t('已拒绝') },
    disabled: { color: 'grey', text: t('已禁用') },
  };
  const item = map[status] || { color: 'grey', text: status || '-' };
  return <Tag color={item.color}>{item.text}</Tag>;
}

const InvitationCard = ({
  t,
  profile,
  summary,
  affLink,
  handleAffLinkClick,
  onOpenCenter,
  referralEnabled = false,
  referralRequireApproval = true,
}) => {
  const hasDashboard =
    profile?.status === 'approved' || profile?.status === 'disabled';
  const statusTextMap = {
    pending: t('待审核'),
    approved: t('已通过'),
    rejected: t('已拒绝'),
    disabled: t('已禁用'),
  };

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='green' className='mr-3 shadow-md'>
          <Gift size={16} />
        </Avatar>
        <div>
          <Typography.Text className='text-lg font-medium'>
            {t('推广中心')}
          </Typography.Text>
          <div className='text-xs'>
            {t('查看推广状态、佣金明细和提现入口')}
          </div>
        </div>
      </div>

      <Space vertical style={{ width: '100%' }}>
        <Card
          className='!rounded-xl w-full'
          cover={
            <div
              className='relative h-30'
              style={{
                '--palette-primary-darkerChannel': '0 75 80',
                backgroundImage:
                  "linear-gradient(0deg, rgba(var(--palette-primary-darkerChannel) / 80%), rgba(var(--palette-primary-darkerChannel) / 80%)), url('/cover-4.webp')",
                backgroundSize: 'cover',
                backgroundPosition: 'center',
                backgroundRepeat: 'no-repeat',
              }}
            >
              <div className='relative z-10 h-full flex flex-col justify-between p-4'>
                <div className='flex justify-between items-center'>
                  <Text strong style={{ color: 'white', fontSize: '16px' }}>
                    {t('推广概览')}
                  </Text>
                  <Button
                    type='primary'
                    theme='solid'
                    size='small'
                    onClick={onOpenCenter}
                    className='!rounded-lg'
                  >
                    <ArrowRight size={12} className='mr-1' />
                    {t('打开推广中心')}
                  </Button>
                </div>
                {!referralEnabled && (
                  <Text
                    style={{
                      color: 'rgba(255,255,255,0.8)',
                      fontSize: 12,
                    }}
                  >
                    {t('推广返佣功能当前未启用，请联系管理员检查配置。')}
                  </Text>
                )}
                {referralEnabled && !hasDashboard && (
                  <Text
                    style={{
                      color: 'rgba(255,255,255,0.8)',
                      fontSize: 12,
                    }}
                  >
                    {referralRequireApproval
                      ? t('提交推广申请并审核通过后即可开始推广。')
                      : t('进入推广中心后可直接开始使用。')}
                  </Text>
                )}

                <div className='grid grid-cols-3 gap-6 mt-4'>
                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {hasDashboard
                        ? Number(summary?.pending_amount || 0).toFixed(2)
                        : statusTextMap[profile?.status] || t('未申请')}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <Wallet
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {hasDashboard ? t('待结算') : t('状态')}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {hasDashboard
                        ? Number(summary?.available_amount || 0).toFixed(2)
                        : summary?.invite_code || '-'}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <Gift
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {hasDashboard ? t('可提现') : t('推广码')}
                      </Text>
                    </div>
                  </div>

                  <div className='text-center'>
                    <div
                      className='text-base sm:text-2xl font-bold mb-2'
                      style={{ color: 'white' }}
                    >
                      {hasDashboard
                        ? summary?.bound_user_count || 0
                        : summary?.paid_user_count || 0}
                    </div>
                    <div className='flex items-center justify-center text-sm'>
                      <Users
                        size={14}
                        className='mr-1'
                        style={{ color: 'rgba(255,255,255,0.8)' }}
                      />
                      <Text
                        style={{
                          color: 'rgba(255,255,255,0.8)',
                          fontSize: '12px',
                        }}
                      >
                        {hasDashboard ? t('邀请人数') : t('付费用户')}
                      </Text>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          }
        >
          <Input
            value={affLink}
            readonly
            className='!rounded-lg'
            prefix={t('邀请链接')}
            suffix={
              <Button
                type='primary'
                theme='solid'
                onClick={handleAffLinkClick}
                icon={<Copy size={14} />}
                className='!rounded-lg'
                disabled={!affLink}
              >
                {t('复制')}
              </Button>
            }
          />
        </Card>

        <Card
          className='!rounded-xl w-full'
          title={<Text type='tertiary'>{t('推广说明')}</Text>}
        >
          <div className='space-y-3'>
            <Text type='tertiary' className='text-sm'>
              {t('推广入口已独立为推广中心，在那里可以查看返佣列表、提现申请和提现记录。')}
            </Text>
            <Text type='tertiary' className='text-sm'>
              {t('若您尚未开通推广账号，请先前往推广中心提交申请。')}
            </Text>
            {profile?.status && <div>{buildStatusTag(profile.status, t)}</div>}
            {profile?.risk_reason && (
              <Text type='tertiary' className='text-sm'>
                {profile.risk_reason}
              </Text>
            )}
            <Button type='primary' theme='solid' onClick={onOpenCenter}>
              {t('前往推广中心')}
            </Button>
          </div>
        </Card>
      </Space>
    </Card>
  );
};

export default InvitationCard;
