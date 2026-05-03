"use client";

import { useState } from "react";
import { GitCommitHorizontal, Database, Cloud } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";

export function LabsTab() {
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const coAuthoredByEnabled =
    (workspace?.settings as Record<string, unknown>)?.co_authored_by_enabled !== false;

  const diffSnapshotMode =
    ((workspace?.settings as Record<string, unknown>)?.diff_snapshot_mode as string) !== "dynamic";

  const handleToggle = async (setting: string, checked: boolean) => {
    if (!workspace || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: {
          ...((workspace.settings as Record<string, unknown>) ?? {}),
          [setting]: checked,
        },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : "Failed to update setting",
      );
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Git</h2>

        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <GitCommitHorizontal className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label
                    htmlFor="co-authored-by"
                    className="text-sm font-medium"
                  >
                    Co-authored-by trailer
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    Automatically add{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-xs">
                      Co-authored-by: multica-agent &lt;github@multica.ai&gt;
                    </code>{" "}
                    to commits made by agents.
                  </p>
                </div>
              </div>
              <Switch
                id="co-authored-by"
                checked={coAuthoredByEnabled}
                onCheckedChange={(checked) => handleToggle("co_authored_by_enabled", checked)}
                disabled={saving}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent>
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  {diffSnapshotMode ? (
                    <Database className="h-4 w-4" />
                  ) : (
                    <Cloud className="h-4 w-4" />
                  )}
                </div>
                <div className="space-y-1">
                  <Label
                    htmlFor="diff-snapshot"
                    className="text-sm font-medium"
                  >
                    Snapshot diffs at commit time
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {diffSnapshotMode
                      ? "Store full diffs in the database. Diffs remain viewable even if the branch is deleted."
                      : "Fetch diffs on-demand from the repository. Saves database storage but diffs are lost if the branch is removed."}
                  </p>
                </div>
              </div>
              <Switch
                id="diff-snapshot"
                checked={diffSnapshotMode}
                onCheckedChange={(checked) => handleToggle("diff_snapshot_mode", checked)}
                disabled={saving}
              />
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
