import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  getModelUpdateTaskErrorPayload,
  getModelUpdateTaskStartInfo,
  waitForModelUpdateTask,
} from './upstreamUpdateTask.js';

test('classic upstream batch detection extracts task info from success and conflict payloads', () => {
  assert.deepEqual(
    getModelUpdateTaskStartInfo({
      data: {
        task_id: 'systask_123',
        status: 'pending',
        type: 'model_update_manual',
      },
    }),
    {
      task_id: 'systask_123',
      status: 'pending',
      type: 'model_update_manual',
    },
  );
  assert.equal(getModelUpdateTaskStartInfo({ data: { task_id: '' } }), null);
  assert.deepEqual(
    getModelUpdateTaskErrorPayload({
      response: { data: { data: { task_id: 'systask_conflict' } } },
    }),
    { data: { task_id: 'systask_conflict' } },
  );
});

test('classic upstream batch detection polls the task endpoint until terminal status', async () => {
  const calls = [];
  const statuses = ['running', 'succeeded'];
  const api = {
    get: async (url, config) => {
      calls.push({ url, config });
      return {
        data: {
          success: true,
          data: {
            status: statuses.shift(),
            result: { checked_channels: 2 },
          },
        },
      };
    },
  };
  const sleeps = [];

  const task = await waitForModelUpdateTask('task/id 1', {
    api,
    maxPolls: 3,
    pollIntervalMs: 5,
    sleep: async (milliseconds) => {
      sleeps.push(milliseconds);
    },
  });

  assert.equal(task.status, 'succeeded');
  assert.deepEqual(task.result, { checked_channels: 2 });
  assert.deepEqual(sleeps, [5]);
  assert.deepEqual(
    calls.map((call) => call.url),
    [
      '/api/channel/upstream_updates/task/task%2Fid%201',
      '/api/channel/upstream_updates/task/task%2Fid%201',
    ],
  );
  assert.deepEqual(calls[0].config, {
    skipErrorHandler: true,
    disableDuplicate: true,
  });
});

test('classic upstream batch detection surfaces polling errors and timeouts', async () => {
  await assert.rejects(
    waitForModelUpdateTask('systask_failed_query', {
      api: {
        get: async () => ({
          data: { success: false, message: 'query failed' },
        }),
      },
      maxPolls: 1,
      sleep: async () => {},
    }),
    /query failed/,
  );

  const task = await waitForModelUpdateTask('systask_running', {
    api: {
      get: async () => ({
        data: { success: true, data: { status: 'running' } },
      }),
    },
    maxPolls: 2,
    pollIntervalMs: 1,
    sleep: async () => {},
  });
  assert.equal(task, null);
});
