"use client";

import { useState } from "react";
import { ExternalLink, KeyRound, Save, Trash2 } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceIntegrationsOptions, myCredentialOptions } from "@multica/core/integrations/queries";
import { useUpsertMyCredential, useDeleteMyCredential } from "@multica/core/integrations/mutations";
import type { IntegrationProvider } from "@multica/core/types";
import { useT } from "../../i18n";

function CredentialRow({ provider, label, instanceUrl }: { provider: IntegrationProvider; label: string; instanceUrl?: string }) {
  const wsId = useWorkspaceId();
  const { data: credential } = useQuery(myCredentialOptions(wsId, provider));
  const [apiKey, setApiKey] = useState("");
  const upsert = useUpsertMyCredential();
  const remove = useDeleteMyCredential();
  const { t } = useT("integrations");

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
              {t($ => $.key_saved)}
            </span>
          )}
        </div>

        <div className="space-y-1.5">
          <label className="text-xs text-muted-foreground">
            {credential?.has_key ? "Replace API key" : "API key"}
          </label>
          {instanceUrl && (
            <div className="rounded-md border border-border bg-muted/40 px-3 py-2 space-y-1.5">
              <p
                className="text-xs text-muted-foreground"
                dangerouslySetInnerHTML={{ __html: t($ => $.redmine_api_key_guide) }}
              />
              <a
                href={`${instanceUrl.replace(/\/$/, "")}/my/account`}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 text-xs text-primary hover:underline"
              >
                <ExternalLink className="h-3 w-3" />
                {t($ => $.redmine_open_profile)}
              </a>
            </div>
          )}
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
              {t($ => $.remove_key)}
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
  const { t } = useT("integrations");

  if (integrations.length === 0) {
    return (
      <div className="space-y-4">
        <h2 className="text-sm font-semibold">{t($ => $.title)}</h2>
        <p className="text-sm text-muted-foreground">
          {t($ => $.no_integrations)}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div>
          <h2 className="text-sm font-semibold">{t($ => $.api_keys_title)}</h2>
          <p className="text-sm text-muted-foreground mt-1">
            {t($ => $.api_keys_desc)}
          </p>
        </div>

        {integrations.map((integration) => (
          <CredentialRow
            key={integration.provider}
            provider={integration.provider}
            label={integration.provider === "redmine" ? "Redmine" : integration.provider}
            instanceUrl={integration.instance_url}
          />
        ))}
      </section>
    </div>
  );
}
