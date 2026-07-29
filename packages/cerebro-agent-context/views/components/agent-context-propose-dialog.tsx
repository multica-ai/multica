"use client";

import { useState } from "react";
import { Check, Loader2, RotateCcw, Send } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { agentContextVersionsOptions } from "../../core/queries";
import {
  useCreateAgentContextChangeRequest,
  useReviewAgentContextChangeRequest,
} from "../../core/mutations";
import {
  bumpPatch,
  canSubmitContextDraft,
  isContextDraftDirty,
  readBriefLayerMode,
  readPositiveIntegerSetting,
  readSpeedMode,
  readSystemPromptMode,
  type ContextDraftFields,
} from "../../core/context-draft";
import { AgentContextConfigFields } from "./agent-context-config-fields";

interface Props {
  agent: Agent;
  canReview?: boolean;
  controlFirst?: boolean;
}

function currentDraft(agent: Agent): ContextDraftFields {
  return {
    instructions: agent.instructions,
    runtimeId: agent.runtime_id,
    model: agent.model ?? "",
    thinkingLevel: agent.thinking_level ?? "",
    workspaceBriefMode: readBriefLayerMode(
      agent.runtime_config,
      "workspace_brief_mode",
    ),
    toolsBriefMode: readBriefLayerMode(agent.runtime_config, "tools_brief_mode"),
    systemPromptMode: readSystemPromptMode(agent.runtime_config),
    speedMode: readSpeedMode(agent.runtime_config),
    maxTurns: readPositiveIntegerSetting(agent.runtime_config, "max_turns"),
    timeoutMinutes: readPositiveIntegerSetting(
      agent.runtime_config,
      "timeout_minutes",
    ),
  };
}

