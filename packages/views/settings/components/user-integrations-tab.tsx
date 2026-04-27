"use client";

import { useState } from "react";
import { KeyRound, Save, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceIntegrationsOptions, myCredentialOptions } from "@multica/core/integrations/queries";
import { useUpsertMyCredential, useDeleteMyCredential } from "@multica/core/integrations/mutations";
import type { IntegrationProvider } from "@multica/core/types";

function CredentialRow({ provider, label }: { provider: IntegrationProvider; label: string }) {
  const wsId = useWorkspaceId();
  const { data: credential } = useQuery(myCredentialOptions(wsId, provider));
  const [apiKey, setApiKey] = useState("");
  const upsert = useUpsertMyCredential();
  const remove = useDeleteMyCredential();

  const handleSave = async () => {
    if (!apiKey.trim()) {
      toast.error("API key is required");
      return;
    }
    try {
      await upsert.mutateAsync({ provider, apiKey: apiKey.trim() });
      setApiKey("");
      toast.success(`${label} API key saved`);
    } catch {
      toast.error(`Failed to save ${label} API key`);
    }
  };

  const handleRemove = async () => {
    try {
      await remove.mutateAsync(provider);
      toast.success(`${label} API key removed`);
    } catch {
      toast.error(`Failed to remove ${label} API key`);
    }
  };

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="flex items-center gap-2">
          <KeyRound className="h-4 w-4 text-muted-foreground" />
          <p className="text-sm font-medium">{label}</p>
          {credential?.has_key && (
            <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs text-emerald-600">
              Key saved
            </span>
          )}
        </div>

        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">
            {credential?.has_key ? "Replace API key" : "API key"}
          </label>
          <Input
            type="password"
            placeholder={credential?.has_key ? "Enter new key to replace" : "Your API key"}
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            className="text-sm font-mono"
          />
        </div>

        <div className="flex items-center justify-between">
          {credential?.has_key && (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={handleRemove}
              disabled={remove.isPending}
            >
              <Trash2 className="h-3 w-3" />
              Remove key
            </Button>
          )}
          {!credential?.has_key && <span />}
          <Button
            size="sm"
            onClick={handleSave}
            disabled={upsert.isPending || !apiKey.trim()}
          >
            <Save className="h-3 w-3" />
            {upsert.isPending ? "Saving..." : "Save"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

export function UserIntegrationsTab() {
  const wsId = useWorkspaceId();
  const { data: integrationsData } = useQuery(workspaceIntegrationsOptions(wsId));
  const integrations = integrationsData?.integrations ?? [];

  if (integrations.length === 0) {
    return (
      <div className="space-y-4">
        <h2 className="text-sm font-semibold">Integrations</h2>
        <p className="text-sm text-muted-foreground">
          No integrations are configured for this workspace yet. Ask an admin to configure one first.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">Integration API keys</h2>
        <p className="text-xs text-muted-foreground">
          Your personal API keys for each enabled integration. These are only used for your account.
        </p>

        {integrations.map((integration) => (
          <CredentialRow
            key={integration.provider}
            provider={integration.provider}
            label={integration.provider === "redmine" ? "Redmine" : integration.provider}
          />
        ))}
      </section>
    </div>
  );
}
