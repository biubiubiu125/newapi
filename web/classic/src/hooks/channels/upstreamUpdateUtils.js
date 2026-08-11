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
import { supportsChannelUpstreamModelUpdate } from '../../constants/channel.constants.js';

const UPSTREAM_MODEL_UPDATE_SETTING_KEYS = [
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
  'upstream_model_update_last_check_time',
  'upstream_model_update_last_detected_models',
  'upstream_model_update_last_removed_models',
];

export const normalizeModelList = (models = []) => {
  const source = Array.isArray(models)
    ? models
    : typeof models === 'string'
      ? models.split(',')
      : [];
  return [
    ...new Set(
      source.map((model) => String(model || '').trim()).filter(Boolean),
    ),
  ];
};

export const getDefaultUpstreamUpdateSelection = ({
  addModels = [],
  removeModels = [],
} = {}) => ({
  addModels: normalizeModelList(addModels),
  removeModels: normalizeModelList(removeModels),
});

export const buildClassicUpstreamUpdateSupportChannel = (
  inputs = {},
  { isEdit = false, batch = false, multiToSingle = false } = {},
) => ({
  ...inputs,
  is_draft_multi_key: !isEdit && batch === true && multiToSingle === true,
});

export const normalizeClassicFetchModelsDraftSnapshot = (payload = {}) => ({
  base_url: String(payload.base_url || ''),
  type: String(payload.type ?? ''),
  key: String(payload.key || '').trim(),
  setting: String(payload.setting || ''),
  settings: String(payload.settings || ''),
  header_override: String(payload.header_override || ''),
  other: String(payload.other || ''),
});

export const getClassicFetchModelsCacheKey = (payload = {}) =>
  JSON.stringify(normalizeClassicFetchModelsDraftSnapshot(payload));

export const shouldUseClassicDraftFetchModels = ({
  isEdit = false,
  draftHasChanges = false,
  canFetchSavedModels = false,
} = {}) => {
  if (!isEdit) return true;
  return draftHasChanges || !canFetchSavedModels;
};

export const shouldRefreshClassicFetchedModelsCache = ({
  cachedModels,
  cachedKey,
  currentKey,
} = {}) =>
  !Array.isArray(cachedModels) ||
  cachedModels.length === 0 ||
  cachedKey !== currentKey;

export const getClassicFetchModelsFailureMessage = (
  source,
  fallback = '获取模型列表失败',
) => {
  const candidates = [
    source?.data?.message,
    source?.response?.data?.message,
    source?.message,
  ];
  const message = candidates.find(
    (candidate) =>
      typeof candidate === 'string' && candidate.trim().length > 0,
  );
  return message ? message.trim() : fallback;
};

export const canUseClassicChannelUpstreamUpdates = (
  channel = {},
  upstreamUpdateMeta = {},
) =>
  Boolean(
    channel &&
      supportsChannelUpstreamModelUpdate(channel) &&
      Number(channel.status) === 1 &&
      upstreamUpdateMeta?.enabled === true,
  );

export const canFetchClassicChannelUpstreamModels = ({
  canSensitiveWriteChannel = false,
} = {}) => canSensitiveWriteChannel === true;

export const canOpenClassicModelMappingValueFetch = ({
  supportsUpstreamModelUpdate = false,
  canFetchUpstreamModels = false,
} = {}) =>
  supportsUpstreamModelUpdate === true && canFetchUpstreamModels === true;

export const buildClassicChannelUpstreamUpdateSettings = ({
  currentSettings = {},
  inputs = {},
} = {}) => {
  const settings =
    currentSettings && typeof currentSettings === 'object'
      ? { ...currentSettings }
      : {};
  const channelType = Number(inputs.type);
  if (!Number.isFinite(channelType)) {
    UPSTREAM_MODEL_UPDATE_SETTING_KEYS.forEach((key) => {
      delete settings[key];
    });
    return settings;
  }
  if (
    !supportsChannelUpstreamModelUpdate({
      ...inputs,
      type: channelType,
    })
  ) {
    UPSTREAM_MODEL_UPDATE_SETTING_KEYS.forEach((key) => {
      delete settings[key];
    });
    return settings;
  }
  settings.upstream_model_update_check_enabled =
    inputs.upstream_model_update_check_enabled === true;
  settings.upstream_model_update_auto_sync_enabled =
    settings.upstream_model_update_check_enabled === true &&
    inputs.upstream_model_update_auto_sync_enabled === true;
  settings.upstream_model_update_ignored_models = normalizeModelList(
    String(inputs.upstream_model_update_ignored_models || '').split(','),
  );
  if (
    !Array.isArray(settings.upstream_model_update_last_detected_models) ||
    settings.upstream_model_update_check_enabled !== true
  ) {
    settings.upstream_model_update_last_detected_models = [];
  }
  if (
    !Array.isArray(settings.upstream_model_update_last_removed_models) ||
    settings.upstream_model_update_check_enabled !== true
  ) {
    settings.upstream_model_update_last_removed_models = [];
  }
  if (settings.upstream_model_update_check_enabled !== true) {
    settings.upstream_model_update_last_check_time = 0;
  } else if (
    typeof settings.upstream_model_update_last_check_time !== 'number'
  ) {
    settings.upstream_model_update_last_check_time = 0;
  }
  return settings;
};

export const parseUpstreamUpdateMeta = (settings) => {
  let parsed = null;
  if (settings && typeof settings === 'object') {
    parsed = settings;
  } else if (typeof settings === 'string') {
    try {
      parsed = JSON.parse(settings);
    } catch {
      parsed = null;
    }
  }

  if (!parsed || typeof parsed !== 'object') {
    return {
      enabled: false,
      pendingAddModels: [],
      pendingRemoveModels: [],
    };
  }

  return {
    enabled: parsed.upstream_model_update_check_enabled === true,
    pendingAddModels: normalizeModelList(
      parsed.upstream_model_update_last_detected_models,
    ),
    pendingRemoveModels: normalizeModelList(
      parsed.upstream_model_update_last_removed_models,
    ),
  };
};
