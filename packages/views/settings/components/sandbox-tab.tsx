"use client";

import { Cloud, Trash2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, sandboxConfigListOptions } from "@multica/core/workspace/queries";
import { useDeleteSandboxConfig } from "@multica/core/workspace/mutations";

export function SandboxTab() {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: configs = [] } = useQuery(sandboxConfigListOptions(wsId));
  const remove = useDeleteSandboxConfig(wsId);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";

  const handleDelete = (configId: string, name: string) => {
    if (!confirm(`Delete sandbox config "${name}"? Active cloud tasks will be cancelled.`)) return;
    remove.mutate(configId, {
      onSuccess: () => toast.success(`Sandbox config "${name}" deleted`),
      onError: (e) => toast.error(e instanceof Error ? e.message : "Failed to delete"),
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Sandbox Configurations</h2>
        <p className="text-sm text-muted-foreground">
          Cloud sandbox configs for agent execution. Create new configs from the Runtimes page.
        </p>
      </div>

      {configs.length === 0 ? (
        <div className="rounded-lg border border-dashed p-6 text-center">
          <Cloud className="mx-auto h-8 w-8 text-muted-foreground/40" />
          <p className="mt-2 text-sm text-muted-foreground">
            No sandbox configurations yet.
          </p>
          <p className="text-xs text-muted-foreground">
            Go to Runtimes and click &quot;+&quot; to create a cloud runtime.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {configs.map((cfg) => (
            <div
              key={cfg.id}
              className="flex items-center justify-between rounded-lg border px-4 py-3"
            >
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{cfg.name}</span>
                  <Badge variant="outline" className="gap-1 text-xs">
                    <Cloud className="h-3 w-3" />
                    {cfg.provider.toUpperCase()}
                  </Badge>
                </div>
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <span>API Key: {cfg.provider_api_key}</span>
                  {cfg.template_id && <span>Template: {cfg.template_id}</span>}
                </div>
              </div>
              {canManage && (
                <Button
                  variant="ghost"
                  size="icon-xs"
                  onClick={() => handleDelete(cfg.id, cfg.name)}
                  disabled={remove.isPending}
                >
                  <Trash2 className="h-3.5 w-3.5 text-muted-foreground" />
                </Button>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
