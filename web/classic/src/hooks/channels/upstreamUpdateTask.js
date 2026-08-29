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
const MODEL_UPDATE_TASK_POLL_INTERVAL_MS = 2000;
const MODEL_UPDATE_TASK_SLOW_POLL_AFTER_POLLS = 900;
const MODEL_UPDATE_TASK_SLOW_POLL_INTERVAL_MS = 10000;
const MODEL_UPDATE_TASK_HIDDEN_POLL_INTERVAL_MS = 30000;
const MODEL_UPDATE_TASK_STORAGE_KEY =
  'newapi.classic.channel.upstream_update.task_id';
const MODEL_UPDATE_APPLY_ALL_TASK_TYPE = 'model_update_apply_all';

const getModelUpdateTaskStorage = () => {
  try {
    if (typeof window === 'undefined') return null;
    return window.sessionStorage;
  } catch {
    return null;
  }
};

export const getPersistedModelUpdateTaskId = (
  storage = getModelUpdateTaskStorage(),
) => {
  try {
    return storage?.getItem(MODEL_UPDATE_TASK_STORAGE_KEY)?.trim() || '';
  } catch {
    return '';
  }
};

export const setPersistedModelUpdateTaskId = (
  taskId,
  storage = getModelUpdateTaskStorage(),
) => {
  const normalizedTaskId = String(taskId || '').trim();
  if (!normalizedTaskId) {
    clearPersistedModelUpdateTaskId(storage);
    return;
  }
  try {
    storage?.setItem(MODEL_UPDATE_TASK_STORAGE_KEY, normalizedTaskId);
  } catch {
    // Ignore storage failures; polling still continues in-memory.
  }
};

export const clearPersistedModelUpdateTaskId = (
  storage = getModelUpdateTaskStorage(),
) => {
  try {
    storage?.removeItem(MODEL_UPDATE_TASK_STORAGE_KEY);
  } catch {
    // Ignore storage failures; the task already reached a terminal state.
  }
};

export const getModelUpdateTaskStartInfo = (payload) => {
  const taskId = payload?.data?.task_id;
  if (typeof taskId !== 'string' || taskId.trim().length === 0) {
    return null;
  }
  if (!isKnownModelUpdateTaskStatus(payload?.data?.status)) {
    return null;
  }
  return {
    task_id: taskId.trim(),
    status: payload?.data?.status,
    type: payload?.data?.type,
  };
};

const isTerminalModelUpdateTaskStatus = (status) =>
  status === 'succeeded' ||
  status === 'failed' ||
  status === 'cancelled' ||
  status === 'canceled';

const isKnownModelUpdateTaskStatus = (status) =>
  status === 'pending' ||
  status === 'running' ||
  status === 'succeeded' ||
  status === 'failed' ||
  status === 'cancelled' ||
  status === 'canceled';

export const shouldPollModelUpdateTaskStartInfo = (taskInfo) =>
  Boolean(taskInfo) && !isTerminalModelUpdateTaskStatus(taskInfo.status);

export const getModelUpdateTaskErrorPayload = (error) =>
  error?.response?.data || {};

export const getCurrentModelUpdateTask = async (api, signal) => {
  if (!api || typeof api.get !== 'function') {
    throw new Error('上游模型批量检测任务查询客户端不可用');
  }
  const res = await api.get('/api/channel/upstream_updates/current', {
    skipErrorHandler: true,
    disableDuplicate: true,
    ...(signal ? { signal } : {}),
  });
  const payload = res.data || {};
  if (!payload.success) {
    throw new Error(payload.message || '上游模型批量检测任务查询失败');
  }
  const task = payload.data || null;
  if (!task) return null;
  if (!isKnownModelUpdateTaskStatus(task.status)) {
    throw new Error(`未知的上游模型批量检测任务状态：${task.status || ''}`);
  }
  return task;
};

export const cancelModelUpdateTask = async (api, taskId = '') => {
  if (!api || typeof api.post !== 'function') {
    throw new Error('上游模型批量检测任务取消客户端不可用');
  }
  const normalizedTaskId = String(taskId || '').trim();
  if (!normalizedTaskId) {
    throw new Error('上游模型批量检测任务 ID 不能为空');
  }
  const body = { task_id: normalizedTaskId };
  const res = await api.post('/api/channel/upstream_updates/cancel', body, {
    skipErrorHandler: true,
  });
  const payload = res.data || {};
  if (!payload.success) {
    throw new Error(payload.message || '上游模型批量检测任务取消失败');
  }
  return payload.data || null;
};

const isRequestCancelled = (error) =>
  error?.name === 'CanceledError' || error?.code === 'ERR_CANCELED';

