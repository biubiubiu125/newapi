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

import React, { useEffect, useState } from 'react';
import { Button, Form, Spin, Typography } from '@douyinfe/semi-ui';
import { API, showError, showSuccess, toBoolean } from '../../../helpers';

export default function SettingsTicketNotification(props) {
  const [loading, setLoading] = useState(false);
  const [emailEnabled, setEmailEnabled] = useState(false);

  useEffect(() => {
    setEmailEnabled(toBoolean(props.options?.TicketEmailNotificationEnabled));
  }, [props.options?.TicketEmailNotificationEnabled]);

  async function onSubmit() {
    try {
      setLoading(true);
      const res = await API.put('/api/option/', {
        key: 'TicketEmailNotificationEnabled',
        value: String(emailEnabled),
      });
      if (!res?.data?.success) {
        showError(res?.data?.message || '工单通知设置保存失败');
        return;
      }
      showSuccess('工单通知设置已保存');
      props.refresh?.();
    } catch (error) {
      showError(error.message || '工单通知设置保存失败');
    } finally {
      setLoading(false);
    }
  }

  return (
    <Spin spinning={loading}>
      <Form style={{ marginBottom: 15 }}>
        <Form.Section text='工单通知'>
          <Typography.Text
            type='tertiary'
            style={{ marginBottom: 16, display: 'block' }}
          >
            新工单通知管理员，管理员回复通知用户；用户回复不发送邮件。邮件只包含提醒和站内链接，开启前请先在系统信息中配置站点地址。
          </Typography.Text>
          <Form.Switch
            field='TicketEmailNotificationEnabled'
            label='邮件通知'
            checked={emailEnabled}
            checkedText='开'
            uncheckedText='关'
            onChange={setEmailEnabled}
          />
          <Button size='default' onClick={onSubmit}>
            保存工单通知
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
