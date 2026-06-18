"use client";

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  BrainCircuit,
  CheckCircle2,
  CircleHelp,
  Loader2,
  Save,
  WandSparkles,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import type { ProbeKnowledgeCuratorResponse } from "@multica/core/knowledge/types";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { useT } from "../../i18n";

type KnowledgeTypeFilter = "lesson" | "playbook" | "reference";
type RAGConfidenceThreshold = "low" | "medium" | "high";
type CuratorProbeState =
  | { status: "idle" }
  | { status: "loading" }
  | { status: "success"; result: ProbeKnowledgeCuratorResponse }
  | { status: "warning"; result: ProbeKnowledgeCuratorResponse }
  | { status: "error"; message: string };

interface KnowledgeCuratorSettings {
  enabled: boolean;
  provider: string;
  model: string;
  embedding_model: string;
  base_url: string;
  secret_ref?: string;
}

interface KnowledgeRAGSettings {
  auto_inject: boolean;
  limit: number;
  type_filters: KnowledgeTypeFilter[];
  confidence_threshold: RAGConfidenceThreshold;
  token_budget: number;
}

const DEFAULT_CURATOR_SETTINGS: KnowledgeCuratorSettings = {
  enabled: false,
  provider: "",
  model: "",
  embedding_model: "",
  base_url: "",
};

const DEFAULT_RAG_SETTINGS: KnowledgeRAGSettings = {
  auto_inject: true,
  limit: 5,
  type_filters: [],
  confidence_threshold: "high",
  token_budget: 2000,
};

function readCuratorSettings(settings: Record<string, unknown> | undefined): KnowledgeCuratorSettings {
  const raw = settings?.knowledge_curator;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    return DEFAULT_CURATOR_SETTINGS;
  }
  const data = raw as Record<string, unknown>;
  return {
    enabled: data.enabled === true,
    provider: typeof data.provider === "string" ? data.provider : "",
    model: typeof data.model === "string" ? data.model : "",
    embedding_model: typeof data.embedding_model === "string" ? data.embedding_model : "",
    base_url: typeof data.base_url === "string" ? data.base_url : "",
    ...(typeof data.secret_ref === "string" ? { secret_ref: data.secret_ref } : {}),
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
    token_budget: Math.max(500, Math.min(8000, Math.round(rawTokenBudget))),
  };
}

