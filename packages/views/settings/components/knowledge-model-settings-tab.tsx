"use client";

import { useEffect, useMemo, useState } from "react";
import { Check, KeyRound, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useCurrentMember } from "@multica/core/permissions";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  knowledgeModelSettingsOptions,
  knowledgeProviderListOptions,
  knowledgeProviderModelsOptions,
  useCreateKnowledgeProvider,
  useDeleteKnowledgeProvider,
  usePutKnowledgeModelSettings,
  useUpdateKnowledgeProvider,
} from "@multica/core/knowledge";
import type { KnowledgeProvider, KnowledgeProviderModel } from "@multica/core/types/knowledge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useT } from "../../i18n";
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
  SettingsTab,
  type SettingsSaveStatus,
} from "./settings-layout";

const NEW_PROVIDER = "new";
const DEFAULT_EMBEDDING_MODELS: Record<string, string> = {
  openai: "text-embedding-3-small",
  siliconflow: "BAAI/bge-m3",
};
const PROVIDER_PRESETS = [
  { value: "openai", label: "OpenAI", name: "OpenAI", baseURL: "https://api.openai.com/v1", protocol: "openai_compatible" },
  { value: "siliconflow", label: "SiliconFlow", name: "SiliconFlow", baseURL: "https://api.siliconflow.cn/v1", protocol: "openai_compatible" },
  { value: "custom", label: "Custom", name: "Custom provider", baseURL: "", protocol: "openai_compatible" },
] as const;

type ProviderPreset = (typeof PROVIDER_PRESETS)[number]["value"];
type ParseEnhancementMode = "off" | "text" | "vision";

function presetDefaults(value: ProviderPreset) {
  return PROVIDER_PRESETS.find((preset) => preset.value === value) ?? PROVIDER_PRESETS[0];
}

function providerTestError(
  result: { status: string; details?: Record<string, unknown> },
  fallback: string,
) {
  if (result.status === "ready") return null;
  const detail = typeof result.details?.error === "string" ? result.details.error : "";
  return detail ? `${fallback} (${detail})` : fallback;
}

