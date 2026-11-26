"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useSetChatExecutionSelection } from "@multica/core/chat/mutations";
import { runtimeListOptions, runtimeModelsOptions } from "@multica/core/runtimes";
import type { Agent, ChatSession, RuntimeModel } from "@multica/core/types";
import { useT } from "../../i18n";
import { findModelCapabilityEntry } from "../../agents/components/inspector/model-capability";

const INHERIT = "__inherit__";

export function nextChatModelSelection(
  model: string | null,
  thinkingLevel: string | null,
  agentLevel: string,
  models: readonly RuntimeModel[],
  provider: string,
): { model: string | null; thinkingLevel: string | null } {
  const selected = findModelCapabilityEntry(models, model || models.find((entry) => entry.default)?.id || "", provider);
  const inheritedLevel = thinkingLevel ?? agentLevel;
  const supported = selected?.thinking?.supported_levels.some((entry) => entry.value === inheritedLevel);
  if (inheritedLevel && !supported) {
    return {
      model,
      thinkingLevel: provider === "zcode" ? selected?.thinking?.default_level ?? "" : "",
    };
  }
  if (provider === "zcode" && selected?.thinking?.supported_levels.length && !inheritedLevel) {
    return { model, thinkingLevel: selected.thinking.default_level ?? selected.thinking.supported_levels[0]!.value };
  }
  return { model, thinkingLevel };
}

/** The conversation setting applies to future chat tasks; running tasks retain their snapshot. */
export function ChatExecutionSelection({
  session,
  agent,
}: {
  session: ChatSession;
  agent: Agent | null;
}) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  const runtime = runtimes.find((entry) => entry.id === agent?.runtime_id);
  const modelsQuery = useQuery(runtimeModelsOptions(runtime?.status === "online" ? runtime.id : null));
  const mutation = useSetChatExecutionSelection();
  if (!agent) return null;

  const models = modelsQuery.data?.models ?? [];
  const catalogReady = modelsQuery.isSuccess && modelsQuery.data.supported && models.length > 0;
  const model = session.model_override ?? null;
  const thinkingLevel = session.thinking_level_override ?? null;
  const effectiveModel = model ?? agent.model ?? "";
  const selectedModel = findModelCapabilityEntry(models, effectiveModel, runtime?.provider ?? "") ??
    (effectiveModel === "" ? models.find((entry) => entry.default) : undefined);
  const effectiveLevel = thinkingLevel ?? agent.thinking_level ?? "";
  const levels = selectedModel?.thinking?.supported_levels ?? [];
  const staleModel = catalogReady && !!effectiveModel && !selectedModel;
  const staleLevel = catalogReady && !!effectiveLevel && !!selectedModel &&
    !levels.some((entry) => entry.value === effectiveLevel);
  const source = model === null && thinkingLevel === null
    ? t(($) => $.execution.inherited)
    : t(($) => $.execution.overridden);
  const save = (next: { model: string | null; thinkingLevel: string | null }) => {
    mutation.mutate(
      { sessionId: session.id, ...next },
      { onError: () => toast.error(t(($) => $.execution.save_failed)) },
    );
  };

  return (
    <div data-slot="chat-execution-selection" className="flex min-w-0 flex-1 items-center gap-1 text-caption text-muted-foreground" title={source}>
      <label className="flex min-w-0 flex-[3] items-center gap-1 @min-[560px]:flex-none">
        <span className="hidden @min-[560px]:inline">{t(($) => $.execution.model)}</span>
        <select
          aria-label={t(($) => $.execution.model)}
          className="w-full min-w-0 rounded-md border border-input bg-background px-1 py-0.5 text-foreground @min-[560px]:w-40"
          title={`${source}: ${effectiveModel || t(($) => $.execution.runtime_default)}`}
          disabled={mutation.isPending || session.status === "archived" || !catalogReady}
          value={model ?? INHERIT}
          onChange={(event) => {
            const nextModel = event.target.value === INHERIT ? null : event.target.value;
            save(nextChatModelSelection(nextModel, thinkingLevel, agent.thinking_level ?? "", models, runtime?.provider ?? ""));
          }}
        >
          <option value={INHERIT}>{t(($) => $.execution.inherit_model, { model: agent.model || t(($) => $.execution.runtime_default) })}</option>
          {model !== null && model !== selectedModel?.id && <option value={model}>{model || t(($) => $.execution.runtime_default)}{staleModel ? ` (${t(($) => $.execution.unavailable)})` : ""}</option>}
          {models.map((entry) => <option key={entry.id} value={entry.id}>{entry.label}</option>)}
        </select>
      </label>
      <label className="flex min-w-0 flex-[2] items-center gap-1 @min-[560px]:flex-none">
        <span className="hidden @min-[560px]:inline">{t(($) => $.execution.reasoning)}</span>
        <select
          aria-label={t(($) => $.execution.reasoning)}
          className="w-full min-w-0 rounded-md border border-input bg-background px-1 py-0.5 text-foreground @min-[560px]:w-32"
          title={`${source}: ${effectiveLevel || t(($) => $.execution.runtime_default)}`}
          disabled={mutation.isPending || session.status === "archived" || !catalogReady || !selectedModel}
          value={thinkingLevel ?? INHERIT}
          onChange={(event) => save({
            model,
            thinkingLevel: event.target.value === INHERIT ? null : event.target.value,
          })}
        >
          <option value={INHERIT}>{t(($) => $.execution.inherit_reasoning, { level: agent.thinking_level || t(($) => $.execution.runtime_default) })}</option>
          {runtime?.provider !== "zcode" && <option value="">{t(($) => $.execution.runtime_default)}</option>}
          {thinkingLevel !== null && !!effectiveLevel && !levels.some((entry) => entry.value === effectiveLevel) && <option value={thinkingLevel}>{effectiveLevel}{staleLevel ? ` (${t(($) => $.execution.unavailable)})` : ""}</option>}
          {levels.map((entry) => <option key={entry.value} value={entry.value}>{entry.label}</option>)}
        </select>
      </label>
      <span className="shrink-0">{source}</span>
      {(staleModel || staleLevel) && (
        <span role="status" className="shrink-0 text-destructive" title={t(($) => $.execution.stale)}>
          <span className="@min-[560px]:hidden" aria-label={t(($) => $.execution.stale)}>!</span>
          <span className="hidden @min-[560px]:inline">{t(($) => $.execution.stale)}</span>
        </span>
      )}
    </div>
  );
}
