"use client";

import { useState } from "react";
import { ExternalLink, Save, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { workspaceIntegrationsOptions } from "@multica/core/integrations/queries";
import {
  useUpsertWorkspaceIntegration,
  useDeleteWorkspaceIntegration,
} from "@multica/core/integrations/mutations";

export function WorkspaceIntegrationsTab() {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: integrationsData } = useQuery(workspaceIntegrationsOptions(wsId));

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const integrations = integrationsData?.integrations ?? [];
  const redmineIntegration = integrations.find((i) => i.provider === "redmine");

  const [instanceURL, setInstanceURL] = useState(redmineIntegration?.instance_url ?? "");
  const upsert = useUpsertWorkspaceIntegration();
  const remove = useDeleteWorkspaceIntegration();

  // Sync local state when data loads
  if (redmineIntegration && instanceURL === "" && redmineIntegration.instance_url) {
    setInstanceURL(redmineIntegration.instance_url);
  }

  const handleSave = async () => {
    if (!instanceURL.trim()) {
      toast.error("Instance URL is required");
      return;
    }
    try {
      await upsert.mutateAsync({ provider: "redmine", instance_url: instanceURL.trim() });
      toast.success("Redmine integration saved");
    } catch {
      toast.error("Failed to save Redmine integration");
    }
  };

  const handleRemove = async () => {
    try {
      await remove.mutateAsync("redmine");
      setInstanceURL("");
      toast.success("Redmine integration removed");
    } catch {
      toast.error("Failed to remove Redmine integration");
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Integrations</h2>

        {/* Redmine */}
        <Card>
          <CardContent className="space-y-4">
            <div className="flex items-start justify-between">
              <div className="space-y-0.5">
                <p className="text-sm font-medium">Redmine</p>
                <p className="text-xs text-muted-foreground">
                  Link Multica projects and issues to Redmine.
                </p>
              </div>
              {redmineIntegration && (
                <a
                  href={redmineIntegration.instance_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
                >
                  <ExternalLink className="h-3 w-3" />
                  Open
                </a>
              )}
            </div>

            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground">Instance URL</label>
              <Input
                type="url"
                placeholder="https://redmine.example.com"
                value={instanceURL}
                onChange={(e) => setInstanceURL(e.target.value)}
                disabled={!canManage}
                className="text-sm"
              />
            </div>

            {canManage && (
              <div className="flex items-center justify-between pt-1">
                {redmineIntegration && (
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-destructive hover:text-destructive"
                    onClick={handleRemove}
                    disabled={remove.isPending}
                  >
                    <Trash2 className="h-3 w-3" />
                    Remove
                  </Button>
                )}
                {!redmineIntegration && <span />}
                <Button
                  size="sm"
                  onClick={handleSave}
                  disabled={upsert.isPending}
                >
                  <Save className="h-3 w-3" />
                  {upsert.isPending ? "Saving..." : "Save"}
                </Button>
              </div>
            )}

            {!canManage && (
              <p className="text-xs text-muted-foreground">
                Only admins and owners can manage integrations.
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
