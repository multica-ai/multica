"use client";

import { useEffect, useState } from "react";
import { Save, Trash2, Cloud } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, sandboxConfigOptions } from "@multica/core/workspace/queries";
import { useUpsertSandboxConfig, useDeleteSandboxConfig } from "@multica/core/workspace/mutations";
import type { SandboxProvider } from "@multica/core/types";

export function SandboxTab() {
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: config, isError } = useQuery(sandboxConfigOptions(wsId));
  const upsert = useUpsertSandboxConfig(wsId);
  const remove = useDeleteSandboxConfig(wsId);

  const [provider, setProvider] = useState<SandboxProvider>("e2b");
  const [providerApiKey, setProviderApiKey] = useState("");
  const [aiGatewayApiKey, setAiGatewayApiKey] = useState("");
  const [gitPat, setGitPat] = useState("");
  const [templateId, setTemplateId] = useState("");

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";
  const hasConfig = config && !isError;

  useEffect(() => {
    if (config) {
      setProvider(config.provider);
      setProviderApiKey(""); // Don't pre-fill encrypted keys
      setAiGatewayApiKey("");
      setGitPat("");
      setTemplateId(config.template_id ?? "");
    }
  }, [config]);

  const handleSave = () => {
    if (!providerApiKey && !hasConfig) {
      toast.error("Provider API key is required");
      return;
    }
    upsert.mutate(
      {
        provider,
        // Only send the key if user entered a new one; omit to keep existing.
        // Never send the redacted value (e.g. "****abcd") back to the server.
        provider_api_key: providerApiKey,
        ai_gateway_api_key: aiGatewayApiKey || undefined,
        git_pat: gitPat || undefined,
        template_id: templateId || undefined,
      },
      {
        onSuccess: () => toast.success("Sandbox configuration saved"),
        onError: (e) => toast.error(e instanceof Error ? e.message : "Failed to save"),
      }
    );
  };

  const handleDelete = () => {
    if (!confirm("Delete sandbox configuration? Active cloud tasks will be cancelled.")) return;
    remove.mutate(undefined, {
      onSuccess: () => {
        toast.success("Sandbox configuration deleted");
        setProviderApiKey("");
        setAiGatewayApiKey("");
        setGitPat("");
        setTemplateId("");
      },
      onError: (e) => toast.error(e instanceof Error ? e.message : "Failed to delete"),
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-lg font-semibold">Sandbox</h2>
        <p className="text-sm text-muted-foreground">
          Configure a sandbox provider for cloud agent execution. Cloud agents run in remote isolated environments.
        </p>
      </div>

      {hasConfig && (
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="gap-1">
            <Cloud className="h-3 w-3" />
            {config.provider.toUpperCase()}
          </Badge>
          <Badge variant="secondary">
            {config.provider_api_key}
          </Badge>
        </div>
      )}

      <div className="space-y-4">
        <div className="space-y-2">
          <label className="text-sm font-medium">Provider</label>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value as SandboxProvider)}
            disabled={!canManage}
            className="flex h-9 w-full max-w-xs rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          >
            <option value="e2b">E2B</option>
            <option value="daytona" disabled>Daytona (coming soon)</option>
          </select>
        </div>

        <div className="space-y-2">
          <label className="text-sm font-medium">Provider API Key *</label>
          <Input
            type="password"
            value={providerApiKey}
            onChange={(e) => setProviderApiKey(e.target.value)}
            placeholder={hasConfig ? "Leave blank to keep current key" : "sk-e2b-..."}
            disabled={!canManage}
            className="max-w-md"
          />
        </div>

        <div className="space-y-2">
          <label className="text-sm font-medium">AI Gateway API Key</label>
          <Input
            type="password"
            value={aiGatewayApiKey}
            onChange={(e) => setAiGatewayApiKey(e.target.value)}
            placeholder={hasConfig && config.ai_gateway_api_key ? "Leave blank to keep current key" : "Optional — for model access in sandbox"}
            disabled={!canManage}
            className="max-w-md"
          />
          <p className="text-xs text-muted-foreground">Used by OpenCode inside the sandbox to call AI models.</p>
        </div>

        <div className="space-y-2">
          <label className="text-sm font-medium">Git Personal Access Token</label>
          <Input
            type="password"
            value={gitPat}
            onChange={(e) => setGitPat(e.target.value)}
            placeholder={hasConfig && config.git_pat ? "Leave blank to keep current key" : "Optional — for git push + PR creation"}
            disabled={!canManage}
            className="max-w-md"
          />
          <p className="text-xs text-muted-foreground">Enables cloud agents to push code and create pull requests. Requires repo write scope.</p>
        </div>

        <div className="space-y-2">
          <label className="text-sm font-medium">Template ID</label>
          <Input
            value={templateId}
            onChange={(e) => setTemplateId(e.target.value)}
            placeholder="Optional — provider-specific sandbox template"
            disabled={!canManage}
            className="max-w-md"
          />
        </div>
      </div>

      {canManage && (
        <div className="flex items-center gap-2">
          <Button onClick={handleSave} disabled={upsert.isPending}>
            <Save className="h-4 w-4 mr-1" />
            {upsert.isPending ? "Saving..." : "Save"}
          </Button>
          {hasConfig && (
            <Button variant="destructive" onClick={handleDelete} disabled={remove.isPending}>
              <Trash2 className="h-4 w-4 mr-1" />
              {remove.isPending ? "Deleting..." : "Delete"}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
