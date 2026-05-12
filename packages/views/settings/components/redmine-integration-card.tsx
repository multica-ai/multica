"use client";

import { useState } from "react";
import { ExternalLink, Save, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { workspaceIntegrationsOptions } from "@multica/core/integrations/queries";
import {
  useUpsertWorkspaceIntegration,
  useDeleteWorkspaceIntegration,
} from "@multica/core/integrations/mutations";
import { useT } from "../../i18n";

export function RedmineIntegrationCard() {
  const { t } = useT("integrations");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: integrationsData } = useQuery(
    workspaceIntegrationsOptions(wsId),
  );

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage =
    currentMember?.role === "owner" || currentMember?.role === "admin";

  const redmineIntegration = (integrationsData?.integrations ?? []).find(
    (i) => i.provider === "redmine",
  );

  const [instanceURL, setInstanceURL] = useState("");
  const upsert = useUpsertWorkspaceIntegration();
  const remove = useDeleteWorkspaceIntegration();

  if (
    redmineIntegration &&
    instanceURL === "" &&
    redmineIntegration.instance_url
  ) {
    setInstanceURL(redmineIntegration.instance_url);
  }

  const handleSave = async () => {
    if (!instanceURL.trim()) {
      toast.error("Instance URL is required");
      return;
    }
    try {
      await upsert.mutateAsync({
        provider: "redmine",
        instance_url: instanceURL.trim(),
      });
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
    <Card>
      <CardContent className="space-y-4">
        <div className="flex items-start justify-between">
          <div className="space-y-0.5">
            <p className="text-sm font-medium">{t(($) => $.redmine_name)}</p>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.redmine_desc)}
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
              {t(($) => $.open)}
            </a>
          )}
        </div>

        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">
            {t(($) => $.instance_url)}
          </label>
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
                {t(($) => $.remove)}
              </Button>
            )}
            {!redmineIntegration && <span />}
            <Button size="sm" onClick={handleSave} disabled={upsert.isPending}>
              <Save className="h-3 w-3" />
              {upsert.isPending ? t(($) => $.saving) : t(($) => $.save)}
            </Button>
          </div>
        )}

        {!canManage && (
          <p className="text-xs text-muted-foreground">
            {t(($) => $.admin_only)}
          </p>
        )}
      </CardContent>
    </Card>
  );
}
