"use client";

import { useState } from "react";
import {
  ChevronDown,
  ChevronRight,
  Clock,
  GitCommit,
  RotateCcw,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent, AgentContextVersion, MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { agentContextVersionsOptions } from "../../core/queries";
import { useRollbackAgentContext } from "../../core/mutations";
import { AgentContextFieldDiff } from "./agent-context-field-diff";

interface Props {
  agent: Agent;
  wsId: string;
  members: MemberWithUser[];
  canManage: boolean;
}

function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function AgentContextVersionsPanel({
  agent,
  wsId,
  members,
  canManage,
}: Props) {
  const [open, setOpen] = useState(true);
  const [diffTarget, setDiffTarget] = useState<AgentContextVersion | null>(null);
  const [rollbackTarget, setRollbackTarget] =
    useState<AgentContextVersion | null>(null);

  const { data: versions = [], isLoading } = useQuery(
    agentContextVersionsOptions(agent.id),
  );
  const rollback = useRollbackAgentContext(agent.id, wsId);

  const creatorName = (createdBy: string | null) => {
    if (!createdBy) return null;
    return members.find((m) => m.user_id === createdBy)?.name ?? null;
  };

  const handleRollback = async () => {
    if (!rollbackTarget) return;
    try {
      await rollback.mutateAsync({ version: rollbackTarget.version });
      toast.success(`Rolled back to ${rollbackTarget.version}`);
      setRollbackTarget(null);
    } catch {
      toast.error("Failed to roll back");
    }
  };

  return (
    <div>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between text-xs font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground"
      >
        <span className="flex items-center gap-1.5">
          <GitCommit className="h-3 w-3" />
          Version history
          {versions.length > 0 && (
            <Badge variant="secondary" className="h-4 text-xs">
              {versions.length}
            </Badge>
          )}
        </span>
        {open ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
      </button>

      {open && (
        <div className="mt-2 space-y-1">
          {isLoading && (
            <div className="text-xs text-muted-foreground">Loading…</div>
          )}
          {!isLoading && versions.length === 0 && (
            <div className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
              No saved versions yet.
            </div>
          )}
          {versions.map((v, idx) => {
            const name = creatorName(v.created_by);
            const isCurrent = idx === 0;
            return (
              <div
                key={v.id}
                className="flex items-center gap-2 rounded-md border bg-card px-2.5 py-1.5"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="truncate font-mono text-xs font-medium">
                      {v.version}
                    </span>
                    {isCurrent && (
                      <Badge variant="secondary" className="h-4 text-[10px]">
                        current
                      </Badge>
                    )}
                  </div>
                  <div className="flex items-center gap-1 text-xs text-muted-foreground">
                    <Clock className="h-2.5 w-2.5" />
                    {formatRelativeTime(v.created_at)}
                    {name && <span>· {name}</span>}
                  </div>
                  {v.description && (
                    <div className="mt-0.5 truncate text-xs text-muted-foreground">
                      {v.description}
                    </div>
                  )}
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  className="h-5 shrink-0 px-1.5 text-xs"
                  onClick={() => setDiffTarget(v)}
                >
                  Diff
                </Button>
                {canManage && !isCurrent && (
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="h-5 shrink-0 gap-1 px-1.5 text-xs"
                    onClick={() => setRollbackTarget(v)}
                  >
                    <RotateCcw className="h-3 w-3" />
                    Restore
                  </Button>
                )}
              </div>
            );
          })}
        </div>
      )}

      <Dialog open={!!diffTarget} onOpenChange={(v) => !v && setDiffTarget(null)}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>
              Diff — {diffTarget?.version ?? "…"} vs current
            </DialogTitle>
          </DialogHeader>
          {diffTarget && versions[0] && (
            <div className="mt-2 max-h-[60vh] overflow-y-auto pr-1">
              <AgentContextFieldDiff
                base={diffTarget.snapshot}
                proposed={versions[0].snapshot}
                baseLabel={diffTarget.version}
                proposedLabel={`current (${versions[0].version})`}
              />
            </div>
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!rollbackTarget}
        onOpenChange={(v) => {
          if (!v && !rollback.isPending) setRollbackTarget(null);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Restore version {rollbackTarget?.version}?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            This applies the {rollbackTarget?.version} snapshot as a new version.
            History is append-only — nothing is deleted, and you can roll forward
            again.
          </p>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setRollbackTarget(null)}
              disabled={rollback.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleRollback}
              disabled={rollback.isPending}
            >
              {rollback.isPending ? "Restoring…" : "Restore"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
