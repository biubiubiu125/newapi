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

export const hasValidSyncPreview = (previewData) =>
  previewData !== null && previewData !== undefined;

export const getSyncPreviewDecision = (previewData) => {
  if (!hasValidSyncPreview(previewData)) {
    return {
      shouldShowConflict: false,
      shouldSync: false,
      conflicts: [],
    };
  }

  const conflicts = Array.isArray(previewData.conflicts)
    ? previewData.conflicts
    : [];

  return {
    shouldShowConflict: conflicts.length > 0,
    shouldSync: conflicts.length === 0,
    conflicts,
  };
};

export const buildUpstreamConflictSubmitPayload = (
  selections = {},
  syncMissing = true,
  missing,
) => {
  const overwrite = Object.entries(selections)
    .map(([modelName, set]) => ({
      model_name: modelName,
      fields: [...(set || [])],
    }))
    .filter((x) => x.fields.length > 0);

  if (overwrite.length === 0 && !syncMissing) {
    return null;
  }

  const payload = {
    overwrite,
    skip_missing: !syncMissing,
  };

  if (Array.isArray(missing)) {
    payload.missing = [...missing];
  }

  return payload;
};

export const runClassicSyncWizardFlow = async ({
  locale,
  source = 'official',
  previewUpstreamDiff,
  syncUpstream,
}) => {
  const data = await previewUpstreamDiff?.({ locale, source });
  const decision = getSyncPreviewDecision(data);
  const missing = Array.isArray(data?.missing) ? data.missing : [];

  if (decision.shouldShowConflict) {
    return {
      status: 'conflict',
      conflicts: decision.conflicts,
      missing,
    };
  }

  if (!decision.shouldSync) {
    return { status: 'preview_failed' };
  }

  const ok = await syncUpstream?.({ locale, source, missing });
  return ok ? { status: 'synced' } : { status: 'sync_failed' };
};

export const runClassicPostSyncRefresh = async ({
  refreshVendors,
  refreshModels,
  refreshMissing,
  refreshPricing,
}) => {
  const refreshers = [
    refreshVendors,
    refreshModels,
    refreshMissing,
    refreshPricing,
  ];
  let allLoaded = true;

  for (const refresh of refreshers) {
    try {
      const loaded = await refresh?.();
      if (!loaded) {
        allLoaded = false;
      }
    } catch {
      allLoaded = false;
    }
  }

  return allLoaded;
};

export const MODEL_PRICING_REFRESH_EVENT = 'newapi:model-pricing-refresh';

export const notifyModelPricingChanged = (
  target = globalThis.window,
  EventCtor = globalThis.Event,
) => {
  if (!target?.dispatchEvent || typeof EventCtor !== 'function') {
    return false;
  }
  return target.dispatchEvent(new EventCtor(MODEL_PRICING_REFRESH_EVENT));
};
