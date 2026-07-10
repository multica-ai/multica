"use client";

// Agent Office (FIR-1775) — a small, field-scoped Propose-change dialog used by
// the Skills and MCP tabs. Unlike AgentContextProposeDialog (which edits the
// instructions bundle), this one carries a single pre-built override
// (e.g. { skill_ids } or { mcp_config }) that the tab assembled from its own
// inline editor, and routes it through the same versioned change-request flow:
// propose → owner reviews the diff → approve applies + snapshots a new version.

import { useEffect, useState } from "react";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  CreateAgentContextChangeRequestRequest,
} from "@multica/core/types";
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

// bumpPatch returns the next strict-semver patch of a valid X.Y.Z, else 1.0.1.
function bumpPatch(v: string): string {
  const m = /^(\d+)\.(\d+)\.(\d+)$/.exec(v);
  if (!m) return "1.0.1";
  return `${m[1]}.${m[2]}.${Number(m[3]) + 1}`;
}

interface Props {
  agent: Agent;
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /**
   * The field override(s) this proposal carries — merged onto the agent's
   * current snapshot server-side. e.g. { skill_ids } or { mcp_config }.
   */
  override: Partial<CreateAgentContextChangeRequestRequest>;
  /** What is being changed, for the dialog copy — e.g. "skills" or "MCP config". */
  fieldLabel: string;
  /** Pre-filled title; defaults to "Update <fieldLabel>". */
  defaultTitle?: string;
  /** Called after a successful propose (e.g. to clear the tab's draft). */
  onProposed?: () => void;
}

export function AgentContextFieldProposeDialog({
  agent,
  open,
  onOpenChange,
  override,
  fieldLabel,
  defaultTitle,
  onProposed,
}: Props) {
  const [title, setTitle] = useState(defaultTitle ?? "");
  const [description, setDescription] = useState("");

  const { data: versions = [] } = useQuery(
    agentContextVersionsOptions(agent.id),
  );
  const currentVersion = versions[0]?.version ?? "1.0.0";
  const proposedVersion = bumpPatch(currentVersion);

  const mutation = useCreateAgentContextChangeRequest(agent.id);

  // Reset the form each time the dialog is (re)opened.
  useEffect(() => {
    if (open) {
      setTitle(defaultTitle ?? "");
      setDescription("");
    }
  }, [open, defaultTitle]);

  const handleSubmit = async () => {
    const finalTitle = title.trim() || `Update ${fieldLabel}`;
    try {
      await mutation.mutateAsync({
        title: finalTitle,
        description: description.trim() || undefined,
        proposed_version: proposedVersion,
        ...override,
      });
      toast.success("Change proposed — the owner will be notified.");
      onOpenChange(false);
      onProposed?.();
    } catch {
      toast.error("Failed to submit proposal");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !mutation.isPending && onOpenChange(v)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Propose {fieldLabel} change</DialogTitle>
          <DialogDescription>
            Submit a versioned change request ({currentVersion} →{" "}
            {proposedVersion}). The owner/approvers review the diff before it
            applies — nothing changes until it&apos;s approved.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="acf-title" className="text-xs">
              Title
            </Label>
            <Input
              id="acf-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={`Update ${fieldLabel}`}
              className="h-8 text-sm"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="acf-desc" className="text-xs">
              Rationale{" "}
              <span className="text-muted-foreground">(optional)</span>
            </Label>
            <Textarea
              id="acf-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Why this change improves the agent…"
              rows={2}
              className="resize-none text-sm"
            />
          </div>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            Cancel
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={handleSubmit}
            disabled={mutation.isPending}
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
  );
}
