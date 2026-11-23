"use client";

import { useEffect, useRef, useState } from "react";
import { AlertCircle, Loader2 } from "@multica/ui/icons";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type {
  Agent,
  AgentRuntime,
  AgentRuntimeConfigStrategy,
  AgentRuntimeConfigSummary,
  MemberWithUser,
  UpdateAgentRuntimeCurrentConfigRequest,
} from "@multica/core/types";
import {
  agentRuntimeConfigKeys,
  agentRuntimeCurrentConfigOptions,
} from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  SettingsCard,
  SettingsRow,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useT } from "../../i18n";
import { ConcurrencySettingField } from "./inspector/concurrency-setting-field";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { ServiceTierSettingField } from "./inspector/service-tier-setting-field";
import { ThinkingSettingField } from "./inspector/thinking-prop-row";
import {
  runtimeConfigResolutionMessage,
  runtimeConfigResolutionMessageFromError,
  runtimeConfigResolutionStatusFromError,
} from "../runtime-config-resolution";

interface CurrentRuntimeConfigSectionProps {
  agent: Agent;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
}

/**
 * Per-user runtime configuration for smart-runtime agents.
 * Shown when the agent does not share a single fixed runtime across users.
 */
export function CurrentRuntimeConfigSection({
  agent,
  runtimes,
  members,
  currentUserId,
}: CurrentRuntimeConfigSectionProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const userId = currentUserId ?? "";
  const configKey = agentRuntimeConfigKeys.current(wsId, agent.id, userId);
  const runtimeConfigQuery = useQuery(agentRuntimeCurrentConfigOptions(wsId, agent.id, userId));
  const runtimeConfigSummary =
    runtimeConfigQuery.data ?? agent.runtime_config_summary ?? null;
  const currentConfig = runtimeConfigSummary?.current ?? null;
  const currentResolutionMessage = currentConfig
    ? runtimeConfigResolutionMessage(currentConfig, t)
    : null;
  const currentRuntime = currentConfig?.runtime_id
    ? (runtimes.find((r) => r.id === currentConfig.runtime_id) ?? null)
    : null;
  const currentUserRuntimes = currentUserId
    ? runtimes.filter((r) => r.owner_id === currentUserId)
    : [];
  const [savingRuntimeConfig, setSavingRuntimeConfig] = useState(false);
  const [markingNoticeSeen, setMarkingNoticeSeen] = useState(false);
  const currentConfigStrategyEditable =
    !!currentConfig?.editable && !savingRuntimeConfig;
  const currentConfigFieldsEditable =
    currentConfigStrategyEditable &&
    currentUserRuntimes.length > 0 &&
    currentConfig?.strategy === "custom";

  const cacheRuntimeConfigSummary = (summary: AgentRuntimeConfigSummary) => {
    queryClient.setQueryData(
      configKey,
      summary,
    );
    queryClient.setQueryData<Agent[]>(workspaceKeys.agents(wsId), (old) =>
      old?.map((a) =>
        a.id === agent.id
          ? ({ ...a, runtime_config_summary: summary } as Agent)
          : a,
      ),
    );
  };

  const saveFailedMessage = (failure: unknown) =>
    runtimeConfigResolutionMessageFromError(failure, t) ??
    (failure instanceof Error
      ? failure.message
      : t(($) => $.runtime_config.save_failed_toast));

  const updateCurrentRuntimeConfig = async (
    patch: Partial<UpdateAgentRuntimeCurrentConfigRequest>,
  ) => {
    if (!currentConfig) return;
    if (patch.strategy === "recommended") {
      setSavingRuntimeConfig(true);
      try {
        const summary = await api.updateAgentRuntimeCurrentConfig(agent.id, {
          strategy: "recommended",
        });
        cacheRuntimeConfigSummary(summary);
        toast.success(t(($) => $.runtime_config.saved_toast));
      } catch (err) {
        toast.error(saveFailedMessage(err));
      } finally {
        setSavingRuntimeConfig(false);
      }
      return;
    }

    const runtimeId = patch.runtime_id ?? currentConfig.runtime_id;
    if (!runtimeId) return;

    const request: UpdateAgentRuntimeCurrentConfigRequest = {
      strategy: patch.strategy ?? "custom",
      runtime_id: runtimeId,
      model: patch.model ?? currentConfig.model ?? "",
      thinking_level:
        patch.thinking_level ?? currentConfig.thinking_level ?? "",
      service_tier:
        patch.service_tier ?? currentConfig.service_tier ?? "",
      max_concurrent_tasks:
        patch.max_concurrent_tasks ??
        currentConfig.max_concurrent_tasks ??
        agent.max_concurrent_tasks,
      runtime_config:
        patch.runtime_config ?? currentConfig.runtime_config ?? {},
    };

    setSavingRuntimeConfig(true);
    try {
      const summary = await api.updateAgentRuntimeCurrentConfig(
        agent.id,
        request,
      );
      cacheRuntimeConfigSummary(summary);
      toast.success(t(($) => $.runtime_config.saved_toast));
    } catch (err) {
      // The first custom-mode save carries the recommended template
      // config. When the template model has left the runtime's live catalog,
      // that save is refused — and refusing it deadlocks the screen, because
      // picking a new model requires entering custom mode first. Retry once
      // without the runtime-native template fields (model/thinking/speed),
      // the same reset switching runtimes already does, and say what
      // happened. Only when this request did not explicitly pick a model: a
      // hand-picked model id keeps its named-model refusal, so the user
      // learns that id itself was rejected.
      if (
        patch.model === undefined &&
        request.model !== "" &&
        runtimeConfigResolutionStatusFromError(err) === "model_unavailable"
      ) {
        try {
          const fallbackSummary = await api.updateAgentRuntimeCurrentConfig(
            agent.id,
            {
              ...request,
              model: "",
              thinking_level: patch.thinking_level ?? "",
              service_tier: patch.service_tier ?? "",
            },
          );
          cacheRuntimeConfigSummary(fallbackSummary);
          toast.warning(
            t(($) => $.runtime_config.template_model_reset_toast, {
              model: request.model,
            }),
          );
          return;
        } catch (retryErr) {
          // The retry is what stands between the user and custom mode now.
          // The "model left the catalog" copy is stale once the model has
          // been cleared, so an offline runtime, a dropped network or a 500
          // on the retry must be reported as itself.
          toast.error(saveFailedMessage(retryErr));
          return;
        }
      }
      toast.error(saveFailedMessage(err));
    } finally {
      setSavingRuntimeConfig(false);
    }
  };

  const markRuntimeConfigNoticeSeen = async () => {
    setMarkingNoticeSeen(true);
    try {
      const summary = await api.markAgentRuntimeConfigNoticeSeen(agent.id);
      cacheRuntimeConfigSummary(summary);
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.runtime_config.notice_seen_failed_toast),
      );
    } finally {
      setMarkingNoticeSeen(false);
    }
  };

  // The resolver only produces `source: "shared"` for a share_runtime agent.
  // That summary under the smart-runtime gate that renders this section means
  // the agent object on this page is stale — the agent was switched to a fixed
  // runtime somewhere this client did not see (another tab, another member, or
  // a missed event). Rendering the card would dead-end every control behind a
  // misleading hint, so refuse it and pull the caches until the form converges
  // on the fixed layout (or, if the drift was the other way, on a fresh
  // editable summary).
  const summarySaysShared = currentConfig?.source === "shared";
  const sharedHealRef = useRef(false);
  useEffect(() => {
    if (!summarySaysShared) {
      sharedHealRef.current = false;
      return;
    }
    if (sharedHealRef.current) return;
    sharedHealRef.current = true;
    if (wsId) {
      queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    }
    queryClient.invalidateQueries({
      queryKey: agentRuntimeConfigKeys.current(wsId, agent.id, userId),
    });
  }, [summarySaysShared, wsId, userId, agent.id, queryClient]);

  if (summarySaysShared) {
    return null;
  }

  return (
    <SettingsSection
      title={t(($) => $.runtime_config.current_section)}
      description={t(($) => $.inspector.section_execution_hint)}
    >
      <SettingsCard>
        {runtimeConfigQuery.isLoading && !currentConfig && (
          <div className="flex items-center gap-2 px-1 py-2 text-caption text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            {t(($) => $.runtime_config.loading)}
          </div>
        )}
        {runtimeConfigQuery.isError && !currentConfig && (
          <div className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-caption text-destructive">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            {t(($) => $.runtime_config.load_failed)}
          </div>
        )}
        {currentConfig?.notice_required && (
          <div className="mb-4 flex items-start gap-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
            <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <span className="min-w-0 flex-1">
              {t(($) => $.runtime_config.notice_required)}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-6 shrink-0 px-2 text-caption"
              disabled={markingNoticeSeen}
              onClick={() => void markRuntimeConfigNoticeSeen()}
            >
              {markingNoticeSeen
                ? t(($) => $.runtime_config.confirming_notice)
                : t(($) => $.runtime_config.confirm_notice)}
            </Button>
          </div>
        )}
        {currentConfig && (
          <>
            <SettingsRow
              label={t(($) => $.runtime_config.current_strategy)}
              size="select-wide"
            >
              <CurrentRuntimeConfigStrategyControl
                strategy={currentConfig.strategy}
                canEdit={currentConfigStrategyEditable}
                onChange={(strategy) =>
                  updateCurrentRuntimeConfig({ strategy })
                }
              />
            </SettingsRow>
            <SettingsRow
              label={t(($) => $.runtime_config.current_runtime)}
              size="select-wide"
            >
              <RuntimePicker
                variant="field"
                showLabel={false}
                value={currentConfig.runtime_id ?? ""}
                runtimes={currentUserRuntimes}
                members={members}
                currentUserId={currentUserId}
                canEdit={currentConfigFieldsEditable}
                // Model, thinking level, and speed are runtime-native. Match
                // the recommended-runtime editor by clearing them so the newly
                // selected runtime resolves its own defaults.
                onChange={(id) =>
                  updateCurrentRuntimeConfig({
                    runtime_id: id,
                    model: "",
                    thinking_level: "",
                    service_tier: "",
                  })
                }
              />
            </SettingsRow>
            <SettingsRow
              label={t(($) => $.runtime_config.current_model)}
              size="select-wide"
            >
              <ModelPicker
                variant="field"
                showLabel={false}
                runtimeId={currentConfig.runtime_id}
                runtimeOnline={currentRuntime?.status === "online"}
                value={currentConfig.model ?? ""}
                canEdit={currentConfigFieldsEditable}
                onChange={(model) => updateCurrentRuntimeConfig({ model })}
              />
            </SettingsRow>
            <ThinkingSettingField
              label={t(($) => $.runtime_config.current_thinking)}
              runtimeId={currentConfig.runtime_id}
              runtimeOnline={currentRuntime?.status === "online"}
              provider={currentRuntime?.provider ?? ""}
              model={currentConfig.model ?? ""}
              value={currentConfig.thinking_level ?? ""}
              canEdit={currentConfigFieldsEditable}
              onChange={(thinking_level) =>
                updateCurrentRuntimeConfig({ thinking_level })
              }
            />
            <ServiceTierSettingField
              label={t(($) => $.runtime_config.current_speed)}
              runtimeId={currentConfig.runtime_id}
              runtimeOnline={currentRuntime?.status === "online"}
              provider={currentRuntime?.provider ?? ""}
              model={currentConfig.model ?? ""}
              value={currentConfig.service_tier ?? ""}
              canEdit={currentConfigFieldsEditable}
              onChange={(service_tier) =>
                updateCurrentRuntimeConfig({ service_tier })
              }
            />
            <SettingsRow
              label={t(($) => $.runtime_config.current_concurrency)}
              size="select-wide"
            >
              <ConcurrencySettingField
                value={currentConfig.max_concurrent_tasks}
                canEdit={currentConfigFieldsEditable}
                inputId="agent-current-concurrency"
                ariaLabel={t(($) => $.runtime_config.current_concurrency)}
                onChange={(max_concurrent_tasks) =>
                  updateCurrentRuntimeConfig({ max_concurrent_tasks })
                }
              />
            </SettingsRow>
            {!currentConfig.editable && (
              <p className="px-1 pt-1 text-caption text-muted-foreground">
                {currentResolutionMessage ??
                  t(($) => $.runtime_config.not_editable)}
              </p>
            )}
          </>
        )}
      </SettingsCard>
    </SettingsSection>
  );
}

