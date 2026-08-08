/*
Copyright (C) 2023-2026 QuantumNous

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
import { useRef, useState, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import type {
  SystemTask,
  SystemTaskResponse,
  SystemTaskStatus,
} from "@/features/system-settings/types";
import { api, type ApiRequestConfig } from "@/lib/api";

import { normalizeModelList } from "../lib/upstream-update-utils";

const upstreamUpdateRequestConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
} satisfies ApiRequestConfig;

const modelUpdateTaskPollIntervalMs = 2000;
const modelUpdateTaskMaxPolls = 900;

type ModelUpdateTaskPayload = {
  manual?: boolean;
};

type ModelUpdateTaskState = {
  progress?: number;
};

type ModelUpdateTaskResult = {
  checked_channels?: number;
  changed_channels?: number;
  detected_add_models?: number;
  detected_remove_models?: number;
  failed_channels?: number;
  auto_added_models?: number;
};

type ModelUpdateTask = SystemTask<
  ModelUpdateTaskPayload,
  ModelUpdateTaskState,
  ModelUpdateTaskResult
>;

type ModelUpdateTaskStartInfo = {
  task_id: string;
  status?: SystemTaskStatus;
  type?: string;
};

function countRemainingRemoveModels(results: unknown): number {
  if (!Array.isArray(results)) return 0;
  return results.reduce((total, item) => {
    if (!isRecord(item)) return total;
    return (
      total +
      normalizeModelList(
        (item.remaining_remove_models as unknown[] | undefined) || [],
      ).length
    );
  }, 0);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object";
}

function asSystemTaskStatus(value: unknown): SystemTaskStatus | undefined {
  if (
    value === "pending" ||
    value === "running" ||
    value === "succeeded" ||
    value === "failed"
  ) {
    return value;
  }
  return undefined;
}

function getResponseMessage(payload: unknown): string | undefined {
  if (!isRecord(payload) || typeof payload.message !== "string") return;
  return payload.message;
}

function getErrorPayload(error: unknown): unknown {
  if (!isRecord(error)) return undefined;
  const response = error.response;
  if (!isRecord(response)) return undefined;
  return response.data;
}

function getErrorMessage(error: unknown): string | undefined {
  const payloadMessage = getResponseMessage(getErrorPayload(error));
  if (payloadMessage) return payloadMessage;
  if (isRecord(error) && typeof error.message === "string") {
    return error.message;
  }
  return undefined;
}

function getModelUpdateTaskStartInfo(
  payload: unknown,
): ModelUpdateTaskStartInfo | null {
  if (!isRecord(payload) || !isRecord(payload.data)) return null;
  const taskId = payload.data.task_id;
  if (typeof taskId !== "string" || taskId.length === 0) return null;
  return {
    task_id: taskId,
    status: asSystemTaskStatus(payload.data.status),
    type: typeof payload.data.type === "string" ? payload.data.type : undefined,
  };
}

function isSuccessPayload(payload: unknown): boolean {
  return isRecord(payload) && payload.success === true;
}

function isTerminalTaskStatus(status: SystemTaskStatus): boolean {
  return status === "succeeded" || status === "failed";
}

function sleep(ms: number) {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

async function getChannelModelUpdateTask(taskId: string) {
  const res = await api.get<SystemTaskResponse<ModelUpdateTask>>(
    `/api/channel/upstream_updates/task/${encodeURIComponent(taskId)}`,
    {
      ...upstreamUpdateRequestConfig,
      disableDuplicate: true,
    },
  );
  return res.data;
}

async function waitForModelUpdateTask(taskId: string) {
  for (let i = 0; i < modelUpdateTaskMaxPolls; i++) {
    const res = await getChannelModelUpdateTask(taskId);
    if (!res.success || !res.data) {
      throw new Error(res.message || "");
    }
    if (isTerminalTaskStatus(res.data.status)) return res.data;
    await sleep(modelUpdateTaskPollIntervalMs);
  }
  return null;
}

function getManualIgnoredModelCount(settings: unknown): number {
  let parsed: Record<string, unknown> | null = null;
  if (settings && typeof settings === "object") {
    parsed = settings as Record<string, unknown>;
  } else if (typeof settings === "string") {
    try {
      parsed = JSON.parse(settings);
    } catch {
      parsed = null;
    }
  }
  if (!parsed) return 0;
  return normalizeModelList(
    (parsed.upstream_model_update_ignored_models as unknown[]) || [],
  ).length;
}

export function useChannelUpstreamUpdates(refresh: () => Promise<void>) {
  const { t } = useTranslation();

  const [showModal, setShowModal] = useState(false);
  const [channel, setChannel] = useState<{
    id: number;
    [key: string]: unknown;
  } | null>(null);
  const [addModels, setAddModels] = useState<string[]>([]);
  const [removeModels, setRemoveModels] = useState<string[]>([]);
  const [preferredTab, setPreferredTab] = useState<"add" | "remove">("add");
  const [applyLoading, setApplyLoading] = useState(false);
  const [detectChannelLoadingId, setDetectChannelLoadingId] = useState<
    number | null
  >(null);
  const [detectAllLoading, setDetectAllLoading] = useState(false);
  const [applyAllLoading, setApplyAllLoading] = useState(false);

  const applyRef = useRef(false);
  const detectRef = useRef(false);
  const detectAllRef = useRef(false);
  const applyAllRef = useRef(false);

  const openModal = useCallback(
    (
      record: { id: number; [key: string]: unknown } | null,
      pendingAdd: string[] = [],
      pendingRemove: string[] = [],
      tab: "add" | "remove" = "add",
    ) => {
      const normAdd = normalizeModelList(pendingAdd);
      const normRemove = normalizeModelList(pendingRemove);
      if (!record?.id || (normAdd.length === 0 && normRemove.length === 0)) {
        toast.info(t("No processable upstream model updates for this channel"));
        return;
      }
      setChannel(record);
      setAddModels(normAdd);
      setRemoveModels(normRemove);
      setPreferredTab(tab);
      setShowModal(true);
    },
    [t],
  );

  const closeModal = useCallback(() => {
    setShowModal(false);
    setChannel(null);
    setAddModels([]);
    setRemoveModels([]);
    setPreferredTab("add");
  }, []);

  const applyUpdates = useCallback(
    async ({
      addModels: selectedAdd = [],
      removeModels: selectedRemove = [],
    }: {
      addModels?: string[];
      removeModels?: string[];
    } = {}) => {
      if (applyRef.current) return;
      if (!channel?.id) {
        closeModal();
        return;
      }
      applyRef.current = true;
      setApplyLoading(true);
      try {
        const normSelectedAdd = normalizeModelList(selectedAdd);
        const selectedAddSet = new Set(normSelectedAdd);
        const ignoreModels = addModels.filter((m) => !selectedAddSet.has(m));

        const res = await api.post(
          "/api/channel/upstream_updates/apply",
          {
            id: channel.id,
            add_models: normSelectedAdd,
            ignore_models: ignoreModels,
            remove_models: normalizeModelList(selectedRemove),
          },
          upstreamUpdateRequestConfig,
        );
        const { success, message, data } = res.data || {};
        if (!success) {
          toast.error(message || t("Operation failed"));
          return;
        }

        toast.success(
          t(
            "Upstream model updates applied: {{added}} added, {{removed}} removed, {{ignored}} ignored this time, {{totalIgnored}} total ignored models",
            {
              added: data?.added_models?.length || 0,
              removed: data?.removed_models?.length || 0,
              ignored: normalizeModelList(ignoreModels).length,
              totalIgnored: getManualIgnoredModelCount(data?.settings),
            },
          ),
        );
        closeModal();
        await refresh();
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } };
          message?: string;
        };
        toast.error(
          err?.response?.data?.message || err?.message || t("Operation failed"),
        );
      } finally {
        applyRef.current = false;
        setApplyLoading(false);
      }
    },
    [channel, addModels, closeModal, refresh, t],
  );

  const applyAllUpdates = useCallback(async () => {
    if (applyAllRef.current) return;
    applyAllRef.current = true;
    setApplyAllLoading(true);
    try {
      const res = await api.post(
        "/api/channel/upstream_updates/apply_all",
        {},
        upstreamUpdateRequestConfig,
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        toast.error(message || t("Batch processing failed"));
        return;
      }

      const keptRemoveModels =
        typeof data?.remaining_remove_models_count === "number"
          ? data.remaining_remove_models_count
          : countRemainingRemoveModels(data?.results);
      toast.success(
        t(
          "Batch upstream model additions applied: {{channels}} channels, {{added}} added, {{kept}} pending removals kept for manual review, {{fails}} failed",
          {
            channels: data?.processed_channels || 0,
            added: data?.added_models || 0,
            kept: keptRemoveModels,
            fails: (data?.failed_channel_ids || []).length,
          },
        ),
      );
      await refresh();
    } catch (e: unknown) {
      const err = e as {
        response?: { data?: { message?: string } };
        message?: string;
      };
      toast.error(
        err?.response?.data?.message ||
          err?.message ||
          t("Batch processing failed"),
      );
    } finally {
      applyAllRef.current = false;
      setApplyAllLoading(false);
    }
  }, [refresh, t]);

  const detectChannelUpdates = useCallback(
    async (ch: { id: number; [key: string]: unknown } | null) => {
      if (detectRef.current) {
        toast.info(t("Please wait a moment before trying again."));
        return;
      }
      if (!ch?.id) return;
      detectRef.current = true;
      setDetectChannelLoadingId(ch.id);
      try {
        const res = await api.post(
          "/api/channel/upstream_updates/detect",
          { id: ch.id },
          upstreamUpdateRequestConfig,
        );
        const { success, message, data } = res.data || {};
        if (!success) {
          toast.error(message || t("Detection failed"));
          return;
        }

        toast.success(
          t("Detection complete: {{add}} to add, {{remove}} to remove", {
            add: data?.add_models?.length || 0,
            remove: data?.remove_models?.length || 0,
          }),
        );
        await refresh();
      } catch (e: unknown) {
        const err = e as {
          response?: { data?: { message?: string } };
          message?: string;
        };
        toast.error(
          err?.response?.data?.message || err?.message || t("Detection failed"),
        );
      } finally {
        detectRef.current = false;
        setDetectChannelLoadingId(null);
      }
    },
    [refresh, t],
  );

  const detectAllUpdates = useCallback(async () => {
    if (detectAllRef.current) return;
    detectAllRef.current = true;
    setDetectAllLoading(true);
    const waitAndReportTask = async (
      taskInfo: ModelUpdateTaskStartInfo,
      existingTask: boolean,
    ) => {
      if (existingTask) {
        toast.info(
          t("Batch detection task is already running. Waiting for completion"),
        );
      } else {
        toast.success(t("Batch detection task started"));
      }

      const task = await waitForModelUpdateTask(taskInfo.task_id);
      if (!task) {
        toast.info(t("Batch detection is still running. Please refresh later"));
        return;
      }

      if (task.status === "failed") {
        toast.error(task.error || t("Batch detection failed"));
        return;
      }

      const result = task.result || {};
      toast.success(
        t(
          "Batch detection complete: {{channels}} channels, {{add}} to add, {{remove}} to remove, {{fails}} failed",
          {
            channels: result.checked_channels || 0,
            add: result.detected_add_models || 0,
            remove: result.detected_remove_models || 0,
            fails: result.failed_channels || 0,
          },
        ),
      );
      await refresh();
    };
    try {
      const res = await api.post(
        "/api/channel/upstream_updates/detect_all",
        {},
        upstreamUpdateRequestConfig,
      );
      const taskInfo = getModelUpdateTaskStartInfo(res.data);
      if (!isSuccessPayload(res.data) || !taskInfo) {
        toast.error(
          getResponseMessage(res.data) || t("Batch detection failed"),
        );
        return;
      }

      await waitAndReportTask(taskInfo, false);
    } catch (e: unknown) {
      const taskInfo = getModelUpdateTaskStartInfo(getErrorPayload(e));
      if (taskInfo) {
        try {
          await waitAndReportTask(taskInfo, true);
        } catch (pollError: unknown) {
          toast.error(
            getErrorMessage(pollError) || t("Batch detection failed"),
          );
        }
        return;
      }
      toast.error(getErrorMessage(e) || t("Batch detection failed"));
    } finally {
      detectAllRef.current = false;
      setDetectAllLoading(false);
    }
  }, [refresh, t]);

  // Memoized so consumers (and the channels context value built from this) get
  // a stable reference unless an actual field changes. Callbacks above are all
  // useCallback-stable, so this only changes when relevant state changes.
  return useMemo(
    () => ({
      showModal,
      channel,
      addModels,
      removeModels,
      preferredTab,
      applyLoading,
      detectChannelLoadingId,
      detectAllLoading,
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
    }),
    [
      showModal,
      channel,
      addModels,
      removeModels,
      preferredTab,
      applyLoading,
      detectChannelLoadingId,
      detectAllLoading,
      applyAllLoading,
      openModal,
      closeModal,
      applyUpdates,
      applyAllUpdates,
      detectChannelUpdates,
      detectAllUpdates,
    ],
  );
}
