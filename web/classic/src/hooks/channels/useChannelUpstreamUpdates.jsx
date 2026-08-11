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

import { useEffect, useRef, useState } from 'react';

import { API, showError, showInfo, showSuccess } from '../../helpers';
import {
  clearPersistedModelUpdateTaskId,
  getPersistedModelUpdateTaskId,
  getModelUpdateTaskErrorPayload,
  getModelUpdateTaskStartInfo,
  setPersistedModelUpdateTaskId,
  shouldClearPersistedModelUpdateTaskIdAfterTaskResult,
  shouldClearPersistedModelUpdateTaskIdAfterPollingError,
  waitForModelUpdateTask,
} from './upstreamUpdateTask';
import { normalizeModelList } from './upstreamUpdateUtils';

const getManualIgnoredModelCountFromSettings = (settings) => {
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
    return 0;
  }
  return normalizeModelList(parsed.upstream_model_update_ignored_models).length;
};

const countRemainingRemoveModels = (results) => {
  if (!Array.isArray(results)) return 0;
  return results.reduce((total, item) => {
    if (!item || typeof item !== 'object') return total;
    return total + normalizeModelList(item.remaining_remove_models).length;
  }, 0);
};

export const useChannelUpstreamUpdates = ({
  t,
  refresh,
  channelPermissions = {},
}) => {
  const [showUpstreamUpdateModal, setShowUpstreamUpdateModal] = useState(false);
  const [upstreamUpdateChannel, setUpstreamUpdateChannel] = useState(null);
  const [upstreamUpdateAddModels, setUpstreamUpdateAddModels] = useState([]);
  const [upstreamUpdateRemoveModels, setUpstreamUpdateRemoveModels] = useState(
    [],
  );
  const [upstreamUpdatePreferredTab, setUpstreamUpdatePreferredTab] =
    useState('add');
  const [upstreamApplyLoading, setUpstreamApplyLoading] = useState(false);
  const [detectAllUpstreamUpdatesLoading, setDetectAllUpstreamUpdatesLoading] =
    useState(false);
  const [applyAllUpstreamUpdatesLoading, setApplyAllUpstreamUpdatesLoading] =
    useState(false);

  const applyUpstreamUpdatesInFlightRef = useRef(false);
  const detectChannelUpstreamUpdatesInFlightRef = useRef(false);
  const detectAllUpstreamUpdatesInFlightRef = useRef(false);
  const applyAllUpstreamUpdatesInFlightRef = useRef(false);
  const mountedRef = useRef(true);
  const modelUpdateTaskGenerationRef = useRef(0);
  const modelUpdateTaskAbortControllerRef = useRef(null);
  const didResumePersistedModelUpdateTaskRef = useRef(false);
  const canDetectUpstreamUpdates =
    channelPermissions.canOperateChannel === true;
  const canApplyUpstreamUpdates = channelPermissions.canWriteChannel === true;

  const isCurrentModelUpdateTaskRun = (generation) =>
    mountedRef.current && modelUpdateTaskGenerationRef.current === generation;

  const beginModelUpdateTaskRun = (taskId) => {
    modelUpdateTaskAbortControllerRef.current?.abort();
    const controller = new AbortController();
    modelUpdateTaskAbortControllerRef.current = controller;
    modelUpdateTaskGenerationRef.current += 1;
    const generation = modelUpdateTaskGenerationRef.current;
    setPersistedModelUpdateTaskId(taskId);
    detectAllUpstreamUpdatesInFlightRef.current = true;
    setDetectAllUpstreamUpdatesLoading(true);
    return { controller, generation };
  };

  const finishModelUpdateTaskRun = (generation, clearTaskId = false) => {
    if (!isCurrentModelUpdateTaskRun(generation)) return;
    if (clearTaskId) {
      clearPersistedModelUpdateTaskId();
    }
    modelUpdateTaskAbortControllerRef.current = null;
    detectAllUpstreamUpdatesInFlightRef.current = false;
    setDetectAllUpstreamUpdatesLoading(false);
  };

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      modelUpdateTaskGenerationRef.current += 1;
      modelUpdateTaskAbortControllerRef.current?.abort();
      modelUpdateTaskAbortControllerRef.current = null;
    };
  }, []);

  const openUpstreamUpdateModal = (
    record,
    pendingAddModels = [],
    pendingRemoveModels = [],
    preferredTab = 'add',
  ) => {
    if (!canApplyUpstreamUpdates) {
      showError(t('无权限处理上游模型更新'));
      return;
    }

    const normalizedAddModels = normalizeModelList(pendingAddModels);
    const normalizedRemoveModels = normalizeModelList(pendingRemoveModels);
    if (
      !record?.id ||
      (normalizedAddModels.length === 0 && normalizedRemoveModels.length === 0)
    ) {
      showInfo(t('该渠道暂无可处理的上游模型更新'));
      return;
    }
    setUpstreamUpdateChannel(record);
    setUpstreamUpdateAddModels(normalizedAddModels);
    setUpstreamUpdateRemoveModels(normalizedRemoveModels);
    const normalizedPreferredTab = preferredTab === 'remove' ? 'remove' : 'add';
    setUpstreamUpdatePreferredTab(normalizedPreferredTab);
    setShowUpstreamUpdateModal(true);
  };

  const closeUpstreamUpdateModal = () => {
    setShowUpstreamUpdateModal(false);
    setUpstreamUpdateChannel(null);
    setUpstreamUpdateAddModels([]);
    setUpstreamUpdateRemoveModels([]);
    setUpstreamUpdatePreferredTab('add');
  };

  const applyUpstreamUpdates = async ({
    addModels: selectedAddModels = [],
    removeModels: selectedRemoveModels = [],
  } = {}) => {
    if (!canApplyUpstreamUpdates) {
      showError(t('无权限处理上游模型更新'));
      return;
    }

    if (applyUpstreamUpdatesInFlightRef.current) {
      showInfo(t('正在处理，请稍候'));
      return;
    }
    if (!upstreamUpdateChannel?.id) {
      closeUpstreamUpdateModal();
      return;
    }
    applyUpstreamUpdatesInFlightRef.current = true;
    setUpstreamApplyLoading(true);

    try {
      const normalizedSelectedAddModels = normalizeModelList(selectedAddModels);
      const normalizedSelectedRemoveModels =
        normalizeModelList(selectedRemoveModels);
      const selectedAddSet = new Set(normalizedSelectedAddModels);
      const ignoreModels = upstreamUpdateAddModels.filter(
        (model) => !selectedAddSet.has(model),
      );

      const res = await API.post(
        '/api/channel/upstream_updates/apply',
        {
          id: upstreamUpdateChannel.id,
          add_models: normalizedSelectedAddModels,
          ignore_models: ignoreModels,
          remove_models: normalizedSelectedRemoveModels,
        },
        { skipErrorHandler: true },
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('操作失败'));
        return;
      }

      const addedCount = data?.added_models?.length || 0;
      const removedCount = data?.removed_models?.length || 0;
      const totalIgnoredCount = getManualIgnoredModelCountFromSettings(
        data?.settings,
      );
      const ignoredCount = normalizeModelList(ignoreModels).length;
      showSuccess(
        t(
          '已处理上游模型更新：加入 {{added}} 个，删除 {{removed}} 个，本次忽略 {{ignored}} 个，当前已忽略模型 {{totalIgnored}} 个',
          {
            added: addedCount,
            removed: removedCount,
            ignored: ignoredCount,
            totalIgnored: totalIgnoredCount,
          },
        ),
      );
      closeUpstreamUpdateModal();
      await refresh();
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('操作失败'),
      );
    } finally {
      applyUpstreamUpdatesInFlightRef.current = false;
      setUpstreamApplyLoading(false);
    }
  };

  const applyAllUpstreamUpdates = async () => {
    if (!canApplyUpstreamUpdates) {
      showError(t('无权限处理上游模型更新'));
      return;
    }

    if (applyAllUpstreamUpdatesInFlightRef.current) {
      showInfo(t('正在批量处理，请稍候'));
      return;
    }
    applyAllUpstreamUpdatesInFlightRef.current = true;
    setApplyAllUpstreamUpdatesLoading(true);
    try {
      const res = await API.post(
        '/api/channel/upstream_updates/apply_all',
        {},
        { skipErrorHandler: true },
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('批量处理失败'));
        return;
      }

      const channelCount = data?.processed_channels || 0;
      const addedCount = data?.added_models || 0;
      const keptRemoveCount =
        typeof data?.remaining_remove_models_count === 'number'
          ? data.remaining_remove_models_count
          : countRemainingRemoveModels(data?.results);
      const failedCount = (data?.failed_channel_ids || []).length;
      showSuccess(
        t(
          '已批量加入上游新增模型：渠道 {{channels}} 个，加入 {{added}} 个，保留 {{kept}} 个待人工处理的删除项，失败 {{fails}} 个',
          {
            channels: channelCount,
            added: addedCount,
            kept: keptRemoveCount,
            fails: failedCount,
          },
        ),
      );
      await refresh();
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('批量处理失败'),
      );
    } finally {
      applyAllUpstreamUpdatesInFlightRef.current = false;
      setApplyAllUpstreamUpdatesLoading(false);
    }
  };

  const detectChannelUpstreamUpdates = async (channel) => {
    if (!canDetectUpstreamUpdates) {
      showError(t('无权限检测上游模型更新'));
      return;
    }

    if (detectChannelUpstreamUpdatesInFlightRef.current) {
      showInfo(t('正在检测，请稍候'));
      return;
    }
    if (!channel?.id) {
      return;
    }
    detectChannelUpstreamUpdatesInFlightRef.current = true;
    try {
      const res = await API.post(
        '/api/channel/upstream_updates/detect',
        {
          id: channel.id,
        },
        { skipErrorHandler: true },
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('检测失败'));
        return;
      }

      const addCount = data?.add_models?.length || 0;
      const removeCount = data?.remove_models?.length || 0;
      showSuccess(
        t('检测完成：新增 {{add}} 个，删除 {{remove}} 个', {
          add: addCount,
          remove: removeCount,
        }),
      );
      await refresh();
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('检测失败'),
      );
    } finally {
      detectChannelUpstreamUpdatesInFlightRef.current = false;
    }
  };

  const pollAndReportModelUpdateTask = async (taskInfo, existingTask) => {
    const { controller, generation } = beginModelUpdateTaskRun(
      taskInfo.task_id,
    );
    if (existingTask) {
      showInfo(t('批量检测任务已在运行，等待完成'));
    } else {
      showSuccess(t('批量检测任务已启动'));
    }

    let clearTaskId = false;
    try {
      const task = await waitForModelUpdateTask(taskInfo.task_id, {
        api: API,
        signal: controller.signal,
      });
      if (!isCurrentModelUpdateTaskRun(generation)) return;
      if (!task) {
        showInfo(t('批量检测仍在运行，请稍后刷新查看'));
        return;
      }
      clearTaskId = shouldClearPersistedModelUpdateTaskIdAfterTaskResult(task);
      if (task.status === 'failed') {
        showError(task.error || t('批量检测失败'));
        return;
      }

      const result = task.result || {};
      showSuccess(
        t(
          '批量检测完成：渠道 {{channels}} 个，新增 {{add}} 个，删除 {{remove}} 个，失败 {{fails}} 个',
          {
            channels: result.checked_channels || 0,
            add: result.detected_add_models || 0,
            remove: result.detected_remove_models || 0,
            fails: result.failed_channels || 0,
          },
        ),
      );
      await refresh();
    } catch (error) {
      if (!isCurrentModelUpdateTaskRun(generation)) return;
      clearTaskId =
        shouldClearPersistedModelUpdateTaskIdAfterPollingError(error);
      showError(
        error?.response?.data?.message || error?.message || t('批量检测失败'),
      );
    } finally {
      finishModelUpdateTaskRun(generation, clearTaskId);
    }
  };

  useEffect(() => {
    if (
      !canDetectUpstreamUpdates ||
      didResumePersistedModelUpdateTaskRef.current
    ) {
      return;
    }
    const taskId = getPersistedModelUpdateTaskId();
    if (!taskId) return;
    didResumePersistedModelUpdateTaskRef.current = true;
    pollAndReportModelUpdateTask({ task_id: taskId }, true).then();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canDetectUpstreamUpdates]);

  const detectAllUpstreamUpdates = async () => {
    if (!canDetectUpstreamUpdates) {
      showError(t('无权限检测上游模型更新'));
      return;
    }

    if (detectAllUpstreamUpdatesInFlightRef.current) {
      showInfo(t('正在批量检测，请稍候'));
      return;
    }

    const persistedTaskId = getPersistedModelUpdateTaskId();
    if (persistedTaskId) {
      await pollAndReportModelUpdateTask({ task_id: persistedTaskId }, true);
      return;
    }

    detectAllUpstreamUpdatesInFlightRef.current = true;
    setDetectAllUpstreamUpdatesLoading(true);
    let handedOffToTaskPolling = false;
    try {
      const res = await API.post(
        '/api/channel/upstream_updates/detect_all',
        {},
        { skipErrorHandler: true },
      );
      const { success, message } = res.data || {};
      const taskInfo = getModelUpdateTaskStartInfo(res.data);
      if (!success || !taskInfo) {
        showError(message || t('批量检测失败'));
        return;
      }

      handedOffToTaskPolling = true;
      await pollAndReportModelUpdateTask(taskInfo, false);
    } catch (error) {
      const existingTask = getModelUpdateTaskStartInfo(
        getModelUpdateTaskErrorPayload(error),
      );
      if (existingTask) {
        handedOffToTaskPolling = true;
        await pollAndReportModelUpdateTask(existingTask, true);
        return;
      }
      showError(
        error?.response?.data?.message || error?.message || t('批量检测失败'),
      );
    } finally {
      if (!handedOffToTaskPolling && mountedRef.current) {
        detectAllUpstreamUpdatesInFlightRef.current = false;
        setDetectAllUpstreamUpdatesLoading(false);
      }
    }
  };

  return {
    showUpstreamUpdateModal,
    setShowUpstreamUpdateModal,
    upstreamUpdateChannel,
    upstreamUpdateAddModels,
    upstreamUpdateRemoveModels,
    upstreamUpdatePreferredTab,
    upstreamApplyLoading,
    detectAllUpstreamUpdatesLoading,
    applyAllUpstreamUpdatesLoading,
    canDetectUpstreamUpdates,
    canApplyUpstreamUpdates,
    openUpstreamUpdateModal,
    closeUpstreamUpdateModal,
    applyUpstreamUpdates,
    applyAllUpstreamUpdates,
    detectChannelUpstreamUpdates,
    detectAllUpstreamUpdates,
  };
};
