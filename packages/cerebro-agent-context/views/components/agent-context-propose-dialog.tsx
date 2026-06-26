"use client";

import { useState } from "react";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
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
import { useCreateAgentContextChangeRequest } from "../../core/mutations";

interface Props {
  agent: Agent;
}

// bumpPatch returns the next strict-semver patch of a valid X.Y.Z, else 1.0.1.
function bumpPatch(v: string): string {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(v);
  if (!m) return "1.0.1";
  return `${m[1]}.${m[2]}.${Number(m[3]) + 1}`;
}

export function AgentContextProposeDialog({ agent }: Props) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [instructions, setInstructions] = useState(agent.instructions);

  const { data: versions = [] } = useQuery(agentContextVersionsOptions(agent.id));
  const currentVersion = versions[0]?.version ?? "1.0.0";
  const proposedVersion = bumpPatch(currentVersion);

  const mutation = useCreateAgentContextChangeRequest(agent.id);

  const reset = () => {
    setTitle("");
    setDescription("");
    setInstructions(agent.instructions);
  };

  const handleSubmit = async () => {
    if (!title.trim()) return;
    try {
      await mutation.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: proposedVersion,
        instructions,
      });
      toast.success("Change proposed — the owner will be notified.");
      setOpen(false);
      reset();
    } catch {
      toast.error("Failed to submit proposal");
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

      <Dialog open={open} onOpenChange={(v) => !mutation.isPending && setOpen(v)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>Propose an agent context change</DialogTitle>
            <DialogDescription>
              Edit the instructions and submit a versioned change request (
              {currentVersion} → {proposedVersion}). The owner/approvers review it
              before it applies.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
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
            <div className="space-y-1.5">
              <Label htmlFor="ac-instructions" className="text-xs">
                Instructions
              </Label>
              <Textarea
                id="ac-instructions"
                value={instructions}
                onChange={(e) => setInstructions(e.target.value)}
                rows={14}
                className="font-mono text-xs"
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleSubmit}
              disabled={mutation.isPending || !title.trim()}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Submitting…
                </>
              ) : (
                <>
                  <Send className="h-3 w-3" />
                  Submit proposal
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
