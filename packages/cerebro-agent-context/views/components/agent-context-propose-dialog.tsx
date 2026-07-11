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
} from "../../core/context-draft";
import { AgentContextConfigFields } from "./agent-context-config-fields";

interface Props {
  agent: Agent;
  canReview?: boolean;
}

export function AgentContextProposeDialog({ agent, canReview = false }: Props) {
  const wsId = useWorkspaceId();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState(agent.instructions);
  const [model, setModel] = useState(agent.model ?? "");
  const [thinkingLevel, setThinkingLevel] = useState(agent.thinking_level ?? "");
  const [personaSandbox, setPersonaSandbox] = useState(agent.persona_sandbox ?? "");

  const { data: versions = [] } = useQuery(agentContextVersionsOptions(agent.id));
  const currentVersion = versions[0]?.version ?? "1.0.0";
  const proposedVersion = bumpPatch(currentVersion);
  const mutation = useCreateAgentContextChangeRequest(agent.id);
  const reviewMutation = useReviewAgentContextChangeRequest(agent.id, wsId);
  const busy = mutation.isPending || reviewMutation.isPending;
  const current = {
    instructions: agent.instructions,
    model: agent.model ?? "",
    thinkingLevel: agent.thinking_level ?? "",
    personaSandbox: agent.persona_sandbox ?? "",
  };
  const draft = { instructions, model, thinkingLevel, personaSandbox };
  const dirty = isContextDraftDirty(current, draft);
  const canSubmit = canSubmitContextDraft(title, dirty) && !busy;

  const reset = () => {
    setTitle("");
    setDescription("");
    setInstructions(agent.instructions);
    setModel(agent.model ?? "");
    setThinkingLevel(agent.thinking_level ?? "");
    setPersonaSandbox(agent.persona_sandbox ?? "");
  };

  const submit = async (approve: boolean) => {
    if (!canSubmit) return;
    try {
      const changeRequest = await mutation.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: proposedVersion,
        instructions,
        model: model.trim(),
        thinking_level: thinkingLevel.trim(),
        persona_sandbox: personaSandbox.trim(),
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
    } catch {
      toast.error(approve ? "Failed to save & approve" : "Failed to submit proposal");
    }
  };

  return (
    <section aria-labelledby="agent-instructions-heading" className="space-y-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h3 id="agent-instructions-heading" className="text-sm font-semibold">
            Instructions
          </h3>
          <p className="mt-0.5 max-w-prose text-xs text-muted-foreground">
            Edit the versioned instructions here. Changes stay reviewable and can be rolled back.
          </p>
        </div>
        <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
          {currentVersion} → {proposedVersion}
        </span>
      </div>

      <Textarea
        id="agent-instructions"
        aria-label="Instructions"
        value={instructions}
        onChange={(event) => setInstructions(event.target.value)}
        readOnly={busy}
        rows={18}
        className="min-h-72 resize-y bg-background font-mono text-xs leading-relaxed"
      />

      <AgentContextConfigFields
        agent={agent}
        model={model}
        thinkingLevel={thinkingLevel}
        personaSandbox={personaSandbox}
        onModel={setModel}
        onThinkingLevel={setThinkingLevel}
        onPersonaSandbox={setPersonaSandbox}
      />

      {dirty && (
        <div className="space-y-4 rounded-xl border bg-background p-4" aria-live="polite">
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="ac-title" className="text-xs">
                Change title <span className="text-destructive">*</span>
              </Label>
              <Input
                id="ac-title"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="e.g. Tighten the approval gate wording"
                className="h-8 text-sm"
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
                placeholder="Why this change improves the agent"
                className="h-8 text-sm"
              />
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2 border-t pt-3">
            <Button type="button" variant="ghost" size="sm" onClick={reset} disabled={busy}>
              <RotateCcw className="h-3 w-3" />
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
                <Loader2 className="h-3 w-3 animate-spin" />
              ) : (
                <Send className="h-3 w-3" />
              )}
              {canReview ? "Propose" : "Submit proposal"}
            </Button>
            {canReview && (
              <Button type="button" size="sm" onClick={() => void submit(true)} disabled={!canSubmit}>
                {reviewMutation.isPending ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Check className="h-3 w-3" />
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
