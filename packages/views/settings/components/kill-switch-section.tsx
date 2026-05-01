"use client";

import { useState } from "react";
import { ShieldAlert } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
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
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";

interface Props {
  workspace: Workspace;
  canManage: boolean;
}

/**
 * Fork-specific: workspace-level kill switch ("emergency stop") for runaway agents.
 * Toggle ON: cancels every queued/dispatched/running task in the workspace AND
 * blocks new task creation until the switch is released. Toggle OFF: lifts the
 * block; cancelled tasks stay cancelled.
 */
export function KillSwitchSection({ workspace, canManage }: Props) {
  const qc = useQueryClient();
  const paused = workspace.settings?.tasks_paused === true;
  const [pendingTarget, setPendingTarget] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);

  const applyToggle = async (next: boolean) => {
    setBusy(true);
    try {
      const res = await api.pauseWorkspaceTasks(workspace.id, next);
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === res.workspace.id ? res.workspace : ws)),
      );
      if (next) {
        toast.success(
          res.cancelled_count > 0
            ? `Emergency stop active — cancelled ${res.cancelled_count} task${res.cancelled_count === 1 ? "" : "s"}`
            : "Emergency stop active",
        );
      } else {
        toast.success("Emergency stop released");
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to toggle emergency stop");
    } finally {
      setBusy(false);
      setPendingTarget(null);
    }
  };

  const handleChange = (next: boolean) => {
    if (next === paused) return;
    if (next) {
      // Confirm before engaging — this cancels work in flight.
      setPendingTarget(true);
    } else {
      // Releasing the switch is non-destructive, no confirm needed.
      void applyToggle(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <ShieldAlert className="h-4 w-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">Emergency stop</h2>
      </div>

      <Card>
        <CardContent className="space-y-3">
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-1">
              <Label className="text-sm font-medium">Pause all agent tasks</Label>
              <p className="text-xs text-muted-foreground">
                Immediately cancel every running and queued task in this workspace, and prevent new agent tasks from starting until you release the switch. Use this if an agent is going off the rails.
              </p>
              {paused && (
                <p className="text-xs font-medium text-destructive">
                  Active — agent tasks are paused workspace-wide.
                </p>
              )}
            </div>
            <Switch
              checked={paused}
              disabled={!canManage || busy}
              onCheckedChange={handleChange}
              aria-label="Pause all agent tasks"
            />
          </div>
          {!canManage && (
            <p className="text-xs text-muted-foreground">
              Only admins and owners can toggle the emergency stop.
            </p>
          )}
        </CardContent>
      </Card>

      <AlertDialog
        open={pendingTarget === true}
        onOpenChange={(v) => {
          if (!v && !busy) setPendingTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Engage emergency stop?</AlertDialogTitle>
            <AlertDialogDescription>
              This cancels every queued and running agent task in this workspace right now and blocks new ones until you release the switch. Use only when an agent is misbehaving.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={busy}
              onClick={() => void applyToggle(true)}
            >
              {busy ? "Engaging…" : "Stop all tasks"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}