const getModelUpdateTaskErrorStatus = (error) => error?.response?.status;

export const shouldClearPersistedModelUpdateTaskIdAfterPollingError = (
  error,
) => {
  const status = getModelUpdateTaskErrorStatus(error);
  if (status === 404 || status === 410) return true;
  const payload = getModelUpdateTaskErrorPayload(error);
  const message = payload?.message || error?.message || '';
  return /task.*not found|not found|不存在|missing/i.test(String(message));
};

export const shouldClearPersistedModelUpdateTaskIdAfterTaskResult = (task) =>
  task?.status === 'succeeded' ||
  task?.status === 'failed' ||
  task?.status === 'cancelled' ||
  task?.status === 'canceled';

export const isCancelledModelUpdateTask = (task) =>
  task?.status === 'cancelled' || task?.status === 'canceled';

const getModelUpdateTaskResultNumber = (result, key) => {
  const value = result?.[key];
  return Number.isFinite(value) && value > 0 ? value : 0;
};

const getModelUpdateTaskCacheRefreshError = (result) =>
  typeof result?.runtime_cache_refresh_error === 'string'
    ? result.runtime_cache_refresh_error.trim()
    : '';

const hasModelUpdateTaskPartialResult = (task) => {
  const result = task?.result;
  if (!result) return false;
  return (
    getModelUpdateTaskResultNumber(result, 'checked_channels') > 0 ||
    getModelUpdateTaskResultNumber(result, 'changed_channels') > 0 ||
    getModelUpdateTaskResultNumber(result, 'detected_add_models') > 0 ||
    getModelUpdateTaskResultNumber(result, 'detected_remove_models') > 0 ||
    getModelUpdateTaskResultNumber(result, 'failed_channels') > 0
  );
};

export const formatModelUpdateTaskFailureMessage = (
  task,
  fallbackMessage,
  t = (key) => key,
) => {
  const errorMessage = typeof task?.error === 'string' ? task.error.trim() : '';
  const cacheRefreshError = getModelUpdateTaskCacheRefreshError(task?.result);
  if (task?.type === MODEL_UPDATE_APPLY_ALL_TASK_TYPE && task?.result) {
    const result = task.result;
    const processed = getModelUpdateTaskResultNumber(
      result,
      'processed_channels',
    );
    const added = getModelUpdateTaskResultNumber(result, 'added_models');
    const kept = getModelUpdateTaskResultNumber(
      result,
      'remaining_remove_models_count',
    );
    const failed = Array.isArray(result.failed_channel_ids)
      ? result.failed_channel_ids.length
      : 0;
    if (processed > 0 || added > 0 || kept > 0 || failed > 0) {
      const partialMessage = t(
        '批量加入上游新增模型部分完成：渠道 {{channels}} 个，加入 {{added}} 个，保留 {{kept}} 个待人工处理的删除项，失败 {{fails}} 个。',
        {
          channels: processed,
          added,
          kept,
          fails: failed,
        },
      );
      const suffix = [errorMessage, cacheRefreshError].filter(Boolean).join(' ');
      return suffix ? `${partialMessage} ${suffix}` : partialMessage;
    }
  }
  if (!hasModelUpdateTaskPartialResult(task)) {
    return [errorMessage, cacheRefreshError].filter(Boolean).join(' ') || fallbackMessage;
  }
  const result = task?.result;
  const partialMessage = t(
    '批量检测部分完成：检测渠道 {{channels}} 个，变更 {{changed}} 个，新增 {{add}} 个，删除 {{remove}} 个，失败 {{fails}} 个。',
    {
      channels: getModelUpdateTaskResultNumber(result, 'checked_channels'),
      changed: getModelUpdateTaskResultNumber(result, 'changed_channels'),
      add: getModelUpdateTaskResultNumber(result, 'detected_add_models'),
      remove: getModelUpdateTaskResultNumber(result, 'detected_remove_models'),
      fails: getModelUpdateTaskResultNumber(result, 'failed_channels'),
    },
  );
  const suffix = [errorMessage, cacheRefreshError].filter(Boolean).join(' ');
  return suffix ? `${partialMessage} ${suffix}` : partialMessage;
};

