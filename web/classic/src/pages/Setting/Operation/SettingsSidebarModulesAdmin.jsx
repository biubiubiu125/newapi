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

import React, { useState, useEffect, useContext } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Card,
  Form,
  Button,
  Switch,
  Row,
  Col,
  Typography,
} from '@douyinfe/semi-ui';
import { API, showSuccess, showError } from '../../../helpers';
import { StatusContext } from '../../../context/Status';

const { Text } = Typography;

const removedAdminModuleKeys = ['riskCenter', 'risk_center'];
const defaultSidebarModules = {
  chat: {
    enabled: true,
    playground: true,
    chat: true,
  },
  console: {
    enabled: true,
    detail: true,
    token: true,
    image2: true,
    model_check: true,
    log: true,
    midjourney: true,
    task: true,
  },
  personal: {
    enabled: true,
    topup: true,
    referral: true,
    tickets: true,
    personal: true,
  },
  admin: {
    enabled: true,
    channel: true,
    models: true,
    deployment: true,
    recharge_audit: true,
    providerPricing: true,
    redemption: true,
    user: true,
    subscription: true,
    adminReferral: true,
    ticket_management: true,
    setting: true,
  },
};

const sanitizeSidebarModulesConfig = (config) => {
  if (!config || typeof config !== 'object') return config;
  const sanitized = { ...config };
  if (sanitized.console?.tickets !== undefined) {
    sanitized.personal = { enabled: true, ...(sanitized.personal || {}) };
    if (sanitized.personal.tickets === undefined) {
      sanitized.personal.tickets = sanitized.console.tickets;
    }
    sanitized.console = { ...sanitized.console };
    delete sanitized.console.tickets;
  }
  if (sanitized.admin && typeof sanitized.admin === 'object') {
    sanitized.admin = { ...sanitized.admin };
    removedAdminModuleKeys.forEach((key) => {
      delete sanitized.admin[key];
    });
    sanitized.admin.setting = true;
  }
  return sanitized;
};

