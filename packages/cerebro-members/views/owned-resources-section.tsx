"use client";

// JEH-1067 (Bundle B / PR-B) — Member detail "Owned runtimes" + "Owned agents".
//
// Lists workspace runtimes / agents whose `owner_id` equals this member.
// Visible only to admins or owners. For Bundle B the actions exposed are
// delete (runtimes: hard delete; agents: archive). Ownership transfer
// is a follow-up — the picker UI isn't in scope here.

import { useMemo, useState } from "react";
import { Bot, Cpu, Trash2 } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import {
  runtimeListOptions,
  runtimeKeys,
} from "@multica/core/runtimes/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from "@multica/ui/components/ui/alert-dialog";

export interface OwnedResourcesSectionProps {
  userId: string;
  /** Whether the *viewer* (current user) is admin/owner. Non-admins don't see
   *  this section at all. */
  isAdmin: boolean;
}

export function OwnedResourcesSection({
  userId,
  isAdmin,
}: OwnedResourcesSectionProps) {
  if (!isAdmin) return null;
  return (
    <>
      <OwnedRuntimes userId={userId} />
      <OwnedAgents userId={userId} />
    </>
  );
}

function OwnedRuntimes({ userId }: { userId: string }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const owned = useMemo(
    () => runtimes.filter((r) => r.owner_id === userId),
    [runtimes, userId],
  );
  const [pendingDelete, setPendingDelete] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteRuntime(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: runtimeKeys.list(wsId) });
    },
  });

  const handleConfirm = async () => {
    if (!pendingDelete) return;
    try {
      await remove.mutateAsync(pendingDelete.id);
      toast.success(`Runtime "${pendingDelete.name}" deleted`);
      setPendingDelete(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Couldn't delete runtime",
      );
    }
  };

  return (
    <section
      className="rounded-md border border-border p-4 space-y-3"
      data-testid="owned-runtimes-section"
    >
      <h2 className="text-sm font-medium">Owned runtimes ({owned.length})</h2>
      {owned.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No runtimes owned by this user.
        </p>
      ) : (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {owned.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-3 px-3 py-2 text-sm"
              data-testid="owned-runtime-row"
            >
              <Cpu className="size-3.5 text-muted-foreground shrink-0" />
              <span className="flex-1 min-w-0">
                <span className="block truncate">
                  {r.name || r.provider || "Runtime"}
                </span>
                <span className="block text-xs text-muted-foreground truncate">
                  {r.provider}
                </span>
              </span>
              <Badge variant="outline" className="text-[10px] shrink-0">
                {r.status}
              </Badge>
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() =>
                  setPendingDelete({
                    id: r.id,
                    name: r.name || r.provider || "Runtime",
                  })
                }
                aria-label={`Delete runtime ${r.name || r.provider}`}
              >
                <Trash2 className="size-4 text-muted-foreground" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <AlertDialog
        open={pendingDelete != null}
        onOpenChange={(open) => {
          if (!open) setPendingDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete runtime "{pendingDelete?.name}"?
            </AlertDialogTitle>
            <AlertDialogDescription>
              All agents pointing at this runtime will lose their execution
              target until repointed. This can't be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={(e) => {
                e.preventDefault();
                void handleConfirm();
              }}
              disabled={remove.isPending}
            >
              {remove.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

function OwnedAgents({ userId }: { userId: string }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const owned = useMemo(
    () => agents.filter((a) => a.owner_id === userId && !a.archived_at),
    [agents, userId],
  );
  const [pendingArchive, setPendingArchive] = useState<{
    id: string;
    name: string;
  } | null>(null);

  const archive = useMutation({
    mutationFn: (id: string) => api.archiveAgent(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });

  const handleConfirm = async () => {
    if (!pendingArchive) return;
    try {
      await archive.mutateAsync(pendingArchive.id);
      toast.success(`Agent "${pendingArchive.name}" archived`);
      setPendingArchive(null);
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Couldn't archive agent",
      );
    }
  };

  return (
    <section
      className="rounded-md border border-border p-4 space-y-3"
      data-testid="owned-agents-section"
    >
      <h2 className="text-sm font-medium">Owned agents ({owned.length})</h2>
      {owned.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          No agents owned by this user.
        </p>
      ) : (
        <ul className="divide-y divide-border/50 rounded-md border border-border/50">
          {owned.map((a) => (
            <li
              key={a.id}
              className="flex items-center gap-3 px-3 py-2 text-sm"
              data-testid="owned-agent-row"
            >
              <Bot className="size-3.5 text-muted-foreground shrink-0" />
              <span className="flex-1 min-w-0">
                <span className="block truncate">{a.name}</span>
                {a.description && (
                  <span className="block truncate text-xs text-muted-foreground">
                    {a.description}
                  </span>
                )}
              </span>
              {a.visibility === "private" && (
                <Badge variant="outline" className="text-[10px] shrink-0">
                  Private
                </Badge>
              )}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() =>
                  setPendingArchive({ id: a.id, name: a.name })
                }
                aria-label={`Archive agent ${a.name}`}
              >
                <Trash2 className="size-4 text-muted-foreground" />
              </Button>
            </li>
          ))}
        </ul>
      )}

      <AlertDialog
        open={pendingArchive != null}
        onOpenChange={(open) => {
          if (!open) setPendingArchive(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Archive agent "{pendingArchive?.name}"?
            </AlertDialogTitle>
            <AlertDialogDescription>
              The agent can't be triggered after archiving. History is
              preserved.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={archive.isPending}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={(e) => {
                e.preventDefault();
                void handleConfirm();
              }}
              disabled={archive.isPending}
            >
              {archive.isPending ? "Archiving…" : "Archive"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
