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
import { useTranslation } from 'react-i18next'
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
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const updateBoolean = async (key: string, value: boolean) => {
    try {
      await updateOption.mutateAsync({ key, value, skipToast: true })
      toast.success(t('Ticket notification settings saved'))
    } catch {
      toast.error(t('Failed to save ticket notification settings'))
    }
  }

  return (
    <SettingsSection
      title={t('Ticket Notifications')}
      description={t(
        'Ticket badges are enabled by default. This only controls ordinary email reminders and does not enable a separate in-app message center.'
      )}
    >
      <SettingsSwitchField
        checked={emailEnabled}
        onCheckedChange={(value) =>
          updateBoolean('TicketEmailNotificationEnabled', value)
        }
        label={t('Email notifications')}
        description={t(
          'New tickets notify admins, and admin replies notify users. User replies do not send email. Emails only include a reminder and an in-site link. Configure the site address in System Info before enabling this.'
        )}
        disabled={updateOption.isPending}
      />
    </SettingsSection>
  )
}
