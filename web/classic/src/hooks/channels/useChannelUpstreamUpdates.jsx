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
  cancelModelUpdateTask as cancelModelUpdateTaskRequest,
  clearPersistedModelUpdateTaskId,
  getCurrentModelUpdateTask,
  getPersistedModelUpdateTaskId,
  getModelUpdateTaskErrorPayload,
  formatModelUpdateTaskFailureMessage,
  formatModelUpdateTaskSuccessMessage,
  getModelUpdateTaskStartInfo,
  isCancelledModelUpdateTask,
  shouldClearPersistedModelUpdateTaskIdAfterTaskResult,
  setPersistedModelUpdateTaskId,
  shouldClearPersistedModelUpdateTaskIdAfterPollingError,
  waitForModelUpdateTask,
} from './upstreamUpdateTask';
import { normalizeModelList } from './upstreamUpdateUtils';

const MODEL_UPDATE_TASK_DISCOVERY_INTERVAL_MS = 5000;

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

const refreshChannelsBestEffort = async (refresh) => {
  try {
    await refresh();
  } catch {
    // Keep the original operation result visible; refresh is best-effort.
  }
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
  const [cancelModelUpdateTaskLoading, setCancelModelUpdateTaskLoading] =
    useState(false);
  const [currentModelUpdateTask, setCurrentModelUpdateTask] = useState(null);
  const [
    currentModelUpdateTaskLookupComplete,
    setCurrentModelUpdateTaskLookupComplete,
  ] = useState(false);

  const applyUpstreamUpdatesInFlightRef = useRef(false);
  const detectChannelUpstreamUpdatesInFlightRef = useRef(false);
  const detectAllUpstreamUpdatesInFlightRef = useRef(false);
  const applyAllUpstreamUpdatesInFlightRef = useRef(false);
  const cancelModelUpdateTaskInFlightRef = useRef(false);
  const mountedRef = useRef(true);
  const modelUpdateTaskGenerationRef = useRef(0);
  const modelUpdateTaskAbortControllerRef = useRef(null);
  const didResumePersistedModelUpdateTaskRef = useRef(false);
  const canDetectUpstreamUpdates =
    channelPermissions.canOperateChannel === true;
  const canApplyUpstreamUpdates = channelPermissions.canWriteChannel === true;
  const canAccessModelUpdateTasks =
    canDetectUpstreamUpdates || canApplyUpstreamUpdates;

  const isCurrentModelUpdateTaskRun = (generation) =>
    mountedRef.current && modelUpdateTaskGenerationRef.current === generation;

  const beginModelUpdateTaskRun = (taskInfo, existingController = null) => {
    if (
      existingController &&
      modelUpdateTaskAbortControllerRef.current !== existingController
    ) {
      modelUpdateTaskAbortControllerRef.current?.abort();
    } else if (!existingController) {
      modelUpdateTaskAbortControllerRef.current?.abort();
    }
    const controller = existingController || new AbortController();
    modelUpdateTaskAbortControllerRef.current = controller;
    modelUpdateTaskGenerationRef.current += 1;
    const generation = modelUpdateTaskGenerationRef.current;
    setPersistedModelUpdateTaskId(taskInfo.task_id);
    const isApplyAllTask = taskInfo.type === 'model_update_apply_all';
    if (isApplyAllTask) {
      applyAllUpstreamUpdatesInFlightRef.current = true;
      setApplyAllUpstreamUpdatesLoading(true);
    } else {
      detectAllUpstreamUpdatesInFlightRef.current = true;
      setDetectAllUpstreamUpdatesLoading(true);
    }
    return { controller, generation, isApplyAllTask };
  };

  const finishModelUpdateTaskRun = (
    generation,
    clearTaskId = false,
    isApplyAllTask = false,
  ) => {
    if (!isCurrentModelUpdateTaskRun(generation)) return;
    if (clearTaskId) {
      clearPersistedModelUpdateTaskId();
      setCurrentModelUpdateTask(null);
    }
    modelUpdateTaskAbortControllerRef.current = null;
    if (isApplyAllTask) {
      applyAllUpstreamUpdatesInFlightRef.current = false;
      setApplyAllUpstreamUpdatesLoading(false);
      detectAllUpstreamUpdatesInFlightRef.current = false;
      setDetectAllUpstreamUpdatesLoading(false);
    } else {
      detectAllUpstreamUpdatesInFlightRef.current = false;
      setDetectAllUpstreamUpdatesLoading(false);
      applyAllUpstreamUpdatesInFlightRef.current = false;
      setApplyAllUpstreamUpdatesLoading(false);
    }
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

  useEffect(() => {
    if (!canAccessModelUpdateTasks) {
      didResumePersistedModelUpdateTaskRef.current = false;
      setCurrentModelUpdateTaskLookupComplete(false);
    }
  }, [canAccessModelUpdateTasks]);

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
        await refreshChannelsBestEffort(refresh);
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
      await refreshChannelsBestEffort(refresh);
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('操作失败'),
      );
      await refreshChannelsBestEffort(refresh);
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
      const taskInfo = getModelUpdateTaskStartInfo(res.data);
      if (!res.data?.success || !taskInfo) {
        showError(res.data?.message || t('批量处理失败'));
        await refreshChannelsBestEffort(refresh);
        return;
      }
      await pollAndReportModelUpdateTask(
        taskInfo,
        false,
        true,
        MODEL_UPDATE_APPLY_ALL_TASK_TYPE,
      );
    } catch (error) {
      const taskInfo = getModelUpdateTaskStartInfo(
        getModelUpdateTaskErrorPayload(error),
      );
      if (taskInfo) {
        await pollAndReportModelUpdateTask(taskInfo, true);
        return;
      }
      showError(
        error?.response?.data?.message || error?.message || t('批量处理失败'),
      );
      await refreshChannelsBestEffort(refresh);
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
        await refreshChannelsBestEffort(refresh);
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
      await refreshChannelsBestEffort(refresh);
    } catch (error) {
      showError(
        error?.response?.data?.message || error?.message || t('检测失败'),
      );
      await refreshChannelsBestEffort(refresh);
    } finally {
      detectChannelUpstreamUpdatesInFlightRef.current = false;
    }
  };

  const pollAndReportModelUpdateTask = async (
    taskInfo,
    existingTask,
    notify = true,
    fallbackType = '',
  ) => {
    if (
      !taskInfo.type?.trim() &&
      modelUpdateTaskAbortControllerRef.current
    ) {
      return;
    }
    const resolvingController = taskInfo.type?.trim()
      ? null
      : new AbortController();
    if (resolvingController) {
      modelUpdateTaskAbortControllerRef.current = resolvingController;
    }
    let taskRun;
    let clearTaskId = false;
    try {
      let resolvedTaskInfo = taskInfo;
      if (!resolvedTaskInfo.type?.trim()) {
        const response = await API.get(
          `/api/channel/upstream_updates/task/${encodeURIComponent(
            resolvedTaskInfo.task_id,
          )}`,
          {
            skipErrorHandler: true,
            disableDuplicate: true,
            signal: resolvingController?.signal,
          },
        );
        resolvedTaskInfo = getModelUpdateTaskStartInfo(response.data);
        if (
          resolvedTaskInfo &&
          !resolvedTaskInfo.type?.trim() &&
          fallbackType.trim()
        ) {
          resolvedTaskInfo = {
            ...resolvedTaskInfo,
            type: fallbackType.trim(),
          };
        }
        if (!resolvedTaskInfo) {
          throw new Error('上游模型更新任务类型缺失');
        }
      }

      if (resolvingController?.signal.aborted) return;
      taskRun = beginModelUpdateTaskRun(
        resolvedTaskInfo,
        resolvingController,
      );
      const { controller, generation, isApplyAllTask } = taskRun;
      setCurrentModelUpdateTask(resolvedTaskInfo);
      if (!notify) {
        // Mount recovery keeps the UI synchronized without a duplicate toast.
      } else if (existingTask) {
        showInfo(t('批量检测任务已在运行，等待完成'));
      } else {
        showSuccess(t('批量检测任务已启动'));
      }

      const polledTask = await waitForModelUpdateTask(resolvedTaskInfo.task_id, {
        api: API,
        signal: controller.signal,
      });
      const task =
        polledTask &&
        !polledTask.type?.trim() &&
        resolvedTaskInfo.type?.trim()
          ? { ...polledTask, type: resolvedTaskInfo.type }
          : polledTask;
      if (!isCurrentModelUpdateTaskRun(generation)) return;
      if (!task) {
        showInfo(t('批量检测仍在运行，请稍后刷新查看'));
        return;
      }
      clearTaskId = shouldClearPersistedModelUpdateTaskIdAfterTaskResult(task);
      if (
        task.status === 'failed' ||
        task.status === 'cancelled' ||
        task.status === 'canceled'
      ) {
        showError(
          formatModelUpdateTaskFailureMessage(task, t('批量检测失败'), t),
        );
        await refreshChannelsBestEffort(refresh);
        return;
      }

      showSuccess(formatModelUpdateTaskSuccessMessage(task, t));
      await refreshChannelsBestEffort(refresh);
    } catch (error) {
      if (!mountedRef.current || resolvingController?.signal.aborted) {
        return;
      }
      if (taskRun && !isCurrentModelUpdateTaskRun(taskRun.generation)) {
        return;
      }
      clearTaskId =
        shouldClearPersistedModelUpdateTaskIdAfterPollingError(error);
      showError(
        error?.response?.data?.message || error?.message || t('批量检测失败'),
      );
      await refreshChannelsBestEffort(refresh);
    } finally {
      if (taskRun) {
        finishModelUpdateTaskRun(
          taskRun.generation,
          clearTaskId,
          taskRun.isApplyAllTask,
        );
      } else if (
        resolvingController &&
        modelUpdateTaskAbortControllerRef.current === resolvingController
      ) {
        if (
          clearTaskId &&
          getPersistedModelUpdateTaskId() === taskInfo.task_id
        ) {
          clearPersistedModelUpdateTaskId();
          setCurrentModelUpdateTask((currentTask) =>
            currentTask?.task_id === taskInfo.task_id ? null : currentTask,
          );
        }
        modelUpdateTaskAbortControllerRef.current = null;
      }
    }
  };

  useEffect(() => {
    if (
      !canAccessModelUpdateTasks ||
      !currentModelUpdateTaskLookupComplete ||
      didResumePersistedModelUpdateTaskRef.current
    ) {
      return;
    }
    const taskId = getPersistedModelUpdateTaskId();
    if (!taskId || modelUpdateTaskAbortControllerRef.current) return;
    didResumePersistedModelUpdateTaskRef.current = true;
    pollAndReportModelUpdateTask({ task_id: taskId }, true).then();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [canAccessModelUpdateTasks, currentModelUpdateTaskLookupComplete]);

  useEffect(() => {
    if (!canAccessModelUpdateTasks) return;

    let cancelled = false;
    let lookupInFlight = false;
    let lookupController = null;

    const lookupCurrentTask = async () => {
      if (cancelled || lookupInFlight) return;
      lookupInFlight = true;
      lookupController = new AbortController();
      try {
        const currentTask = await getCurrentModelUpdateTask(
          API,
          lookupController.signal,
        );
        if (cancelled || !mountedRef.current) return;
        const persistedTaskId = getPersistedModelUpdateTaskId();
        if (currentTask) {
          // The current-task lookup owns recovery when it finds a live task.
          // Prevent the persisted-id effect from starting a second poll with
          // an id-only task shape and aborting this richer recovery run.
          didResumePersistedModelUpdateTaskRef.current = true;
          if (
            shouldClearPersistedModelUpdateTaskIdAfterTaskResult(currentTask)
          ) {
            clearPersistedModelUpdateTaskId();
            setCurrentModelUpdateTask(null);
          } else {
            setCurrentModelUpdateTask(currentTask);
            if (persistedTaskId && persistedTaskId !== currentTask.task_id) {
              clearPersistedModelUpdateTaskId();
            }
            if (
              !detectAllUpstreamUpdatesInFlightRef.current &&
              !modelUpdateTaskAbortControllerRef.current
            ) {
              void pollAndReportModelUpdateTask(
                {
                  task_id: currentTask.task_id,
                  status: currentTask.status,
                  type: currentTask.type,
                },
                true,
                false,
              );
            }
          }
        } else if (persistedTaskId) {
          // Keep the persisted task ID as the recovery anchor when the
          // current-task lookup temporarily returns no row. A later
          // discovery pass can resume task polling after a transient API or
          // database error instead of leaving the UI stuck indefinitely.
          if (
            !detectAllUpstreamUpdatesInFlightRef.current &&
            !modelUpdateTaskAbortControllerRef.current
          ) {
            void pollAndReportModelUpdateTask(
              { task_id: persistedTaskId },
              true,
              false,
            );
          }
        } else {
          setCurrentModelUpdateTask(null);
        }
        if (!cancelled && mountedRef.current) {
          setCurrentModelUpdateTaskLookupComplete(true);
        }
      } catch (error) {
        if (
          !cancelled &&
          mountedRef.current &&
          !(
            error?.name === 'CanceledError' || error?.code === 'ERR_CANCELED'
          ) &&
          !getPersistedModelUpdateTaskId()
        ) {
          setCurrentModelUpdateTask(null);
        }
        if (!cancelled && mountedRef.current) {
          setCurrentModelUpdateTaskLookupComplete(true);
        }
      } finally {
        lookupInFlight = false;
        lookupController = null;
      }
    };

    void lookupCurrentTask();
    const discoveryTimer = setInterval(() => {
      void lookupCurrentTask();
    }, MODEL_UPDATE_TASK_DISCOVERY_INTERVAL_MS);

    return () => {
      cancelled = true;
      lookupController?.abort();
      clearInterval(discoveryTimer);
    };
  }, [canAccessModelUpdateTasks]);

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

    detectAllUpstreamUpdatesInFlightRef.current = true;
    setDetectAllUpstreamUpdatesLoading(true);
    let handedOffToTaskPolling = false;
    try {
      const currentTask = await getCurrentModelUpdateTask(API);
      if (currentTask) {
        setCurrentModelUpdateTask(currentTask);
        if (persistedTaskId && persistedTaskId !== currentTask.task_id) {
          clearPersistedModelUpdateTaskId();
        }
        if (shouldClearPersistedModelUpdateTaskIdAfterTaskResult(currentTask)) {
          clearPersistedModelUpdateTaskId();
          setCurrentModelUpdateTask(null);
          showInfo(t('上游模型更新任务已结束，正在刷新渠道列表'));
          await refreshChannelsBestEffort(refresh);
          return;
        }
        handedOffToTaskPolling = true;
        await pollAndReportModelUpdateTask(
          {
            task_id: currentTask.task_id,
            status: currentTask.status,
            type: currentTask.type,
          },
          true,
        );
        return;
      }
      if (persistedTaskId) {
        handedOffToTaskPolling = true;
        await pollAndReportModelUpdateTask({ task_id: persistedTaskId }, true);
        return;
      }
      setCurrentModelUpdateTask(null);

      const res = await API.post(
        '/api/channel/upstream_updates/detect_all',
        {},
        { skipErrorHandler: true },
      );
      const { success, message } = res.data || {};
      const taskInfo = getModelUpdateTaskStartInfo(res.data);
      if (!success || !taskInfo) {
        showError(message || t('批量检测失败'));
        await refreshChannelsBestEffort(refresh);
        return;
      }
      if (shouldClearPersistedModelUpdateTaskIdAfterTaskResult(taskInfo)) {
        clearPersistedModelUpdateTaskId();
        setCurrentModelUpdateTask(null);
        showInfo(t('上游模型更新任务已结束，正在刷新渠道列表'));
        await refreshChannelsBestEffort(refresh);
        return;
      }

      handedOffToTaskPolling = true;
      await pollAndReportModelUpdateTask(
        taskInfo,
        false,
        true,
        'model_update_manual',
      );
    } catch (error) {
      const existingTask = getModelUpdateTaskStartInfo(
        getModelUpdateTaskErrorPayload(error),
      );
      if (existingTask) {
        setCurrentModelUpdateTask(existingTask);
        if (persistedTaskId && persistedTaskId !== existingTask.task_id) {
          clearPersistedModelUpdateTaskId();
        }
        if (
          shouldClearPersistedModelUpdateTaskIdAfterTaskResult(existingTask)
        ) {
          clearPersistedModelUpdateTaskId();
          setCurrentModelUpdateTask(null);
          showInfo(t('上游模型更新任务已结束，正在刷新渠道列表'));
          await refreshChannelsBestEffort(refresh);
          return;
        }
        handedOffToTaskPolling = true;
        await pollAndReportModelUpdateTask(existingTask, true);
        return;
      }
      showError(
        error?.response?.data?.message || error?.message || t('批量检测失败'),
      );
      await refreshChannelsBestEffort(refresh);
    } finally {
      if (!handedOffToTaskPolling && mountedRef.current) {
        detectAllUpstreamUpdatesInFlightRef.current = false;
        setDetectAllUpstreamUpdatesLoading(false);
      }
    }
  };

  const cancelModelUpdateTask = async () => {
    if (!canAccessModelUpdateTasks) {
      showError(t('无权限处理上游模型更新'));
      return;
    }
    if (cancelModelUpdateTaskInFlightRef.current) return;

    const taskId =
      currentModelUpdateTask?.task_id?.trim() ||
      getPersistedModelUpdateTaskId();
    if (!taskId) {
      showInfo(t('没有正在运行的上游模型更新任务'));
      return;
    }

    cancelModelUpdateTaskInFlightRef.current = true;
    setCancelModelUpdateTaskLoading(true);
    try {
      const task = await cancelModelUpdateTaskRequest(API, taskId);
      modelUpdateTaskGenerationRef.current += 1;
      modelUpdateTaskAbortControllerRef.current?.abort();
      modelUpdateTaskAbortControllerRef.current = null;
      clearPersistedModelUpdateTaskId();
      setCurrentModelUpdateTask(null);
      detectAllUpstreamUpdatesInFlightRef.current = false;
      setDetectAllUpstreamUpdatesLoading(false);
      applyAllUpstreamUpdatesInFlightRef.current = false;
      setApplyAllUpstreamUpdatesLoading(false);
      if (isCancelledModelUpdateTask(task)) {
        showSuccess(t('上游模型更新任务已取消'));
      } else {
        showInfo(t('没有正在运行的上游模型更新任务'));
      }
      await refreshChannelsBestEffort(refresh);
    } catch (error) {
      if (shouldClearPersistedModelUpdateTaskIdAfterPollingError(error)) {
        const persistedTaskId = getPersistedModelUpdateTaskId();
        const currentTaskId = currentModelUpdateTask?.task_id?.trim() || '';
        const stillTrackingCancelledTask =
          persistedTaskId === taskId ||
          (!persistedTaskId && currentTaskId === taskId);
        if (stillTrackingCancelledTask) {
          modelUpdateTaskGenerationRef.current += 1;
          modelUpdateTaskAbortControllerRef.current?.abort();
          modelUpdateTaskAbortControllerRef.current = null;
          clearPersistedModelUpdateTaskId();
          setCurrentModelUpdateTask(null);
          detectAllUpstreamUpdatesInFlightRef.current = false;
          setDetectAllUpstreamUpdatesLoading(false);
          applyAllUpstreamUpdatesInFlightRef.current = false;
          setApplyAllUpstreamUpdatesLoading(false);
        }
      }
      showError(
        error?.response?.data?.message ||
          error?.message ||
          t('取消上游模型更新任务失败'),
      );
      await refreshChannelsBestEffort(refresh);
    } finally {
      cancelModelUpdateTaskInFlightRef.current = false;
      setCancelModelUpdateTaskLoading(false);
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
    cancelModelUpdateTaskLoading,
    currentModelUpdateTask,
    applyAllUpstreamUpdatesLoading,
    canDetectUpstreamUpdates,
    canApplyUpstreamUpdates,
    openUpstreamUpdateModal,
    closeUpstreamUpdateModal,
    applyUpstreamUpdates,
    applyAllUpstreamUpdates,
    detectChannelUpstreamUpdates,
    detectAllUpstreamUpdates,
    cancelModelUpdateTask,
  };
};
