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

const defaultSleep = (milliseconds) =>
  new Promise((resolve) => setTimeout(resolve, milliseconds));

export const waitForModelUpdateTask = async (
  taskId,
  {
    api,
    maxPolls = MODEL_UPDATE_TASK_MAX_POLLS,
    pollIntervalMs = MODEL_UPDATE_TASK_POLL_INTERVAL_MS,
    sleep = defaultSleep,
  } = {},
) => {
  if (!api || typeof api.get !== 'function') {
    throw new Error('上游模型批量检测任务查询客户端不可用');
  }

  for (let poll = 0; poll < maxPolls; poll += 1) {
    const res = await api.get(
      `/api/channel/upstream_updates/task/${encodeURIComponent(taskId)}`,
      {
        skipErrorHandler: true,
        disableDuplicate: true,
      },
    );
    const payload = res.data || {};
    if (!payload.success || !payload.data) {
      throw new Error(payload.message || '上游模型批量检测任务查询失败');
    }

    const task = payload.data;
    if (task.status === 'succeeded' || task.status === 'failed') {
      return task;
    }
    if (poll + 1 < maxPolls) {
      await sleep(pollIntervalMs);
    }
  }
  return null;
};
