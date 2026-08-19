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

export const DEFAULT_ADMIN_CONFIG = {
  chat: {
    enabled: true,
    playground: true,
    chat: true,
  },
  console: {
    enabled: true,
    detail: true,
    token: true,
    model_check: true,
    log: true,
    midjourney: true,
    image_tasks: true,
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
    redemption: true,
    user: true,
    subscription: true,
    adminReferral: true,
    ticket_management: true,
    setting: true,
  },
};

const deepClone = (value) => JSON.parse(JSON.stringify(value));
const removedConsoleModuleKeys = ['image2'];

const removedAdminModuleKeys = [
  'riskCenter',
  'risk_center',
  'providerPricing',
  'provider_price_export',
];

export const sanitizeSidebarConfig = (config) => {
  if (!config || typeof config !== 'object') return config;
  const sanitized = { ...config };
  if (sanitized.console?.tickets !== undefined) {
    sanitized.personal = { enabled: true, ...sanitized.personal };
    if (sanitized.personal.tickets === undefined) {
      sanitized.personal.tickets = sanitized.console.tickets;
    }
    sanitized.console = { ...sanitized.console };
    delete sanitized.console.tickets;
  }
  if (sanitized.console && typeof sanitized.console === 'object') {
    sanitized.console = { ...sanitized.console };
    removedConsoleModuleKeys.forEach((key) => {
      delete sanitized.console[key];
    });
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

export const mergeAdminConfig = (savedConfig) => {
  const merged = deepClone(DEFAULT_ADMIN_CONFIG);
  if (!savedConfig || typeof savedConfig !== 'object') return merged;
  const hasLegacyTickets = savedConfig.console?.tickets !== undefined;
  const hasPersonalTickets = Object.hasOwn(
    savedConfig.personal || {},
    'tickets',
  );

  for (const [sectionKey, sectionConfig] of Object.entries(savedConfig)) {
    if (!sectionConfig || typeof sectionConfig !== 'object') continue;

    if (!merged[sectionKey]) {
      merged[sectionKey] = { ...sectionConfig };
      continue;
    }

    merged[sectionKey] = { ...merged[sectionKey], ...sectionConfig };
  }

  if (merged.admin) {
    if (
      savedConfig.admin?.adminReferral === undefined &&
      merged.admin.referral !== undefined
    ) {
      merged.admin.adminReferral = merged.admin.referral ?? true;
    }
    if (
      savedConfig.admin?.recharge_audit === undefined &&
      merged.admin.order_management !== undefined
    ) {
      merged.admin.recharge_audit = merged.admin.order_management ?? true;
    }
    merged.admin.setting = true;
    removedAdminModuleKeys.forEach((key) => {
      delete merged.admin[key];
    });
  }

  if (merged.console?.tickets !== undefined) {
    merged.personal = { enabled: true, ...merged.personal };
    if (hasLegacyTickets && !hasPersonalTickets) {
      merged.personal.tickets = savedConfig.console.tickets;
    } else if (merged.personal.tickets === undefined) {
      merged.personal.tickets = merged.console.tickets;
    }
    merged.console = { ...merged.console };
    delete merged.console.tickets;
  }
  if (merged.console) {
    merged.console = { ...merged.console };
    removedConsoleModuleKeys.forEach((key) => {
      delete merged.console[key];
    });
  }

  return merged;
};
