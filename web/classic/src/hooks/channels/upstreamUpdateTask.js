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
const MODEL_UPDATE_TASK_MAX_POLLS = 900;
const MODEL_UPDATE_TASK_STORAGE_KEY =
  'newapi.classic.channel.upstream_update.task_id';

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
  if (typeof taskId !== 'string' || taskId.length === 0) {
    return null;
  }
  return {
    task_id: taskId,
    status: payload?.data?.status,
    type: payload?.data?.type,
  };
};

export const getModelUpdateTaskErrorPayload = (error) =>
  error?.response?.data || {};

const isRequestCancelled = (error) =>
  error?.name === 'CanceledError' || error?.code === 'ERR_CANCELED';

const getModelUpdateTaskErrorStatus = (error) => error?.response?.status;

export const shouldClearPersistedModelUpdateTaskIdAfterPollingError = (
  error,
) => {
  const status = getModelUpdateTaskErrorStatus(error);
  if (status === 404 || status === 410) return true;
  const payload = getModelUpdateTaskErrorPayload(error);
  const message =
    payload?.message || error?.message || '';
  return /task.*not found|not found|不存在|missing/i.test(String(message));
};

export const shouldClearPersistedModelUpdateTaskIdAfterTaskResult = (
  task,
) => task?.status === 'succeeded' || task?.status === 'failed';

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

export const waitForModelUpdateTask = async (
  taskId,
  {
    api,
    maxPolls = MODEL_UPDATE_TASK_MAX_POLLS,
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
    if (task.status === 'succeeded' || task.status === 'failed') {
      return task;
    }
    if (poll + 1 < maxPolls) {
      await sleep(pollIntervalMs, signal);
    }
  }
  return null;
};
