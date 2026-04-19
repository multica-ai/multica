"use client";

import { useEffect, useMemo, useState } from "react";
import { Bell, Save } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { api } from "@multica/core/api";
import type { Workspace, WorkspaceSettings } from "@multica/core/types";

function nextWorkspaceSettings(
  settings: WorkspaceSettings | undefined,
  botToken: string,
  userId: string,
): WorkspaceSettings {
  const next: WorkspaceSettings = { ...(settings ?? {}) };
  const trimmedBotToken = botToken.trim();
  const trimmedUserID = userId.trim();

  if (!trimmedBotToken && !trimmedUserID) {
    delete next.telegram;
    return next;
  }

  next.telegram = {
    bot_token: trimmedBotToken,
    user_id: trimmedUserID,
  };
  return next;
}

export function NotificationsTab() {
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const [botToken, setBotToken] = useState(workspace?.settings.telegram?.bot_token ?? "");
  const [userId, setUserId] = useState(workspace?.settings.telegram?.user_id ?? "");
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";

  useEffect(() => {
    setBotToken(workspace?.settings.telegram?.bot_token ?? "");
    setUserId(workspace?.settings.telegram?.user_id ?? "");
  }, [workspace]);

  const hasPartialConfig = useMemo(() => {
    const hasBotToken = botToken.trim().length > 0;
    const hasUserId = userId.trim().length > 0;
    return hasBotToken !== hasUserId;
  }, [botToken, userId]);

  const handleSave = async () => {
    if (!workspace) return;
    if (hasPartialConfig) {
      toast.error("Enter both Telegram bot token and user ID, or clear both fields.");
      return;
    }

    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        settings: nextWorkspaceSettings(workspace.settings, botToken, userId),
      });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success("Telegram notifications saved");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to save Telegram notifications");
    } finally {
      setSaving(false);
    }
  };

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-center gap-2">
          <Bell className="h-4 w-4 text-muted-foreground" />
          <h2 className="text-sm font-semibold">Telegram</h2>
        </div>

        <Card>
          <CardContent className="space-y-3">
            <p className="text-xs text-muted-foreground">
              Send workspace Telegram notifications for task status transitions and every item that lands in the workspace inbox.
            </p>

            <div>
              <Label className="text-xs text-muted-foreground">Bot token</Label>
              <Input
                type="password"
                value={botToken}
                onChange={(e) => setBotToken(e.target.value)}
                disabled={!canManageWorkspace}
                className="mt-1"
                placeholder="123456:ABCDEF..."
                autoComplete="off"
              />
            </div>

            <div>
              <Label className="text-xs text-muted-foreground">User ID</Label>
              <Input
                type="text"
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                disabled={!canManageWorkspace}
                className="mt-1"
                placeholder="123456789"
              />
            </div>

            {hasPartialConfig && (
              <p className="text-xs text-destructive">
                Telegram notifications require both a bot token and a user ID.
              </p>
            )}

            <div className="flex items-center justify-end gap-2 pt-1">
              <Button
                size="sm"
                onClick={handleSave}
                disabled={!canManageWorkspace || saving || hasPartialConfig}
              >
                <Save className="h-3 w-3" />
                {saving ? "Saving..." : "Save"}
              </Button>
            </div>

            {!canManageWorkspace && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage Telegram notifications.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}