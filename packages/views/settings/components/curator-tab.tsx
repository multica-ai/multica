"use client";

import { useEffect, useMemo, useState } from "react";
import { BrainCircuit, Save } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { Workspace } from "@multica/core/types";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { useT } from "../../i18n";

type CuratorRuntimeMode = "local" | "cloud" | "external";

interface KnowledgeCuratorSettings {
  enabled: boolean;
  provider: string;
  model: string;
  embedding_model: string;
  runtime_mode: CuratorRuntimeMode;
  base_url: string;
  secret_ref: string;
}

const DEFAULT_CURATOR_SETTINGS: KnowledgeCuratorSettings = {
  enabled: false,
  provider: "",
  model: "",
  embedding_model: "",
  runtime_mode: "external",
  base_url: "",
  secret_ref: "",
};

function readCuratorSettings(settings: Record<string, unknown> | undefined): KnowledgeCuratorSettings {
  const raw = settings?.knowledge_curator;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return DEFAULT_CURATOR_SETTINGS;
  }
  const data = raw as Record<string, unknown>;
  const runtimeMode = data.runtime_mode === "local" || data.runtime_mode === "cloud" || data.runtime_mode === "external"
    ? data.runtime_mode
    : DEFAULT_CURATOR_SETTINGS.runtime_mode;
  return {
    enabled: data.enabled === true,
    provider: typeof data.provider === "string" ? data.provider : "",
    model: typeof data.model === "string" ? data.model : "",
    embedding_model: typeof data.embedding_model === "string" ? data.embedding_model : "",
    runtime_mode: runtimeMode,
    base_url: typeof data.base_url === "string" ? data.base_url : "",
    secret_ref: typeof data.secret_ref === "string" ? data.secret_ref : "",
  };
}

function sameSettings(a: KnowledgeCuratorSettings, b: KnowledgeCuratorSettings): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

export function CuratorTab() {
  const { t } = useT("settings");
  const user = useAuthStore((s) => s.user);
  const workspace = useCurrentWorkspace();
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const savedSettings = useMemo(
    () => readCuratorSettings(workspace?.settings as Record<string, unknown> | undefined),
    [workspace?.settings],
  );
  const [settings, setSettings] = useState<KnowledgeCuratorSettings>(savedSettings);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const dirty = !sameSettings(settings, savedSettings);

  useEffect(() => {
    setSettings(savedSettings);
  }, [savedSettings]);

  async function handleSave() {
    if (!workspace || !dirty || saving) return;
    setSaving(true);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        knowledge_curator: settings,
      };
      const updated = await api.updateWorkspace(workspace.id, { settings: merged });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.curator.toast_saved));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.curator.toast_save_failed));
    } finally {
      setSaving(false);
    }
  }

  if (!workspace) return null;

  return (
    <div className="space-y-8">
      <section className="space-y-1">
        <p className="text-sm text-muted-foreground">
          {t(($) => $.curator.page_description)}
        </p>
      </section>

      <section className="space-y-3">
        <Card>
          <CardContent className="space-y-5">
            <div className="flex items-start justify-between gap-4">
              <div className="flex items-start gap-3">
                <div className="rounded-md border bg-muted/50 p-2 text-muted-foreground">
                  <BrainCircuit className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="curator-enabled" className="text-sm font-medium">
                    {t(($) => $.curator.enabled_label)}
                  </Label>
                  <p className="text-sm text-muted-foreground">
                    {t(($) => $.curator.enabled_description)}
                  </p>
                </div>
              </div>
              <Switch
                id="curator-enabled"
                checked={settings.enabled}
                onCheckedChange={(enabled) => setSettings((s) => ({ ...s, enabled }))}
                disabled={!canManageWorkspace || saving}
              />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.provider_label)}</span>
                <Input
                  value={settings.provider}
                  onChange={(e) => setSettings((s) => ({ ...s, provider: e.target.value }))}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.provider_placeholder)}
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.runtime_mode_label)}</span>
                <Select
                  value={settings.runtime_mode}
                  onValueChange={(runtime_mode) =>
                    setSettings((s) => ({ ...s, runtime_mode: runtime_mode as CuratorRuntimeMode }))
                  }
                  disabled={!canManageWorkspace || saving}
                >
                  <SelectTrigger size="sm">
                    <SelectValue>{t(($) => $.curator.runtime_modes[settings.runtime_mode])}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="external">{t(($) => $.curator.runtime_modes.external)}</SelectItem>
                    <SelectItem value="cloud">{t(($) => $.curator.runtime_modes.cloud)}</SelectItem>
                    <SelectItem value="local">{t(($) => $.curator.runtime_modes.local)}</SelectItem>
                  </SelectContent>
                </Select>
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.model_label)}</span>
                <Input
                  value={settings.model}
                  onChange={(e) => setSettings((s) => ({ ...s, model: e.target.value }))}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.model_placeholder)}
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.embedding_model_label)}</span>
                <Input
                  value={settings.embedding_model}
                  onChange={(e) => setSettings((s) => ({ ...s, embedding_model: e.target.value }))}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.embedding_model_placeholder)}
                />
              </label>
              <label className="space-y-1.5 md:col-span-2">
                <span className="text-xs font-medium">{t(($) => $.curator.base_url_label)}</span>
                <Input
                  value={settings.base_url}
                  onChange={(e) => setSettings((s) => ({ ...s, base_url: e.target.value }))}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.base_url_placeholder)}
                />
              </label>
              <label className="space-y-1.5 md:col-span-2">
                <span className="text-xs font-medium">{t(($) => $.curator.secret_ref_label)}</span>
                <Input
                  value={settings.secret_ref}
                  onChange={(e) => setSettings((s) => ({ ...s, secret_ref: e.target.value }))}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.secret_ref_placeholder)}
                />
                <span className="block text-xs text-muted-foreground">
                  {t(($) => $.curator.secret_ref_hint)}
                </span>
              </label>
            </div>

            {canManageWorkspace ? (
              <div className="flex justify-end">
                <Button size="sm" onClick={handleSave} disabled={!dirty || saving}>
                  <Save className="h-3 w-3" />
                  {saving ? t(($) => $.curator.saving) : t(($) => $.curator.save)}
                </Button>
              </div>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t(($) => $.curator.manage_hint)}
              </p>
            )}
          </CardContent>
        </Card>
      </section>
    </div>
  );
}