function CurrentRuntimeConfigStrategyControl({
  strategy,
  canEdit,
  onChange,
}: {
  strategy: AgentRuntimeConfigStrategy;
  canEdit: boolean;
  onChange: (next: AgentRuntimeConfigStrategy) => Promise<void> | void;
}) {
  const { t } = useT("agents");
  const label = t(($) => $.runtime_config.current_strategy);
  const options: Array<{ value: AgentRuntimeConfigStrategy; label: string }> = [
    {
      value: "recommended",
      label: t(($) => $.runtime_config.strategy_recommended),
    },
    {
      value: "custom",
      label: t(($) => $.runtime_config.strategy_custom),
    },
  ];

  return (
    <div
      className="grid min-w-0 flex-1 grid-cols-2 gap-1"
      role="radiogroup"
      aria-label={label}
    >
      {options.map((option) => {
        const selected = strategy === option.value;
        return (
          <button
            key={option.value}
            type="button"
            role="radio"
            aria-checked={selected}
            disabled={!canEdit}
            onClick={() => {
              if (!selected) void onChange(option.value);
            }}
            className={`min-w-0 rounded-md border px-2 py-1 text-center text-micro font-medium transition-colors ${
              selected
                ? "border-primary bg-primary/5 text-foreground"
                : "border-border text-muted-foreground hover:bg-muted"
            } ${canEdit ? "" : "cursor-not-allowed text-muted-foreground hover:bg-transparent"}`}
          >
            <span className="block truncate">{option.label}</span>
          </button>
        );
      })}
    </div>
  );
}