export default function SettingsSidebarModulesAdmin(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [statusState, statusDispatch] = useContext(StatusContext);

  // 左侧边栏模块管理状态（管理员全局控制）
  const [sidebarModulesAdmin, setSidebarModulesAdmin] = useState(
    defaultSidebarModules,
  );

  // 处理区域级别开关变更
  function handleSectionChange(sectionKey) {
    return (checked) => {
      const newModules = {
        ...sidebarModulesAdmin,
        [sectionKey]: {
          ...sidebarModulesAdmin[sectionKey],
          enabled: checked,
        },
      };
      setSidebarModulesAdmin(newModules);
    };
  }

  // 处理功能级别开关变更
  function handleModuleChange(sectionKey, moduleKey) {
    return (checked) => {
      const newModules = {
        ...sidebarModulesAdmin,
        [sectionKey]: {
          ...sidebarModulesAdmin[sectionKey],
          [moduleKey]: checked,
        },
      };
      setSidebarModulesAdmin(newModules);
    };
  }

  // 重置为默认配置
  function resetSidebarModules() {
    setSidebarModulesAdmin(defaultSidebarModules);
    showSuccess(t('已重置为默认配置'));
  }

  // 保存配置
  async function onSubmit() {
    setLoading(true);
    try {
      const sanitizedModules =
        sanitizeSidebarModulesConfig(sidebarModulesAdmin);
      const res = await API.put('/api/option/', {
        key: 'SidebarModulesAdmin',
        value: JSON.stringify(sanitizedModules),
      });
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('保存成功'));
        setSidebarModulesAdmin(sanitizedModules);

        // 立即更新StatusContext中的状态
        statusDispatch({
          type: 'set',
          payload: {
            ...statusState.status,
            SidebarModulesAdmin: JSON.stringify(sanitizedModules),
          },
        });

        // 刷新父组件状态
        if (props.refresh) {
          await props.refresh();
        }
      } else {
        showError(message);
      }
    } catch (error) {
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    // 从 props.options 中获取配置
    if (props.options && props.options.SidebarModulesAdmin) {
      try {
        const modules = JSON.parse(props.options.SidebarModulesAdmin);
        setSidebarModulesAdmin(
          sanitizeSidebarModulesConfig({
            ...defaultSidebarModules,
            ...modules,
            chat: {
              ...defaultSidebarModules.chat,
              ...(modules.chat || {}),
            },
            console: {
              ...defaultSidebarModules.console,
              ...(modules.console || {}),
            },
            personal: {
              ...defaultSidebarModules.personal,
              ...(modules.personal || {}),
            },
            admin: {
              ...defaultSidebarModules.admin,
              ...(modules.admin || {}),
              adminReferral:
                modules.admin?.adminReferral ?? modules.admin?.referral ?? true,
              providerPricing:
                modules.admin?.providerPricing ??
                modules.admin?.provider_price_export ??
                true,
              recharge_audit:
                modules.admin?.recharge_audit ??
                modules.admin?.order_management ??
                true,
            },
          }),
        );
      } catch (error) {
        setSidebarModulesAdmin(defaultSidebarModules);
      }
    }
  }, [props.options]);

  // 区域配置数据
  const sectionConfigs = [
    {
      key: 'chat',
      title: t('聊天区域'),
      description: t('操练场和聊天功能'),
      modules: [
        {
          key: 'playground',
          title: t('操练场'),
          description: t('AI模型测试环境'),
        },
        { key: 'chat', title: t('聊天'), description: t('聊天会话管理') },
      ],
    },
    {
      key: 'console',
      title: t('控制台区域'),
      description: t('数据管理和日志查看'),
      modules: [
        { key: 'detail', title: t('数据看板'), description: t('系统数据统计') },
        { key: 'token', title: t('令牌管理'), description: t('API令牌管理') },
        {
          key: 'image2',
          title: 'Image2生图',
          description: '外部图片生成入口',
        },
        {
          key: 'model_check',
          title: '模型状态监测',
          description: '外部模型状态监测入口',
        },
        { key: 'log', title: t('使用日志'), description: t('API使用记录') },
        {
          key: 'midjourney',
          title: t('绘图日志'),
          description: t('绘图任务记录'),
        },
        { key: 'task', title: t('任务日志'), description: t('系统任务记录') },
      ],
    },
    {
      key: 'personal',
      title: t('个人中心区域'),
      description: t('用户个人功能'),
      modules: [
        { key: 'topup', title: t('钱包管理'), description: t('余额充值管理') },
        {
          key: 'referral',
          title: t('推广中心'),
          description: t('推广链接、佣金和提现'),
        },
        {
          key: 'tickets',
          title: '工单中心',
          description: '用户创建、查看和回复工单',
        },
        {
          key: 'personal',
          title: t('个人设置'),
          description: t('个人信息设置'),
        },
      ],
    },
    {
      key: 'admin',
      title: t('管理员区域'),
      description: t('系统管理功能'),
      modules: [
        { key: 'channel', title: t('渠道管理'), description: t('API渠道配置') },
        { key: 'models', title: t('模型管理'), description: t('AI模型配置') },
        {
          key: 'deployment',
          title: t('模型部署'),
          description: t('模型部署管理'),
        },
        {
          key: 'subscription',
          title: t('订阅管理'),
          description: t('订阅套餐管理'),
        },
        {
          key: 'adminReferral',
          title: t('推广管理'),
          description: t('推广员、返佣和提现管理'),
        },
        {
          key: 'recharge_audit',
          title: '订单管理',
          description: '查看充值和订阅订单',
        },
        {
          key: 'ticket_management',
          title: '工单管理',
          description: '管理员查看和处理用户工单',
        },
        {
          key: 'providerPricing',
          title: t('Public Price Export'),
          description: t('Public provider pricing export'),
        },
        {
          key: 'redemption',
          title: t('兑换码管理'),
          description: t('兑换码生成管理'),
        },
        { key: 'user', title: t('用户管理'), description: t('用户账户管理') },
        {
          key: 'setting',
          title: t('系统设置'),
          description: t('系统设置为必需入口，不能隐藏'),
        },
      ],
    },
  ];

  return (
    <Card>
      <Form.Section
        text={t('侧边栏管理（全局控制）')}
        extraText={t(
          '全局控制侧边栏区域和功能显示，管理员隐藏的功能用户无法启用',
        )}
      >
        {sectionConfigs.map((section) => (
          <div key={section.key} style={{ marginBottom: '32px' }}>
            {/* 区域标题和总开关 */}
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                marginBottom: '16px',
                padding: '12px 16px',
                backgroundColor: 'var(--semi-color-fill-0)',
                borderRadius: '8px',
                border: '1px solid var(--semi-color-border)',
              }}
            >
              <div>
                <div
                  style={{
                    fontWeight: '600',
                    fontSize: '16px',
                    color: 'var(--semi-color-text-0)',
                    marginBottom: '4px',
                  }}
                >
                  {section.title}
                </div>
                <Text
                  type='secondary'
                  size='small'
                  style={{
                    fontSize: '12px',
                    color: 'var(--semi-color-text-2)',
                    lineHeight: '1.4',
                  }}
                >
                  {section.description}
                </Text>
              </div>
              <Switch
                checked={sidebarModulesAdmin[section.key]?.enabled}
                onChange={handleSectionChange(section.key)}
                size='default'
              />
            </div>

            {/* 功能模块网格 */}
            <Row gutter={[16, 16]}>
              {section.modules.map((module) => (
                <Col key={module.key} xs={24} sm={12} md={8} lg={6} xl={6}>
                  <Card
                    bodyStyle={{ padding: '16px' }}
                    hoverable
                    style={{
                      opacity:
                        sidebarModulesAdmin[section.key]?.enabled ||
                        module.key === 'setting'
                          ? 1
                          : 0.5,
                      transition: 'opacity 0.2s',
                    }}
                  >
                    <div
                      style={{
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                        height: '100%',
                      }}
                    >
                      <div style={{ flex: 1, textAlign: 'left' }}>
                        <div
                          style={{
                            fontWeight: '600',
                            fontSize: '14px',
                            color: 'var(--semi-color-text-0)',
                            marginBottom: '4px',
                          }}
                        >
                          {module.title}
                        </div>
                        <Text
                          type='secondary'
                          size='small'
                          style={{
                            fontSize: '12px',
                            color: 'var(--semi-color-text-2)',
                            lineHeight: '1.4',
                            display: 'block',
                          }}
                        >
                          {module.description}
                        </Text>
                      </div>
                      <div style={{ marginLeft: '16px' }}>
                        <Switch
                          checked={
                            module.key === 'setting'
                              ? true
                              : sidebarModulesAdmin[section.key]?.[module.key]
                          }
                          onChange={
                            module.key === 'setting'
                              ? undefined
                              : handleModuleChange(section.key, module.key)
                          }
                          size='default'
                          disabled={
                            module.key === 'setting' ||
                            !sidebarModulesAdmin[section.key]?.enabled
                          }
                        />
                      </div>
                    </div>
                  </Card>
                </Col>
              ))}
            </Row>
          </div>
        ))}

        <div
          style={{
            display: 'flex',
            gap: '12px',
            justifyContent: 'flex-start',
            alignItems: 'center',
            paddingTop: '8px',
            borderTop: '1px solid var(--semi-color-border)',
          }}
        >
          <Button
            size='default'
            type='tertiary'
            onClick={resetSidebarModules}
            style={{
              borderRadius: '6px',
              fontWeight: '500',
            }}
          >
            {t('重置为默认')}
          </Button>
          <Button
            size='default'
            type='primary'
            onClick={onSubmit}
            loading={loading}
            style={{
              borderRadius: '6px',
              fontWeight: '500',
              minWidth: '100px',
            }}
          >
            {t('保存设置')}
          </Button>
        </div>
      </Form.Section>
    </Card>
  );
}