function sameSettings(a: KnowledgeCuratorSettings, b: KnowledgeCuratorSettings): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function sameRAGSettings(a: KnowledgeRAGSettings, b: KnowledgeRAGSettings): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function CuratorProbeNotice({
  state,
  t,
  onUseRecommendation,
}: {
  state: CuratorProbeState;
  t: ReturnType<typeof useT<"settings">>["t"];
  onUseRecommendation: () => void;
}) {
  if (state.status === "idle") return null;
  if (state.status === "loading") {
    return (
      <div className="flex items-center gap-2 rounded-md border bg-muted/40 p-3 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span>{t(($) => $.curator.probe_loading)}</span>
      </div>
    );
  }
  if (state.status === "error") {
    return (
      <div className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
        <span>{state.message}</span>
      </div>
    );
  }
  const warning = state.status === "warning";
  return (
    <div
      className={`space-y-2 rounded-md border p-3 text-sm ${
        warning
          ? "border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100"
          : "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-100"
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-start gap-2">
          {warning ? <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /> : <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />}
          <div className="space-y-1">
            <p className="font-medium">
              {warning ? t(($) => $.curator.probe_warning) : t(($) => $.curator.probe_success)}
            </p>
            <p>
              {t(($) => $.curator.probe_recommendation, {
                provider: state.result.provider,
                model: state.result.model || "-",
                embeddingModel: state.result.embedding_model || "-",
              })}
            </p>
          </div>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onUseRecommendation}>
          <WandSparkles className="h-3.5 w-3.5" />
          {t(($) => $.curator.probe_use_recommendation)}
        </Button>
      </div>
      {state.result.warnings.length > 0 && (
        <ul className="space-y-1 pl-6 text-xs list-disc">
          {state.result.warnings.map((warningText) => (
            <li key={warningText}>{warningText}</li>
          ))}
        </ul>
      )}
    </div>
  );
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
  const [probeState, setProbeState] = useState<CuratorProbeState>({ status: "idle" });
  const [typeHelpOpen, setTypeHelpOpen] = useState(false);
  const [manualFields, setManualFields] = useState({
    provider: false,
    model: false,
    embedding_model: false,
  });

  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canManageWorkspace = currentMember?.role === "owner" || currentMember?.role === "admin";
  const dirty = !sameSettings(settings, savedSettings) || !sameRAGSettings(ragSettings, savedRAGSettings);
  const modelChanged =
    settings.model !== savedSettings.model ||
    settings.embedding_model !== savedSettings.embedding_model;

  useEffect(() => {
    setSettings(savedSettings);
    setRAGSettings(savedRAGSettings);
    setProbeState({ status: "idle" });
    setManualFields({ provider: false, model: false, embedding_model: false });
  }, [savedSettings, savedRAGSettings]);

  function applyProbeResult(result: ProbeKnowledgeCuratorResponse, force: boolean) {
    setSettings((current) => ({
      ...current,
      provider: (force || !manualFields.provider) && result.provider ? result.provider : current.provider,
      model: (force || !manualFields.model) && result.model ? result.model : current.model,
      embedding_model: (force || !manualFields.embedding_model) && result.embedding_model
        ? result.embedding_model
        : current.embedding_model,
    }));
  }

  async function runProbe(forceApply = false) {
    const baseURL = settings.base_url.trim();
    if (!baseURL || probeState.status === "loading") return;
    setProbeState({ status: "loading" });
    try {
      const result = await api.probeKnowledgeCurator({
        base_url: baseURL,
        model: settings.model,
        embedding_model: settings.embedding_model,
      });
      setProbeState(result.warnings.length > 0 ? { status: "warning", result } : { status: "success", result });
      applyProbeResult(result, forceApply);
    } catch (e) {
      setProbeState({ status: "error", message: e instanceof Error ? e.message : t(($) => $.curator.probe_failed) });
    }
  }

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
                  onChange={(e) => {
                    setManualFields((s) => ({ ...s, provider: true }));
                    setSettings((s) => ({ ...s, provider: e.target.value }));
                  }}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.provider_placeholder)}
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.model_label)}</span>
                <Input
                  value={settings.model}
                  onChange={(e) => {
                    setManualFields((s) => ({ ...s, model: true }));
                    setSettings((s) => ({ ...s, model: e.target.value }));
                  }}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.model_placeholder)}
                />
              </label>
              <label className="space-y-1.5">
                <span className="text-xs font-medium">{t(($) => $.curator.embedding_model_label)}</span>
                <Input
                  value={settings.embedding_model}
                  onChange={(e) => {
                    setManualFields((s) => ({ ...s, embedding_model: true }));
                    setSettings((s) => ({ ...s, embedding_model: e.target.value }));
                  }}
                  disabled={!canManageWorkspace || saving}
                  placeholder={t(($) => $.curator.embedding_model_placeholder)}
                />
              </label>
              <label className="space-y-1.5 md:col-span-2">
                <span className="text-xs font-medium">{t(($) => $.curator.base_url_label)}</span>
                <Input
                  value={settings.base_url}
                  onChange={(e) => {
                    setProbeState({ status: "idle" });
                    setSettings((s) => ({ ...s, base_url: e.target.value }));
                  }}
                  onBlur={() => void runProbe(false)}
                  disabled={!canManageWorkspace || saving || probeState.status === "loading"}
                  placeholder={t(($) => $.curator.base_url_placeholder)}
                />
              </label>
            </div>
            {canManageWorkspace && (
              <CuratorProbeNotice
                state={probeState}
                t={t}
                onUseRecommendation={() => {
                  if ("result" in probeState) {
                    applyProbeResult(probeState.result, true);
                  }
                }}
              />
            )}
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
              <div className="space-y-2 md:col-span-2">
                <div className="flex items-center gap-1.5">
                  <span className="text-xs font-medium">{t(($) => $.curator.rag_type_filters_label)}</span>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => setTypeHelpOpen(true)}
                    disabled={saving}
                    aria-label={t(($) => $.curator.rag_type_help_open)}
                  >
                    <CircleHelp className="h-3.5 w-3.5" />
                  </Button>
                </div>
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
      <Dialog open={typeHelpOpen} onOpenChange={setTypeHelpOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(($) => $.curator.rag_type_help_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.curator.rag_type_help_description)}</DialogDescription>
          </DialogHeader>
          <div className="space-y-3 text-sm">
            {(["lesson", "playbook", "reference"] as KnowledgeTypeFilter[]).map((type) => (
              <div key={type} className="space-y-1">
                <p className="font-medium">{t(($) => $.curator.rag_types[type])}</p>
                <p className="text-muted-foreground">{t(($) => $.curator.rag_type_help[type])}</p>
              </div>
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
