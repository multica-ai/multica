"use client";

import { useState } from "react";
import { Check, Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { agentContextVersionsOptions } from "../../core/queries";
import {
  useCreateAgentContextChangeRequest,
  useReviewAgentContextChangeRequest,
} from "../../core/mutations";
import { AgentContextSnapshotView } from "./agent-context-snapshot-view";
import { AgentContextConfigFields } from "./agent-context-config-fields";

interface Props {
  agent: Agent;
  /** True when the current user can approve (owner/approver/admin). Unlocks
   *  the one-step "Save & approve" action so an approver doesn't have to
   *  propose to themselves and then approve their own request. */
  canReview?: boolean;
}

// bumpPatch returns the next strict-semver patch of a valid X.Y.Z, else 1.0.1.
function bumpPatch(v: string): string {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(v);
  if (!m) return "1.0.1";
  return `${m[1]}.${m[2]}.${Number(m[3]) + 1}`;
}

export function AgentContextProposeDialog({ agent, canReview = false }: Props) {
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState(agent.instructions);
  const [model, setModel] = useState(agent.model ?? "");
  const [thinkingLevel, setThinkingLevel] = useState(agent.thinking_level ?? "");
  const [personaSandbox, setPersonaSandbox] = useState(
    agent.persona_sandbox ?? "",
  );

  const { data: versions = [] } = useQuery(agentContextVersionsOptions(agent.id));
  const currentVersion = versions[0]?.version ?? "1.0.0";
  const proposedVersion = bumpPatch(currentVersion);
  const currentSnapshot = versions[0]?.snapshot;

  const mutation = useCreateAgentContextChangeRequest(agent.id);
  const reviewMutation = useReviewAgentContextChangeRequest(agent.id, wsId);
  const busy = mutation.isPending || reviewMutation.isPending;

  const reset = () => {
    setTitle("");
    setDescription("");
    setInstructions(agent.instructions);
    setModel(agent.model ?? "");
    setThinkingLevel(agent.thinking_level ?? "");
    setPersonaSandbox(agent.persona_sandbox ?? "");
  };

  // approve=true creates the change request and immediately merges it, so an
  // approver commits their edit in one step instead of proposing to themselves
  // and then approving their own pending request.
  const submit = async (approve: boolean) => {
    if (!title.trim()) return;
    try {
      const cr = await mutation.mutateAsync({
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
          crId: cr.id,
          data: { action: "approve" },
        });
        toast.success(`Change approved — now live as ${proposedVersion}.`);
      } else {
        toast.success("Change proposed — the owner will be notified.");
      }
      setOpen(false);
      reset();
    } catch {
      toast.error(
        approve ? "Failed to save & approve" : "Failed to submit proposal",
      );
    }
  };

  return (
    <>
      <Button
        type="button"
        size="xs"
        className="gap-1 bg-blue-600 text-white hover:bg-blue-700"
        onClick={() => {
          reset();
          setOpen(true);
        }}
      >
        <Send className="h-3 w-3" />
        Propose change
      </Button>

      <Dialog open={open} onOpenChange={(v) => !busy && setOpen(v)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Propose an agent context change</DialogTitle>
            <DialogDescription>
              Edit any field below and submit a versioned change request (
              {currentVersion} → {proposedVersion}). The whole bundle is
              versioned together; the owner/approvers review the field-by-field
              diff before it applies.
            </DialogDescription>
          </DialogHeader>

          <div className="max-h-[65vh] space-y-4 overflow-y-auto pr-1">
            <div className="space-y-1.5">
              <Label htmlFor="ac-title" className="text-xs">
                Title <span className="text-destructive">*</span>
              </Label>
              <Input
                id="ac-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g. Tighten the approval gate wording"
                className="h-8 text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="ac-desc" className="text-xs">
                Rationale{" "}
                <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id="ac-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Why this change improves the agent…"
                rows={2}
                className="resize-none text-sm"
              />
            </div>

            <div className="h-px bg-border" />
            <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
              Editing
            </p>

            <div className="space-y-1.5">
              <Label htmlFor="ac-instructions" className="text-xs">
                Instructions
              </Label>
              <Textarea
                id="ac-instructions"
                value={instructions}
                onChange={(e) => setInstructions(e.target.value)}
                rows={12}
                className="font-mono text-xs"
              />
            </div>

            <AgentContextConfigFields
              agent={agent}
              model={model}
              thinkingLevel={thinkingLevel}
              personaSandbox={personaSandbox}
              onModel={setModel}
              onThinkingLevel={setThinkingLevel}
              onPersonaSandbox={setPersonaSandbox}
            />

            {currentSnapshot && (
              <>
                <div className="h-px bg-border" />
                <div>
                  <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                    Carried over unchanged
                  </p>
                  <p className="mb-2 mt-0.5 text-[11px] text-muted-foreground">
                    Also part of this version. Edit these from their own tabs;
                    they roll into the next snapshot automatically.
                  </p>
                  <AgentContextSnapshotView
                    snapshot={currentSnapshot}
                    exclude={[
                      "instructions",
                      "description",
                      "model",
                      "thinking_level",
                      "persona_sandbox",
                    ]}
                  />
                </div>
              </>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={busy}
            >
              Cancel
            </Button>
            {/* An approver gets both paths: propose (leave it pending for a
                second reviewer) or save & approve (commit it live now). A
                non-approver only sees the propose action. */}
            <Button
              type="button"
              size="sm"
              variant={canReview ? "outline" : "default"}
              onClick={() => submit(false)}
              disabled={busy || !title.trim()}
            >
              {mutation.isPending && !reviewMutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Submitting…
                </>
              ) : (
                <>
                  <Send className="h-3 w-3" />
                  {canReview ? "Propose only" : "Submit proposal"}
                </>
              )}
            </Button>
            {canReview && (
              <Button
                type="button"
                size="sm"
                onClick={() => submit(true)}
                disabled={busy || !title.trim()}
              >
                {reviewMutation.isPending ? (
                  <>
                    <Loader2 className="h-3 w-3 animate-spin" />
                    Saving…
                  </>
                ) : (
                  <>
                    <Check className="h-3 w-3" />
                    Save &amp; approve
                  </>
                )}
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
