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

export const normalizeModelList = (models = []) => [
  ...new Set(
    (models || []).map((model) => String(model || '').trim()).filter(Boolean),
  ),
];

export const getDefaultUpstreamUpdateSelection = ({
  addModels = [],
  removeModels = [],
} = {}) => ({
  addModels: normalizeModelList(addModels),
  removeModels: normalizeModelList(removeModels),
});

export const buildClassicChannelUpstreamUpdateSettings = ({
  currentSettings = {},
  inputs = {},
} = {}) => {
  const settings =
    currentSettings && typeof currentSettings === 'object'
      ? { ...currentSettings }
      : {};
  if (
    typeof inputs.type === 'number' &&
    !supportsChannelUpstreamModelUpdate(inputs)
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
  if (typeof settings.upstream_model_update_last_check_time !== 'number') {
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
