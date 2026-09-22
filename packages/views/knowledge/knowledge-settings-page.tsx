"use client";

import { useMemo, useState } from "react";
import { ArrowLeft, Save, Trash2 } from "lucide-react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { knowledgeBaseOptions, knowledgeModelSettingsOptions, knowledgeProviderListOptions, useDeleteKnowledgeBase, usePutKnowledgeBaseModelSettings, useUpdateKnowledgeBase } from "@multica/core/knowledge";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

const PURPOSES = ["extract", "answer", "parse", "embedding", "rerank"] as const;
type ParseEnhancementMode = "text" | "vision";

export function KnowledgeSettingsPage({ baseId }: { baseId: string }) {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const base = useQuery(knowledgeBaseOptions(workspaceId, baseId));
  const settings = useQuery(knowledgeModelSettingsOptions(workspaceId, baseId));
  const providers = useInfiniteQuery(knowledgeProviderListOptions(workspaceId));
  const providerItems = providers.data?.pages.flatMap((page) => page.providers) ?? [];
  const update = useUpdateKnowledgeBase(baseId);
  const saveModels = usePutKnowledgeBaseModelSettings(baseId);
  const remove = useDeleteKnowledgeBase();
  const [visibility, setVisibility] = useState<"private" | "workspace" | null>(null);
  const [purposeModes, setPurposeModes] = useState<Record<string, string>>({});
  const [parseEnhancementDraft, setParseEnhancementDraft] = useState<ParseEnhancementMode | null>(null);
  const [modelDrafts, setModelDrafts] = useState<Record<string, string>>({});
  const [providerDrafts, setProviderDrafts] = useState<Record<string, string>>({});
  const effectiveVisibility = base.data?.visibility === "workspace" ? "workspace" : "private";
  const currentVisibility = visibility ?? effectiveVisibility;
  const modes = useMemo(() => Object.fromEntries(PURPOSES.map((purpose) => [purpose, purposeModes[purpose] ?? settings.data?.purposes?.[purpose]?.mode ?? "inherit"])), [purposeModes, settings.data?.purposes]);
  const parseEnhancementMode: ParseEnhancementMode = parseEnhancementDraft ?? (settings.data?.purposes?.parse?.options?.mode === "vision" ? "vision" : "text");

  if (base.isPending || settings.isPending) return <div className="p-6" role="status">{t(($) => $.loading)}</div>;
  if (base.isError || !base.data) return <div className="p-6" role="alert">{base.error?.message ?? t(($) => $.error)}</div>;

  async function save() {
    try {
      const purposes: Record<string, { mode: string; provider_id?: string; model?: string; options?: Record<string, unknown> }> = Object.fromEntries(PURPOSES.map((purpose) => {
        const mode = modes[purpose] ?? "inherit";
        const providerID = providerDrafts[purpose] ?? settings.data?.purposes?.[purpose]?.provider_id ?? providerItems[0]?.id;
        const model = modelDrafts[purpose] || settings.data?.purposes?.[purpose]?.model || undefined;
        return [purpose, {
          mode,
          ...(mode === "explicit" ? { provider_id: providerID, model } : {}),
          ...(purpose === "parse" && mode === "explicit" ? { options: { mode: parseEnhancementMode } } : {}),
        }];
      }));
      const savedModels = await saveModels.mutateAsync({ expected_revision: (settings.data?.revision ?? base.data!.revision), purposes });
      await update.mutateAsync({ visibility: visibility ?? effectiveVisibility, expected_revision: savedModels.revision });
      toast.success(t(($) => $.source_added));
    } catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  async function deleteBase() {
    try { await remove.mutateAsync({ baseId, expectedRevision: base.data!.revision }); navigation.push(paths.knowledge()); }
    catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center gap-2 border-b px-4 py-3">
        <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.back)} onClick={() => navigation.push(paths.knowledgeBase(baseId))}><ArrowLeft className="size-4" /></Button>
        <div className="min-w-0"><h1 className="truncate text-title font-semibold">{base.data.name}</h1><p className="text-micro text-muted-foreground">{t(($) => $.model_settings)}</p></div>
        <div className="ml-auto flex gap-2"><Button size="sm" disabled={update.isPending || saveModels.isPending} onClick={() => void save()}><Save className="size-4" />{t(($) => $.create_submit)}</Button><Button variant="destructive" size="sm" disabled={remove.isPending} onClick={() => void deleteBase()}><Trash2 className="size-4" />{t(($) => $.delete)}</Button></div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        <div className="mx-auto max-w-3xl space-y-6">
          <section className="space-y-3 rounded-xl border bg-card p-5">
            <h2 className="text-body font-semibold">{t(($) => $.visibility)}</h2>
            <select className="flex h-9 w-full max-w-xs rounded-md border bg-background px-3 text-caption" value={currentVisibility} onChange={(event) => setVisibility(event.target.value as "private" | "workspace")}><option value="private">{t(($) => $.private)}</option><option value="workspace">{t(($) => $.workspace)}</option></select>
          </section>
          <section className="space-y-4 rounded-xl border bg-card p-5">
            <div><h2 className="text-body font-semibold">{t(($) => $.model_settings)}</h2><p className="mt-1 text-caption text-muted-foreground">{t(($) => $.subtitle)}</p></div>
            {PURPOSES.map((purpose) => <div key={purpose} className="grid gap-2 rounded-lg border p-3 md:grid-cols-[8rem_10rem_12rem_minmax(0,1fr)] md:items-end">
              <Label htmlFor={`knowledge-purpose-${purpose}`}>{purpose === "parse" ? t(($) => $.parse_enhancement) : purpose}</Label>
              <select id={`knowledge-purpose-${purpose}`} className="flex h-9 rounded-md border bg-background px-3 text-caption" value={modes[purpose]} onChange={(event) => setPurposeModes((current) => ({ ...current, [purpose]: event.target.value }))}><option value="inherit">{t(($) => $.binding_inherit)}</option><option value="off">{t(($) => $.binding_off)}</option><option value="explicit">{t(($) => $.binding_explicit)}</option></select>
              {modes[purpose] === "explicit" ? <>
                <select aria-label={t(($) => $.provider)} className="flex h-9 rounded-md border bg-background px-3 text-caption" value={providerDrafts[purpose] ?? settings.data?.purposes?.[purpose]?.provider_id ?? providerItems[0]?.id ?? ""} onChange={(event) => setProviderDrafts((current) => ({ ...current, [purpose]: event.target.value }))} disabled={!providerItems.length}><option value="" disabled>{t(($) => $.provider_placeholder)}</option>{providerItems.map((provider) => <option key={provider.id} value={provider.id}>{provider.name}</option>)}</select>
                <Input value={modelDrafts[purpose] ?? ""} onChange={(event) => setModelDrafts((current) => ({ ...current, [purpose]: event.target.value }))} placeholder={providerItems[0]?.name ?? "model"} />
                {purpose === "parse" ? <select aria-label={t(($) => $.parse_enhancement)} className="flex h-9 rounded-md border bg-background px-3 text-caption" value={parseEnhancementMode} onChange={(event) => setParseEnhancementDraft(event.target.value as ParseEnhancementMode)}><option value="text">{t(($) => $.enhancement_text)}</option><option value="vision">{t(($) => $.enhancement_vision)}</option></select> : null}
              </> : <span className="text-micro text-muted-foreground md:col-span-2">{settings.data?.purposes?.[purpose]?.model ?? t(($) => $.none)}</span>}
            </div>)}
          </section>
          {providers.hasNextPage ? <Button variant="outline" size="sm" onClick={() => void providers.fetchNextPage()} disabled={providers.isFetchingNextPage}>{providers.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button> : null}
          <AppLink href={paths.knowledgeBase(baseId)} className="text-caption text-primary underline">{t(($) => $.base_back)}</AppLink>
        </div>
      </div>
    </div>
  );
}
