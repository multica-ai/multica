"use client";

import { useEffect, useMemo, useState } from "react";
import { BrainCircuit, Save, AlertTriangle } from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
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
type KnowledgeTypeFilter = "lesson" | "playbook" | "reference";
type RAGConfidenceThreshold = "low" | "medium" | "high";
type RAGCuratorRuntimePolicy = "workspace_default" | "cloud" | "local";

interface KnowledgeCuratorSettings {
  enabled: boolean;
  provider: string;
  model: string;
  embedding_model: string;
  runtime_mode: CuratorRuntimeMode;
  base_url: string;
  secret_ref: string;
}

interface KnowledgeRAGSettings {
  auto_inject: boolean;
  limit: number;
  type_filters: KnowledgeTypeFilter[];
  confidence_threshold: RAGConfidenceThreshold;
  curator_runtime_policy: RAGCuratorRuntimePolicy;
  token_budget: number;
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

const DEFAULT_RAG_SETTINGS: KnowledgeRAGSettings = {
  auto_inject: true,
  limit: 5,
  type_filters: [],
  confidence_threshold: "high",
  curator_runtime_policy: "workspace_default",
  token_budget: 2000,
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

function readRAGSettings(settings: Record<string, unknown> | undefined): KnowledgeRAGSettings {
  const raw = settings?.knowledge_rag;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return DEFAULT_RAG_SETTINGS;
  }
  const data = raw as Record<string, unknown>;
  const threshold = data.confidence_threshold === "low" || data.confidence_threshold === "medium" || data.confidence_threshold === "high"
    ? data.confidence_threshold
    : DEFAULT_RAG_SETTINGS.confidence_threshold;
  const runtimePolicy = data.curator_runtime_policy === "cloud" || data.curator_runtime_policy === "local" || data.curator_runtime_policy === "workspace_default"
    ? data.curator_runtime_policy
    : DEFAULT_RAG_SETTINGS.curator_runtime_policy;
  const typeFilters = Array.isArray(data.type_filters)
    ? data.type_filters.filter((value): value is KnowledgeTypeFilter =>
      value === "lesson" || value === "playbook" || value === "reference",
    )
    : DEFAULT_RAG_SETTINGS.type_filters;
  const rawLimit = typeof data.limit === "number" ? data.limit : DEFAULT_RAG_SETTINGS.limit;
  const rawTokenBudget = typeof data.token_budget === "number" ? data.token_budget : DEFAULT_RAG_SETTINGS.token_budget;
  return {
    auto_inject: data.auto_inject !== false,
    limit: Math.max(1, Math.min(8, Math.round(rawLimit))),
    type_filters: typeFilters,
    confidence_threshold: threshold,
    curator_runtime_policy: runtimePolicy,
    token_budget: Math.max(500, Math.min(8000, Math.round(rawTokenBudget))),
  };
}

function sameSettings(a: KnowledgeCuratorSettings, b: KnowledgeCuratorSettings): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function sameRAGSettings(a: KnowledgeRAGSettings, b: KnowledgeRAGSettings): boolean {
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
  const savedRAGSettings = useMemo(
    () => readRAGSettings(workspace?.settings as Record<string, unknown> | undefined),
    [workspace?.settings],
  );
  const [settings, setSettings] = useState<KnowledgeCuratorSettings>(savedSettings);
  const [ragSettings, setRAGSettings] = useState<KnowledgeRAGSettings>(savedRAGSettings);
  const [saving, setSaving] = useState(false);

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const dirty = !sameSettings(settings, savedSettings) || !sameRAGSettings(ragSettings, savedRAGSettings);
  const modelChanged =
    settings.model !== savedSettings.model ||
    settings.embedding_model !== savedSettings.embedding_model;

  useEffect(() => {
    setSettings(savedSettings);
    setRAGSettings(savedRAGSettings);
  }, [savedSettings, savedRAGSettings]);

  async function handleSave() {
    if (!workspace || !dirty || saving) return;
    setSaving(true);
    try {
      const merged = {
        ...((workspace.settings as Record<string, unknown>) ?? {}),
        knowledge_curator: settings,
        knowledge_rag: ragSettings,
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

  const toggleTypeFilter = (type: KnowledgeTypeFilter, checked: boolean) => {
    setRAGSettings((s) => ({
      ...s,
      type_filters: checked
        ? Array.from(new Set([...s.type_filters, type]))
        : s.type_filters.filter((value) => value !== type),
    }));
  };

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

          </CardContent>
        </Card>
        <Card>
          <CardContent className="space-y-5">
            <div className="flex items-start justify-between gap-4">
              <div className="space-y-1">
                <Label htmlFor="rag-auto-inject" className="text-sm font-medium">
                  {t(($) => $.curator.rag_auto_inject_label)}
                </Label>
                <p className="text-sm text-muted-foreground">
                  {t(($) => $.curator.rag_auto_inject_description)}
                </p>
              </div>
              <Switch
                id="rag-auto-inject"
                checked={ragSettings.auto_inject}
                onCheckedChange={(auto_inject) => setRAGSettings((s) => ({ ...s, auto_inject }))}
                disabled={!canManageWorkspace || saving}
              />
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.rag_limit_label)}</span>
                <Input
                  type="number"
                  min={1}
                  max={8}
                  value={ragSettings.limit}
                  onChange={(e) => setRAGSettings((s) => ({ ...s, limit: Math.max(1, Math.min(8, Number(e.target.value) || 1)) }))}
                  disabled={!canManageWorkspace || saving}
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.rag_token_budget_label)}</span>
                <Input
                  type="number"
                  min={500}
                  max={8000}
                  step={100}
                  value={ragSettings.token_budget}
                  onChange={(e) => setRAGSettings((s) => ({ ...s, token_budget: Math.max(500, Math.min(8000, Number(e.target.value) || 500)) }))}
                  disabled={!canManageWorkspace || saving}
                />
                <span className="block text-xs text-muted-foreground">
                  {t(($) => $.curator.rag_token_budget_description)}
                </span>
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.rag_confidence_label)}</span>
                <Select
                  value={ragSettings.confidence_threshold}
                  onValueChange={(confidence_threshold) =>
                    setRAGSettings((s) => ({ ...s, confidence_threshold: confidence_threshold as RAGConfidenceThreshold }))
                  }
                  disabled={!canManageWorkspace || saving}
                >
                  <SelectTrigger size="sm">
                    <SelectValue>{t(($) => $.curator.rag_confidence[ragSettings.confidence_threshold])}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="high">{t(($) => $.curator.rag_confidence.high)}</SelectItem>
                    <SelectItem value="medium">{t(($) => $.curator.rag_confidence.medium)}</SelectItem>
                    <SelectItem value="low">{t(($) => $.curator.rag_confidence.low)}</SelectItem>
                  </SelectContent>
                </Select>
              </label>
              <label className="space-y-1.5 md:col-span-2">
                <span className="text-xs font-medium">{t(($) => $.curator.rag_runtime_policy_label)}</span>
                <Select
                  value={ragSettings.curator_runtime_policy}
                  onValueChange={(curator_runtime_policy) =>
                    setRAGSettings((s) => ({ ...s, curator_runtime_policy: curator_runtime_policy as RAGCuratorRuntimePolicy }))
                  }
                  disabled={!canManageWorkspace || saving}
                >
                  <SelectTrigger size="sm">
                    <SelectValue>{t(($) => $.curator.rag_runtime_policy[ragSettings.curator_runtime_policy])}</SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="workspace_default">{t(($) => $.curator.rag_runtime_policy.workspace_default)}</SelectItem>
                    <SelectItem value="cloud">{t(($) => $.curator.rag_runtime_policy.cloud)}</SelectItem>
                    <SelectItem value="local">{t(($) => $.curator.rag_runtime_policy.local)}</SelectItem>
                  </SelectContent>
                </Select>
              </label>
              <div className="space-y-2 md:col-span-2">
                <span className="text-xs font-medium">{t(($) => $.curator.rag_type_filters_label)}</span>
                <div className="flex flex-wrap gap-3">
                  {(["lesson", "playbook", "reference"] as KnowledgeTypeFilter[]).map((type) => (
                    <label key={type} className="flex items-center gap-2 text-sm">
                      <Checkbox
                        checked={ragSettings.type_filters.includes(type)}
                        onCheckedChange={(checked) => toggleTypeFilter(type, checked === true)}
                        disabled={!canManageWorkspace || saving}
                      />
                      {t(($) => $.curator.rag_types[type])}
                    </label>
                  ))}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
        {canManageWorkspace && dirty && modelChanged && (
          <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
            <AlertTriangle className="h-4 w-4 shrink-0 mt-0.5" />
            <span>{t(($) => $.curator.rag_model_changed_hint)}</span>
          </div>
        )}
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
      </section>
    </div>
  );
}
