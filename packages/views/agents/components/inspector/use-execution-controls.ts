"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import {
  isRuntimeUsableForUser,
  runtimeModelsOptions,
} from "@multica/core/runtimes";
import { api } from "@multica/core/api";
import { buildModelChangeUpdate, type ModelCatalog } from "./model-change-cleanup";

/**
 * Runtime and model editing shared by the properties panel and the General
 * settings form, so both write the same PATCH for the same change.
 */
export function useExecutionControls({
  agent,
  runtime: fixedRuntime,
  runtimes,
  currentUserId,
  update,
  onSaved,
}: {
  agent: Agent;
  runtime: AgentRuntime | null;
  runtimes: AgentRuntime[];
  currentUserId: string | null;
  update: (data: Record<string, unknown>) => Promise<void>;
  /** Runs after a personal-config save so the panel re-reads the summary. */
  onSaved?: () => void;
}) {
  // Smart agents carry no template runtime: the viewer's personal runtime
  // configuration names the runtime (and model) their runs actually use, so
  // reads resolve through it and writes go to the personal-config endpoint
  // instead of the template PATCH (which the server rejects for smart
  // agents with `smart strategy requires a CLI and no runtime_id`).
  const isSmart = agent.share_runtime === false;
  const currentConfig = agent.runtime_config_summary?.current ?? null;
  const effectiveRuntimeId =
    (isSmart ? currentConfig?.runtime_id : agent.runtime_id) ?? "";
  const runtime = useMemo(
    () => (isSmart ? runtimes.find((r) => r.id === effectiveRuntimeId) ?? null : fixedRuntime),
    [isSmart, effectiveRuntimeId, runtimes, fixedRuntime],
  );
  // A Qoder agent is bound to an agent in Qoder Cloud Agents, and the model lives there. Moving
  // between Qoder and any other runtime would need a different configuration
  // the server either rejects or the bridge refuses, so neither direction is
  // offered.
  const qoderRuntime = runtime?.provider === "qoder_cloud";
  const isOnline = runtime?.status === "online";
  const canReadRuntime =
    runtime != null && isRuntimeUsableForUser(runtime, currentUserId);
  const canDiscoverRuntimeModels = isOnline && canReadRuntime && !qoderRuntime;
  const runtimeOptions = useMemo(
    () =>
      qoderRuntime
        ? runtimes
        : runtimes.filter((r) => r.provider !== "qoder_cloud"),
    [qoderRuntime, runtimes],
  );

  // Same query the Thinking / Speed fields already use, so switching model
  // costs no extra request. `null` = not authoritative (offline runtime, still
  // loading, or discovery failed) and must not trigger any clearing.
  const modelsQuery = useQuery(
    runtimeModelsOptions(canDiscoverRuntimeModels ? agent.runtime_id : null),
  );
  const modelCatalog = useMemo<ModelCatalog>(
    () =>
      modelsQuery.isSuccess
        ? modelsQuery.data.supported
          ? modelsQuery.data.models
          : []
        : null,
    [modelsQuery.data, modelsQuery.isSuccess],
  );

  const savePersonalConfig = useCallback(
    (patch: { runtime_id?: string; model?: string; thinking_level?: string }) =>
      api
        .updateAgentRuntimeCurrentConfig(agent.id, {
          strategy: "custom",
          runtime_id: patch.runtime_id ?? currentConfig?.runtime_id ?? "",
          model: patch.model ?? currentConfig?.model ?? "",
          thinking_level:
            patch.thinking_level ?? currentConfig?.thinking_level ?? "",
          service_tier: currentConfig?.service_tier ?? "",
          max_concurrent_tasks:
            currentConfig?.max_concurrent_tasks ?? agent.max_concurrent_tasks,
          runtime_config: currentConfig?.runtime_config ?? {},
        })
        .then(() => onSaved?.()),
    [agent.id, agent.max_concurrent_tasks, currentConfig, onSaved],
  );

  const changeModel = useCallback(
    (model: string) => {
      if (isSmart) {
        return savePersonalConfig({ model });
      }
      return update(
        buildModelChangeUpdate({
          provider: runtime?.provider ?? "",
          model,
          thinkingLevel: agent.thinking_level ?? "",
          serviceTier: agent.service_tier ?? "",
          catalog: modelCatalog,
        }),
      );
    },
    [agent.service_tier, agent.thinking_level, isSmart, modelCatalog, runtime?.provider, savePersonalConfig, update],
  );

  // Model, thinking level, and service tier are runtime/model native. Clear
  // them together so the new runtime resolves its own defaults instead of
  // inheriting incompatible tokens.
  const changeRuntime = useCallback(
    (runtimeId: string) => {
      if (isSmart) {
        // Switching runtimes clears the model/thinking overrides so the new
        // runtime resolves its own defaults, mirroring the fixed path.
        return savePersonalConfig({ runtime_id: runtimeId, model: "", thinking_level: "" });
      }
      return update({
        runtime_id: runtimeId,
        model: "",
        thinking_level: "",
        service_tier: "",
      });
    },
    [isSmart, savePersonalConfig, update],
  );

  return {
    qoderRuntime,
    canDiscoverRuntimeModels,
    runtimeOptions,
    changeModel,
    changeRuntime,
  };
}
