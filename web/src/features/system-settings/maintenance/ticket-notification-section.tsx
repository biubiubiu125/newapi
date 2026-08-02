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

import { toast } from 'sonner'
import { SettingsSwitchField } from '../components/settings-form-layout'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

type TicketNotificationSectionProps = {
  emailEnabled: boolean
}

export function TicketNotificationSection({
  emailEnabled,
}: TicketNotificationSectionProps) {
  const updateOption = useUpdateOption()

  const updateBoolean = async (key: string, value: boolean) => {
    try {
      await updateOption.mutateAsync({ key, value, skipToast: true })
      toast.success('工单通知设置已保存')
    } catch {
      toast.error('工单通知设置保存失败')
    }
  }

  return (
    <SettingsSection
      title='工单通知'
      description='工单角标默认启用；这里仅控制普通邮件提醒，不启用独立站内消息中心。'
    >
      <SettingsSwitchField
        checked={emailEnabled}
        onCheckedChange={(value) =>
          updateBoolean('TicketEmailNotificationEnabled', value)
        }
        label='邮件通知'
        description='新工单通知管理员，管理员回复通知用户；用户回复不发送邮件，邮件只包含提醒和站内链接。开启前请先在系统信息中配置站点地址。'
        disabled={updateOption.isPending}
      />
    </SettingsSection>
  )
}
