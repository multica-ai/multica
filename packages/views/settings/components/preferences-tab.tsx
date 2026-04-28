"use client";

import { useEffect, useState } from "react";
import { Save } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { DEFAULT_AUTO_HIDE_DAYS } from "../../issues/utils/auto-hide";

export function PreferencesTab() {
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const qc = useQueryClient();

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const readOnly = !currentMember || currentMember.role === "member";

  const [autoHideDays, setAutoHideDays] = useState<number>(
    workspace?.settings?.auto_hide_days ?? DEFAULT_AUTO_HIDE_DAYS
  );
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setAutoHideDays(workspace?.settings?.auto_hide_days ?? DEFAULT_AUTO_HIDE_DAYS);
  }, [workspace?.settings?.auto_hide_days]);

  const handleSave = async () => {
    if (!workspace) return;
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: { ...workspace.settings, auto_hide_days: autoHideDays },
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Preferences saved");
    } catch {
      toast.error("Failed to save preferences");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Preferences</h2>
        <p className="text-sm text-muted-foreground">
          Workspace behavior settings.
        </p>
      </div>

      <Card>
        <CardContent className="pt-6 space-y-4">
          <div>
            <h3 className="text-sm font-medium mb-3">Closed issues</h3>
            <div className="space-y-2">
              <Label htmlFor="auto-hide-days">
                Hide issues closed more than (days) ago
              </Label>
              <div className="flex items-center gap-3">
                <Input
                  id="auto-hide-days"
                  type="number"
                  min={1}
                  max={365}
                  value={autoHideDays}
                  onChange={(e) => setAutoHideDays(Math.max(1, parseInt(e.target.value) || 1))}
                  className="w-24"
                  readOnly={readOnly}
                  disabled={readOnly}
                />
                <span className="text-sm text-muted-foreground">days</span>
              </div>
              <p className="text-xs text-muted-foreground">
                Issues in a terminal status (done, cancelled) closed more than this many days ago will be hidden by default in list views.
              </p>
            </div>
          </div>

          {readOnly ? (
            <p className="text-xs text-muted-foreground">
              Only workspace owners and admins can edit preferences.
            </p>
          ) : (
            <Button onClick={handleSave} disabled={saving} size="sm">
              <Save className="h-4 w-4 mr-2" />
              {saving ? "Saving..." : "Save"}
            </Button>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
