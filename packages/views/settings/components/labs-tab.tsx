"use client";

import { useState } from "react";
import { GitCommitHorizontal } from "lucide-react";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { useT } from "../../i18n";

export function LabsTab() {
  const { t } = useT("settings");
  const workspace = useCurrentWorkspace();
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);

  const coAuthoredByEnabled =
    (workspace?.settings as Record<string, unknown>)?.co_authored_by_enabled !== false;

  const handleToggle = async (checked: boolean) => {
    if (!workspace || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: {
          ...((workspace.settings as Record<string, unknown>) ?? {}),
          co_authored_by_enabled: checked,
        },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t(($) => $.labs.toast_failed),
      );
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.labs.section_git)}</h2>

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
                    {t(($) => $.labs.co_authored_by_label)}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {t(($) => $.labs.co_authored_by_description_prefix)}{" "}
                    <code className="rounded bg-muted px-1 py-0.5 text-xs">
                      {"Co-authored-by: multica-agent <github@multica.ai>"}
                    </code>{" "}
                    {t(($) => $.labs.co_authored_by_description_suffix)}
                  </p>
                </div>
              </div>
              <Switch
                id="co-authored-by"
                checked={coAuthoredByEnabled}
                onCheckedChange={handleToggle}
                disabled={saving}
              />
            </div>
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