export const formatModelUpdateTaskSuccessMessage = (task, t = (key) => key) => {
  const result = task?.result || {};
  if (task?.type === MODEL_UPDATE_APPLY_ALL_TASK_TYPE) {
    return t(
      '已批量加入上游新增模型：渠道 {{channels}} 个，加入 {{added}} 个，保留 {{kept}} 个待人工处理的删除项，失败 {{fails}} 个',
      {
        channels: getModelUpdateTaskResultNumber(result, 'processed_channels'),
        added: getModelUpdateTaskResultNumber(result, 'added_models'),
        kept: getModelUpdateTaskResultNumber(
          result,
          'remaining_remove_models_count',
        ),
        fails: Array.isArray(result.failed_channel_ids)
          ? result.failed_channel_ids.length
          : 0,
      },
    );
  }
  if (task?.type === 'model_update') {
    return t(
      '后台上游模型同步完成：检测渠道 {{channels}} 个，新增 {{add}} 个，删除 {{remove}} 个，自动加入 {{autoAdded}} 个，失败 {{fails}} 个',
      {
        channels: getModelUpdateTaskResultNumber(result, 'checked_channels'),
        add: getModelUpdateTaskResultNumber(result, 'detected_add_models'),
        remove: getModelUpdateTaskResultNumber(
          result,
          'detected_remove_models',
        ),
        autoAdded: getModelUpdateTaskResultNumber(result, 'auto_added_models'),
        fails: getModelUpdateTaskResultNumber(result, 'failed_channels'),
      },
    );
  }
  return t(
    '批量检测完成：渠道 {{channels}} 个，新增 {{add}} 个，删除 {{remove}} 个，失败 {{fails}} 个',
    {
      channels: getModelUpdateTaskResultNumber(result, 'checked_channels'),
      add: getModelUpdateTaskResultNumber(result, 'detected_add_models'),
      remove: getModelUpdateTaskResultNumber(result, 'detected_remove_models'),
      fails: getModelUpdateTaskResultNumber(result, 'failed_channels'),
    },
  );
};

const defaultSleep = (milliseconds, signal) =>
  new Promise((resolve) => {
    if (signal?.aborted) {
      resolve();
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      resolve();
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener?.('abort', onAbort);
      resolve();
    }, milliseconds);
    signal?.addEventListener?.('abort', onAbort, { once: true });
  });

const isModelUpdateTaskDocumentHidden = () => {
  try {
    return (
      typeof document !== 'undefined' && document.visibilityState === 'hidden'
    );
  } catch {
    return false;
  }
};

export const getModelUpdateTaskPollIntervalMs = (
  pollIndex,
  basePollIntervalMs,
) => {
  const normalizedBasePollIntervalMs =
    Number.isFinite(basePollIntervalMs) && basePollIntervalMs > 0
      ? basePollIntervalMs
      : 0;
  let intervalMs = normalizedBasePollIntervalMs;
  if (pollIndex >= MODEL_UPDATE_TASK_SLOW_POLL_AFTER_POLLS) {
    intervalMs = Math.max(intervalMs, MODEL_UPDATE_TASK_SLOW_POLL_INTERVAL_MS);
  }
  if (isModelUpdateTaskDocumentHidden()) {
    intervalMs = Math.max(
      intervalMs,
      MODEL_UPDATE_TASK_HIDDEN_POLL_INTERVAL_MS,
    );
  }
  return intervalMs;
};

export const waitForModelUpdateTask = async (
  taskId,
  {
    api,
    // The server processes enabled channels sequentially. Keep polling until
    // the task reaches a terminal state unless the caller explicitly supplies
    // a finite cap.
    maxPolls = Number.POSITIVE_INFINITY,
    pollIntervalMs = MODEL_UPDATE_TASK_POLL_INTERVAL_MS,
    sleep = defaultSleep,
    signal,
  } = {},
) => {
  if (!api || typeof api.get !== 'function') {
    throw new Error('上游模型批量检测任务查询客户端不可用');
  }

  for (let poll = 0; poll < maxPolls; poll += 1) {
    if (signal?.aborted) return null;

    let res;
    try {
      res = await api.get(
        `/api/channel/upstream_updates/task/${encodeURIComponent(taskId)}`,
        {
          skipErrorHandler: true,
          disableDuplicate: true,
          ...(signal ? { signal } : {}),
        },
      );
    } catch (error) {
      if (signal?.aborted || isRequestCancelled(error)) return null;
      throw error;
    }
    if (signal?.aborted) return null;
    const payload = res.data || {};
    if (!payload.success || !payload.data) {
      throw new Error(payload.message || '上游模型批量检测任务查询失败');
    }

    const task = payload.data;
    if (!isKnownModelUpdateTaskStatus(task.status)) {
      throw new Error(`未知的上游模型批量检测任务状态：${task.status || ''}`);
    }
    if (isTerminalModelUpdateTaskStatus(task.status)) {
      return task;
    }
    if (poll + 1 < maxPolls) {
      await sleep(
        getModelUpdateTaskPollIntervalMs(poll, pollIntervalMs),
        signal,
      );
    }
  }
  return null;
};