export function KnowledgeModelSettingsTab() {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const currentMember = useCurrentMember(workspaceId);
  const canManage = currentMember.role === "owner" || currentMember.role === "admin";
  const settings = useQuery(knowledgeModelSettingsOptions(workspaceId));
  const providers = useInfiniteQuery(knowledgeProviderListOptions(workspaceId));
  const providerItems = providers.data?.pages.flatMap((page) => page.providers) ?? [];
  const [providerSelection, setProviderSelection] = useState(NEW_PROVIDER);
  const [preset, setPreset] = useState<ProviderPreset>("openai");
  const [providerName, setProviderName] = useState("OpenAI");
  const [baseURL, setBaseURL] = useState("https://api.openai.com/v1");
  const [protocol, setProtocol] = useState("openai_compatible");
  const [apiKey, setAPIKey] = useState("");
  const [model, setModel] = useState("");
  const [embeddingModel, setEmbeddingModel] = useState("");
  const [embeddingProvider, setEmbeddingProvider] = useState("same");
  const [embeddingDirty, setEmbeddingDirty] = useState(false);
  const [parseEnhancementMode, setParseEnhancementMode] = useState<ParseEnhancementMode>("off");
  const [rotationKey, setRotationKey] = useState("");
  const [saveStatus, setSaveStatus] = useState<SettingsSaveStatus>("idle");
  const [saveError, setSaveError] = useState("");
  const [embeddingNotice, setEmbeddingNotice] = useState("");
  const [initializedRevision, setInitializedRevision] = useState<number | null>(null);

  const createProvider = useCreateKnowledgeProvider();
  const updateProvider = useUpdateKnowledgeProvider();
  const deleteProvider = useDeleteKnowledgeProvider();
  const saveSettings = usePutKnowledgeModelSettings();

  const isNewProvider = providerSelection === NEW_PROVIDER;
  const models = useQuery(
    knowledgeProviderModelsOptions(workspaceId, isNewProvider ? "" : providerSelection),
  );
  const modelOptions = useMemo(
    () => (models.data ?? []).filter((item): item is KnowledgeProviderModel => !!item.id),
    [models.data],
  );
  const existingEmbedding = settings.data?.purposes?.embedding ?? null;

  useEffect(() => {
    const revision = settings.data?.revision;
    if (revision === undefined || revision === initializedRevision) return;
    const main = settings.data?.main;
    const embedding = settings.data?.purposes?.embedding;
    const parse = settings.data?.purposes?.parse;
    const parseMode = parse?.mode === "explicit" || parse?.mode === "auto"
      ? parse?.options?.mode === "vision" ? "vision" : "text"
      : "off";
    setProviderSelection(main?.provider_id ?? NEW_PROVIDER);
    setModel(main?.model ?? "");
    setEmbeddingModel(embedding?.model ?? "");
    setEmbeddingProvider(embedding?.provider_id ?? "same");
    setParseEnhancementMode(parseMode);
    setEmbeddingDirty(false);
    setInitializedRevision(revision);
  }, [initializedRevision, settings.data]);

  function choosePreset(value: ProviderPreset) {
    const defaults = presetDefaults(value);
    setPreset(value);
    setProviderName(defaults.name);
    setBaseURL(defaults.baseURL);
    setProtocol(defaults.protocol);
    setEmbeddingModel(DEFAULT_EMBEDDING_MODELS[value] ?? "");
    setEmbeddingProvider("same");
    setEmbeddingDirty(true);
  }

  function chooseProvider(value: string) {
    setProviderSelection(value);
    setEmbeddingNotice("");
    if (value === NEW_PROVIDER) {
      choosePreset("openai");
      setAPIKey("");
      return;
    }
    const provider = providerItems.find((item) => item.id === value);
    if (provider) {
      setEmbeddingProvider(existingEmbedding?.provider_id ?? "same");
      setEmbeddingModel(existingEmbedding?.model ?? "");
      setEmbeddingDirty(false);
    }
  }

  async function save() {
    if (!canManage || saveSettings.isPending || createProvider.isPending) return;
    setSaveStatus("saving");
    setSaveError("");
    setEmbeddingNotice("");
    try {
      if (!model.trim()) throw new Error(t(($) => $.model_required));
      if (isNewProvider && !apiKey.trim()) throw new Error(t(($) => $.api_key_required));
      if (isNewProvider && !baseURL.trim()) throw new Error(t(($) => $.base_url_required));

      const draftTest = await api.testKnowledgeProvider({
        provider_id: isNewProvider ? undefined : providerSelection,
        base_url: isNewProvider ? baseURL.trim() : undefined,
        protocol: isNewProvider ? protocol : undefined,
        api_key: isNewProvider ? apiKey : undefined,
        model: model.trim(),
        capability: "text",
      });
      const textError = providerTestError(draftTest, t(($) => $.provider_test_failed));
      if (textError) throw new Error(textError);

      let providerId = providerSelection;
      if (isNewProvider) {
        const created = await createProvider.mutateAsync({
          name: providerName.trim(),
          preset,
          protocol,
          base_url: baseURL.trim(),
          api_key: apiKey,
        });
        providerId = created.id;
        setProviderSelection(created.id);
        setAPIKey("");
      }

      if (parseEnhancementMode === "vision") {
        const visionTest = await api.testKnowledgeProvider({
          provider_id: providerId,
          model: model.trim(),
          capability: "vision",
        });
        const visionError = providerTestError(visionTest, t(($) => $.vision_test_failed));
        if (visionError) throw new Error(visionError);
      }

      const purposes: Record<string, { mode: string; provider_id?: string; model?: string; options?: Record<string, unknown> }> = {
        parse: parseEnhancementMode === "off"
          ? { mode: "off" }
          : { mode: "explicit", provider_id: providerId, model: model.trim(), options: { mode: parseEnhancementMode } },
      };
      const shouldSaveEmbedding = embeddingDirty || !existingEmbedding;
      if (shouldSaveEmbedding && embeddingModel.trim()) {
        const embeddingProviderId = embeddingProvider === "same" ? providerId : embeddingProvider;
        if (!embeddingProviderId || embeddingProviderId === NEW_PROVIDER) {
          setEmbeddingNotice(t(($) => $.embedding_provider_required));
        } else {
          const embeddingTest = await api.testKnowledgeProvider({
            provider_id: embeddingProviderId,
            model: embeddingModel.trim(),
            capability: "embedding",
          });
          if (embeddingTest.status === "ready") {
            purposes.embedding = {
              mode: "auto",
              provider_id: embeddingProviderId,
              model: embeddingModel.trim(),
            };
          } else {
            setEmbeddingNotice(t(($) => $.embedding_not_ready));
          }
        }
      } else if (embeddingDirty && !embeddingModel.trim()) {
        purposes.embedding = { mode: "off" };
      }

      await saveSettings.mutateAsync({
        expected_revision: settings.data?.revision ?? 0,
        main: { mode: "explicit", provider_id: providerId, model: model.trim() },
        ...(Object.keys(purposes).length ? { purposes } : {}),
      });
      setSaveStatus("saved");
    } catch (error) {
      const message = error instanceof Error && error.message ? error.message : t(($) => $.save_failed);
      setSaveError(message);
      setSaveStatus("error");
      toast.error(message);
    }
  }

  async function rotateProviderKey(provider: KnowledgeProvider) {
    if (!rotationKey.trim() || updateProvider.isPending) return;
    try {
      await updateProvider.mutateAsync({
        providerId: provider.id,
        data: { api_key: rotationKey, expected_revision: provider.revision },
      });
      setRotationKey("");
      toast.success(t(($) => $.key_rotated));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.save_failed));
    }
  }

  async function toggleProvider(provider: KnowledgeProvider) {
    try {
      await updateProvider.mutateAsync({
        providerId: provider.id,
        data: { is_enabled: !provider.is_enabled, expected_revision: provider.revision },
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.save_failed));
    }
  }

  async function removeProvider(provider: KnowledgeProvider) {
    if (!window.confirm(t(($) => $.delete_provider_confirm, { name: provider.name }))) return;
    try {
      await deleteProvider.mutateAsync(provider.id);
      if (providerSelection === provider.id) setProviderSelection(NEW_PROVIDER);
      toast.success(t(($) => $.provider_deleted));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.provider_delete_failed));
    }
  }

  if (settings.isPending || providers.isPending) {
    return <div className="flex items-center gap-2 text-caption text-muted-foreground" role="status"><Loader2 className="size-4 animate-spin" />{t(($) => $.loading)}</div>;
  }

  return (
    <SettingsTab
      title={t(($) => $.workspace_model_title)}
      description={t(($) => $.workspace_model_description)}
    >
      {!canManage ? (
        <p className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-caption text-muted-foreground" role="note">
          {t(($) => $.admin_only)}
        </p>
      ) : null}

      <SettingsSection
        title={t(($) => $.main_model)}
        description={t(($) => $.main_model_hint)}
        action={
          <SettingsSaveState
            status={saveStatus}
            savingLabel={t(($) => $.testing)}
            savedLabel={t(($) => $.saved)}
            errorLabel={saveError || t(($) => $.save_failed)}
          />
        }
      >
        <SettingsCard>
          <SettingsRow label={<Label htmlFor="knowledge-provider">{t(($) => $.provider)}</Label>} size="select-wide">
            <select id="knowledge-provider" className="flex h-8 w-full rounded-lg border border-input bg-background px-2.5 text-body" value={providerSelection} onChange={(event) => chooseProvider(event.target.value)} disabled={!canManage}>
              <option value={NEW_PROVIDER}>{t(($) => $.new_provider)}</option>
              {providerItems.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}
            </select>
          </SettingsRow>

          {isNewProvider ? (
            <>
              <SettingsRow label={<Label htmlFor="knowledge-preset">{t(($) => $.preset)}</Label>} size="select-wide">
                <select id="knowledge-preset" className="flex h-8 w-full rounded-lg border border-input bg-background px-2.5 text-body" value={preset} onChange={(event) => choosePreset(event.target.value as ProviderPreset)} disabled={!canManage}>
                  {PROVIDER_PRESETS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                </select>
              </SettingsRow>
              <SettingsRow label={<Label htmlFor="knowledge-provider-name">{t(($) => $.provider_name)}</Label>} size="text">
                <Input id="knowledge-provider-name" value={providerName} onChange={(event) => setProviderName(event.target.value)} disabled={!canManage} required />
              </SettingsRow>
              {preset === "custom" ? (
                <>
                  <SettingsRow label={<Label htmlFor="knowledge-base-url">{t(($) => $.base_url)}</Label>} description={t(($) => $.base_url_hint)} size="text">
                    <Input id="knowledge-base-url" type="url" value={baseURL} onChange={(event) => setBaseURL(event.target.value)} disabled={!canManage} required />
                  </SettingsRow>
                  <SettingsRow label={<Label htmlFor="knowledge-protocol">{t(($) => $.protocol)}</Label>} size="select-wide">
                    <select id="knowledge-protocol" className="flex h-8 w-full rounded-lg border border-input bg-background px-2.5 text-body" value={protocol} onChange={(event) => setProtocol(event.target.value)} disabled={!canManage}>
                      <option value="openai_compatible">{t(($) => $.openai_compatible)}</option>
                      <option value="cohere_compatible">{t(($) => $.cohere_compatible)}</option>
                    </select>
                  </SettingsRow>
                </>
              ) : null}
              <SettingsRow label={<Label htmlFor="knowledge-api-key">{t(($) => $.api_key)}</Label>} description={t(($) => $.api_key_hint)} size="text">
                <Input id="knowledge-api-key" type="password" value={apiKey} onChange={(event) => setAPIKey(event.target.value)} disabled={!canManage} autoComplete="new-password" required />
              </SettingsRow>
            </>
          ) : null}

          <SettingsRow label={<Label htmlFor="knowledge-main-model">{t(($) => $.model)}</Label>} description={t(($) => $.model_hint)} size="select-wide">
            <Input id="knowledge-main-model" list="knowledge-model-options" value={model} onChange={(event) => setModel(event.target.value)} disabled={!canManage} placeholder={t(($) => $.model_id_placeholder)} required />
            <datalist id="knowledge-model-options">{modelOptions.map((item) => <option key={item.id} value={item.id} />)}</datalist>
            {models.isFetching ? <span className="mt-1 flex items-center gap-1 text-micro text-muted-foreground"><Loader2 className="size-3 animate-spin" />{t(($) => $.model_list_loading)}</span> : null}
          </SettingsRow>

          <SettingsRow label={<Label htmlFor="knowledge-parse-enhancement">{t(($) => $.parse_enhancement)}</Label>} description={t(($) => $.parse_enhancement_hint)} size="select-wide">
            <select id="knowledge-parse-enhancement" className="flex h-8 w-full rounded-lg border border-input bg-background px-2.5 text-body" value={parseEnhancementMode} onChange={(event) => setParseEnhancementMode(event.target.value as ParseEnhancementMode)} disabled={!canManage}>
              <option value="off">{t(($) => $.enhancement_off)}</option>
              <option value="text">{t(($) => $.enhancement_text)}</option>
              <option value="vision">{t(($) => $.enhancement_vision)}</option>
            </select>
          </SettingsRow>

          <SettingsRow label={<Label htmlFor="knowledge-embedding-model">{t(($) => $.embedding_model)}</Label>} description={t(($) => $.embedding_hint)} size="select-wide" align="start">
            <div className="space-y-2">
              <select id="knowledge-embedding-provider" className="flex h-8 w-full rounded-lg border border-input bg-background px-2.5 text-body" value={embeddingProvider} onChange={(event) => { setEmbeddingProvider(event.target.value); setEmbeddingDirty(true); }} disabled={!canManage}>
                <option value="same">{t(($) => $.embedding_same_provider)}</option>
                {providerItems.filter((provider) => provider.is_enabled).map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}
              </select>
              <Input id="knowledge-embedding-model" value={embeddingModel} onChange={(event) => { setEmbeddingModel(event.target.value); setEmbeddingDirty(true); }} disabled={!canManage} placeholder={t(($) => $.embedding_optional)} />
            </div>
          </SettingsRow>

          {embeddingNotice ? <p className="px-4 py-3 text-caption text-warning" role="status">{embeddingNotice}</p> : null}
          {saveError ? <p className="px-4 py-3 text-caption text-destructive" role="alert">{saveError}</p> : null}
          <div className="flex justify-end px-4 py-3.5">
            <Button type="button" onClick={() => void save()} disabled={!canManage || saveStatus === "saving"}>
              {saveStatus === "saving" ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
              {t(($) => $.test_and_save)}
            </Button>
          </div>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection title={t(($) => $.providers)} description={t(($) => $.provider_management_hint)}>
        <SettingsCard>
          {providerItems.length === 0 ? <p className="px-4 py-4 text-caption text-muted-foreground">{t(($) => $.no_providers)}</p> : null}
          {providerItems.map((provider) => (
            <div key={provider.id} className="flex flex-col gap-3 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between">
              <div className="min-w-0">
                <div className="flex items-center gap-2 text-body font-medium"><KeyRound className="size-4 shrink-0 text-muted-foreground" />{provider.name}<span className="text-micro text-muted-foreground">{provider.is_enabled ? t(($) => $.enabled) : t(($) => $.disabled)}</span></div>
                <p className="mt-1 truncate text-micro text-muted-foreground">{provider.preset} · {provider.protocol} · {provider.has_api_key ? "••••••" : t(($) => $.no_api_key)}</p>
              </div>
              {canManage ? (
                <div className="flex flex-wrap items-center gap-2">
                  <Button type="button" variant="outline" size="sm" onClick={() => void toggleProvider(provider)} disabled={updateProvider.isPending}>
                    <RefreshCw className="size-3.5" />{provider.is_enabled ? t(($) => $.disable) : t(($) => $.enable)}
                  </Button>
                  <Input aria-label={t(($) => $.rotate_key)} type="password" value={providerSelection === provider.id ? rotationKey : ""} onChange={(event) => { setProviderSelection(provider.id); setRotationKey(event.target.value); }} placeholder={t(($) => $.rotate_key)} className="h-8 w-36" autoComplete="new-password" />
                  <Button type="button" variant="outline" size="sm" onClick={() => void rotateProviderKey(provider)} disabled={providerSelection !== provider.id || !rotationKey.trim() || updateProvider.isPending}><KeyRound className="size-3.5" />{t(($) => $.rotate)}</Button>
                  <Button type="button" variant="ghost" size="sm" onClick={() => void removeProvider(provider)} disabled={deleteProvider.isPending}><Trash2 className="size-3.5 text-destructive" />{t(($) => $.delete_provider)}</Button>
                </div>
              ) : null}
            </div>
          ))}
          {providers.hasNextPage ? <div className="px-4 pb-4"><Button type="button" variant="outline" size="sm" onClick={() => void providers.fetchNextPage()} disabled={providers.isFetchingNextPage}>{providers.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button></div> : null}
          {canManage ? <p className="flex items-center gap-1 px-4 pb-4 text-micro text-muted-foreground"><Plus className="size-3" />{t(($) => $.new_provider_hint)}</p> : null}
        </SettingsCard>
      </SettingsSection>
    </SettingsTab>
  );
}