export function AgentContextProposeDialog({
  agent,
  canReview = false,
  controlFirst = false,
}: Props) {
  const wsId = useWorkspaceId();
  const initial = currentDraft(agent);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState(initial.instructions);
  const [runtimeId, setRuntimeId] = useState(initial.runtimeId);
  const [model, setModel] = useState(initial.model);
  const [thinkingLevel, setThinkingLevel] = useState(initial.thinkingLevel);
  const [workspaceBriefMode, setWorkspaceBriefMode] = useState(
    initial.workspaceBriefMode,
  );
  const [toolsBriefMode, setToolsBriefMode] = useState(initial.toolsBriefMode);
  const [systemPromptMode, setSystemPromptMode] = useState(
    initial.systemPromptMode,
  );
  const [speedMode, setSpeedMode] = useState(initial.speedMode);
  const [maxTurns, setMaxTurns] = useState(initial.maxTurns);
  const [timeoutMinutes, setTimeoutMinutes] = useState(initial.timeoutMinutes);

  const { data: versions = [] } = useQuery(
    agentContextVersionsOptions(agent.id),
  );
  const currentVersion = versions[0]?.version ?? "1.0.0";
  const proposedVersion = bumpPatch(currentVersion);
  const mutation = useCreateAgentContextChangeRequest(agent.id);
  const reviewMutation = useReviewAgentContextChangeRequest(agent.id, wsId);
  const busy = mutation.isPending || reviewMutation.isPending;
  const current = currentDraft(agent);
  const draft: ContextDraftFields = {
    instructions,
    runtimeId,
    model,
    thinkingLevel,
    workspaceBriefMode,
    toolsBriefMode,
    systemPromptMode,
    speedMode,
    maxTurns,
    timeoutMinutes,
  };
  const dirty = isContextDraftDirty(current, draft);
  const changeCount = Object.keys(current).filter(
    (key) =>
      current[key as keyof ContextDraftFields] !==
      draft[key as keyof ContextDraftFields],
  ).length;
  const canSubmit = canSubmitContextDraft(title, dirty) && !busy;

  const reset = () => {
    const next = currentDraft(agent);
    setTitle("");
    setDescription("");
    setInstructions(next.instructions);
    setRuntimeId(next.runtimeId);
    setModel(next.model);
    setThinkingLevel(next.thinkingLevel);
    setWorkspaceBriefMode(next.workspaceBriefMode);
    setToolsBriefMode(next.toolsBriefMode);
    setSystemPromptMode(next.systemPromptMode);
    setSpeedMode(next.speedMode);
    setMaxTurns(next.maxTurns);
    setTimeoutMinutes(next.timeoutMinutes);
  };

  const submit = async (approve: boolean) => {
    if (!canSubmit) return;
    try {
      const changeRequest = await mutation.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: proposedVersion,
        instructions,
        runtime_id: runtimeId,
        model: model.trim(),
        thinking_level: thinkingLevel.trim(),
        workspace_brief_mode: workspaceBriefMode as "" | "off",
        tools_brief_mode: toolsBriefMode as "" | "summary",
        system_prompt_mode: systemPromptMode as
          | ""
          | "append"
          | "replace"
          | "prepend",
        speed_mode: speedMode as "" | "standard" | "fast",
        max_turns: maxTurns === "" ? 0 : Number(maxTurns),
        timeout_minutes:
          timeoutMinutes === "" ? 0 : Number(timeoutMinutes),
      });
      if (approve) {
        await reviewMutation.mutateAsync({
          crId: changeRequest.id,
          data: { action: "approve" },
        });
        toast.success(`Change approved — now live as ${proposedVersion}.`);
      } else {
        toast.success("Change proposed — the owner will be notified.");
      }
      reset();
    } catch (error) {
      const message =
        error instanceof Error && error.message
          ? error.message
          : approve
            ? "Failed to save & approve"
            : "Failed to submit proposal";
      toast.error(message);
    }
  };

  const configFields = (
    <AgentContextConfigFields
      agent={agent}
      runtimeId={runtimeId}
      model={model}
      thinkingLevel={thinkingLevel}
      workspaceBriefMode={workspaceBriefMode}
      toolsBriefMode={toolsBriefMode}
      systemPromptMode={systemPromptMode}
      speedMode={speedMode}
      maxTurns={maxTurns}
      timeoutMinutes={timeoutMinutes}
      instructions={instructions}
      busy={busy}
      controlFirst={controlFirst}
      onInstructions={setInstructions}
      onRuntimeId={setRuntimeId}
      onModel={setModel}
      onThinkingLevel={setThinkingLevel}
      onWorkspaceBriefMode={setWorkspaceBriefMode}
      onToolsBriefMode={setToolsBriefMode}
      onSystemPromptMode={setSystemPromptMode}
      onSpeedMode={setSpeedMode}
      onMaxTurns={setMaxTurns}
      onTimeoutMinutes={setTimeoutMinutes}
    />
  );

  return (
    <section aria-labelledby="agent-instructions-heading" className="space-y-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3
            id="agent-instructions-heading"
            className={controlFirst ? "text-lg font-semibold" : "text-sm font-semibold"}
          >
            {controlFirst ? `${agent.name} configuration` : "Instructions"}
          </h3>
          <p className="mt-1 max-w-3xl text-xs text-muted-foreground">
            {controlFirst
              ? "Control what the agent reads and how each run behaves. Every change is versioned, reviewable and reversible."
              : "Edit the versioned instructions here. Changes stay reviewable and can be rolled back."}
          </p>
        </div>
        <span className="shrink-0 rounded-md border px-2 py-1 font-mono text-xs text-muted-foreground">
          {currentVersion} → {proposedVersion}
        </span>
      </div>

      {controlFirst ? (
        configFields
      ) : (
        <>
          <Textarea
            id="agent-instructions"
            aria-label="Instructions"
            value={instructions}
            onChange={(event) => setInstructions(event.target.value)}
            readOnly={busy}
            rows={18}
            className="min-h-72 resize-y bg-background font-mono text-xs leading-relaxed"
          />
          {configFields}
        </>
      )}

      {dirty && (
        <div
          className={
            controlFirst
              ? "sticky bottom-3 z-20 space-y-3 rounded-xl border bg-background/95 p-4 shadow-lg backdrop-blur"
              : "space-y-4 rounded-xl border bg-background p-4"
          }
          aria-live="polite"
        >
          <div className="grid gap-3 md:grid-cols-[minmax(14rem,1fr)_minmax(14rem,1fr)_auto] md:items-end">
            <div className="space-y-1.5">
              <Label htmlFor="ac-title" className="text-xs">
                Change title <span className="text-destructive">*</span>
              </Label>
              <Input
                id="ac-title"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="What changed?"
                className="text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ac-desc" className="text-xs">
                Rationale <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Input
                id="ac-desc"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                placeholder="Why this improves the agent"
                className="text-sm"
              />
            </div>
            <span className="pb-2 text-xs font-medium text-muted-foreground">
              {changeCount} {changeCount === 1 ? "change" : "changes"}
            </span>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-3">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={reset}
              disabled={busy}
            >
              <RotateCcw className="size-3.5" />
              Discard
            </Button>
            <Button
              type="button"
              size="sm"
              variant={canReview ? "outline" : "default"}
              onClick={() => void submit(false)}
              disabled={!canSubmit}
            >
              {mutation.isPending && !reviewMutation.isPending ? (
                <Loader2 className="size-3.5 animate-spin" />
              ) : (
                <Send className="size-3.5" />
              )}
              {canReview ? "Propose" : "Submit proposal"}
            </Button>
            {canReview && (
              <Button
                type="button"
                size="sm"
                onClick={() => void submit(true)}
                disabled={!canSubmit}
              >
                {reviewMutation.isPending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <Check className="size-3.5" />
                )}
                Save &amp; approve
              </Button>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
