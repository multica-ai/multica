"use client";

import { useEffect, useMemo, useState } from "react";
import {
  Bot,
  ChevronLeft,
  ChevronRight,
  LayoutTemplate,
  Loader2,
  Plus,
  Search,
  ShieldAlert,
  Sparkles,
  Users,
} from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import {
  marketplaceTemplateDetailOptions,
  marketplaceTemplateKeys,
  marketplaceTemplateListOptions,
} from "@multica/core/templates";
import {
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import type {
  MarketplaceTemplate,
  MarketplaceTemplateScope,
  MarketplaceTemplateSort,
  MarketplaceTemplateSourceType,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader } from "@multica/ui/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
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
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { RuntimePicker } from "../../agents/components/runtime-picker";
import { CollectionPageHeader, CollectionPageHeaderAction, CollectionPageState } from "../../layout/collection-page";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { CreateTemplateDialog } from "./create-template-dialog";

const PAGE_SIZE = 12;

function TemplateCard({
  template,
  onUse,
}: {
  template: MarketplaceTemplate;
  onUse: (template: MarketplaceTemplate) => void;
}) {
  const { t } = useT("templates");
  const p = useWorkspacePaths();
  const TypeIcon = template.source_type === "squad" ? Users : Bot;
  const visibilityCopy = template.visibility === "public"
    ? t(($) => $.card.public)
    : template.visibility === "workspace"
      ? t(($) => $.card.workspace)
      : t(($) => $.card.private);

  return (
    <Card className="group flex min-h-72 flex-col overflow-hidden transition-colors hover:border-foreground/20">
      <CardHeader className="gap-3 pb-2">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-brand/10 text-brand">
              <TypeIcon className="size-5" aria-hidden="true" />
            </div>
            <div className="min-w-0">
              <AppLink
                href={p.templateDetail(template.id)}
                className="block truncate text-body font-medium hover:underline"
              >
                {template.name}
              </AppLink>
              <p className="mt-0.5 truncate text-caption text-muted-foreground">
                {t(($) => $.card.creator, { name: template.creator_name })}
              </p>
            </div>
          </div>
          {template.featured_at ? (
            <Badge variant="secondary" className="shrink-0 gap-1">
              <Sparkles className="size-3" />
              {t(($) => $.card.featured)}
            </Badge>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline">
            {template.source_type === "squad" ? t(($) => $.card.squad) : t(($) => $.card.agent)}
          </Badge>
          <Badge variant="outline">{visibilityCopy}</Badge>
          {template.tags.slice(0, 3).map((tag) => (
            <Badge key={tag} variant="secondary">{tag}</Badge>
          ))}
        </div>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col gap-4 pt-2">
        <p className="line-clamp-3 text-body text-muted-foreground">
          {template.description}
        </p>
        <div className="mt-auto flex flex-wrap gap-2 text-caption text-muted-foreground">
          <span>{t(($) => $.card.agents, { count: template.agent_count })}</span>
          <span>·</span>
          <span>{t(($) => $.card.skills, { count: template.skill_count })}</span>
          <span>·</span>
          <span>{t(($) => $.card.applied, { count: template.applied_count })}</span>
        </div>
        {template.preview_agents.length > 0 ? (
          <div className="flex flex-wrap gap-1.5">
            {template.preview_agents.slice(0, 4).map((agent) => (
              <span key={agent.key} className="max-w-full truncate rounded-md bg-muted px-2 py-1 text-caption">
                {agent.name}{agent.role ? ` · ${agent.role}` : ""}
              </span>
            ))}
          </div>
        ) : null}
      </CardContent>
      <CardFooter className="justify-end border-t pt-4">
        <Button size="sm" onClick={() => onUse(template)}>
          {t(($) => $.card.use)}
        </Button>
      </CardFooter>
    </Card>
  );
}

export function ApplyTemplateDialog({
  templateId,
  onOpenChange,
  wsId,
}: {
  templateId: string | null;
  onOpenChange: (open: boolean) => void;
  wsId: string;
}) {
  const { t } = useT("templates");
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const queryClient = useQueryClient();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const { data: template, isPending } = useQuery(marketplaceTemplateDetailOptions(wsId, templateId ?? ""));
  const { data: runtimes = [], isPending: runtimesLoading } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [runtimeIds, setRuntimeIds] = useState<Record<string, string>>({});
  const [bulkRuntimeId, setBulkRuntimeId] = useState("");
  const [squadName, setSquadName] = useState("");

  useEffect(() => {
    setRuntimeIds({});
    setBulkRuntimeId("");
    setSquadName(template?.snapshot?.squad?.name ?? template?.name ?? "");
  }, [templateId, template?.id, template?.name, template?.snapshot?.squad?.name]);

  const agents = template?.snapshot?.agents ?? [];
  const mutation = useMutation({
    mutationFn: () => api.applyMarketplaceTemplate(template!.id, {
      name: template?.source_type === "squad" ? squadName.trim() : undefined,
      runtime_ids: runtimeIds,
    }),
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: marketplaceTemplateKeys.all(wsId) });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) });
      toast.success(t(($) => $.apply.success));
      onOpenChange(false);
      if (result.squad_id) push(p.squadDetail(result.squad_id));
      else {
        const firstAgentId = Object.values(result.agent_ids)[0];
        if (firstAgentId) push(p.agentDetail(firstAgentId));
      }
    },
  });

  const allAssigned = agents.length > 0 && agents.every((agent) => runtimeIds[agent.key]);

  return (
    <Dialog open={templateId !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[88vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{template ? t(($) => $.apply.title, { name: template.name }) : t(($) => $.card.use)}</DialogTitle>
          <DialogDescription>{t(($) => $.apply.description)}</DialogDescription>
        </DialogHeader>
        {isPending || !template ? (
          <div className="grid gap-3 py-4"><Skeleton className="h-20" /><Skeleton className="h-20" /></div>
        ) : (
          <div className="grid gap-5 py-2">
            <div className="rounded-lg border border-warning/30 bg-warning/5 p-3">
              <div className="flex gap-2">
                <ShieldAlert className="mt-0.5 size-4 shrink-0 text-warning" />
                <div>
                  <p className="text-body font-medium">{t(($) => $.apply.warning_title)}</p>
                  <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.apply.warning_body)}</p>
                </div>
              </div>
            </div>
            {template.source_type === "squad" ? (
              <div className="grid gap-1.5">
                <Label htmlFor="import-squad-name">{t(($) => $.apply.squad_name)}</Label>
                <Input id="import-squad-name" value={squadName} onChange={(event) => setSquadName(event.target.value)} />
              </div>
            ) : null}
            <div className="grid gap-2 rounded-lg border p-3">
              <Label>{t(($) => $.apply.bulk_runtime)}</Label>
              <RuntimePicker
                runtimes={runtimes}
                runtimesLoading={runtimesLoading}
                members={members}
                currentUserId={userId}
                selectedRuntimeId={bulkRuntimeId}
                onSelect={setBulkRuntimeId}
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!bulkRuntimeId}
                onClick={() => setRuntimeIds(Object.fromEntries(agents.map((agent) => [agent.key, bulkRuntimeId])))}
              >
                {t(($) => $.apply.apply_all)}
              </Button>
            </div>
            <div className="grid gap-3">
              <Label>{t(($) => $.apply.agent_runtime)}</Label>
              {agents.map((agent) => (
                <div key={agent.key} className="grid gap-2 rounded-lg border p-3 md:grid-cols-[minmax(0,1fr)_minmax(240px,1fr)] md:items-center">
                  <div className="min-w-0">
                    <p className="truncate text-body font-medium">{agent.name}</p>
                    <p className="line-clamp-2 text-caption text-muted-foreground">{agent.description}</p>
                  </div>
                  <RuntimePicker
                    runtimes={runtimes}
                    runtimesLoading={runtimesLoading}
                    members={members}
                    currentUserId={userId}
                    selectedRuntimeId={runtimeIds[agent.key] ?? ""}
                    onSelect={(runtimeId) => setRuntimeIds((current) => ({ ...current, [agent.key]: runtimeId }))}
                  />
                </div>
              ))}
            </div>
          </div>
        )}
        <DialogFooter>
          <Button disabled={!template || !allAssigned || mutation.isPending || (template.source_type === "squad" && squadName.trim() === "")} onClick={() => mutation.mutate()}>
            {mutation.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
            {t(($) => $.apply.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function TemplatesPage() {
  const { t } = useT("templates");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const [query, setQuery] = useState("");
  const [sourceType, setSourceType] = useState<MarketplaceTemplateSourceType | "">("");
  const [scope, setScope] = useState<MarketplaceTemplateScope>("all");
  const [sort, setSort] = useState<MarketplaceTemplateSort>("popular");
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [applyTemplateId, setApplyTemplateId] = useState<string | null>(null);

  const params = useMemo(() => ({
    query: query.trim() || undefined,
    source_type: sourceType || undefined,
    scope,
    sort,
    page,
    page_size: PAGE_SIZE,
  }), [page, query, scope, sort, sourceType]);
  const { data, isPending, isError } = useQuery(marketplaceTemplateListOptions(wsId, params));
  const templates = data?.templates ?? [];
  const totalPages = Math.max(1, Math.ceil((data?.total ?? 0) / PAGE_SIZE));

  useEffect(() => setPage(1), [query, sourceType, scope, sort]);

  const typeItems = [
    { value: "all", label: t(($) => $.page.all_types) },
    { value: "agent", label: t(($) => $.page.agent_type) },
    { value: "squad", label: t(($) => $.page.squad_type) },
  ];
  const sortItems = [
    { value: "popular", label: t(($) => $.page.popular_sort) },
    { value: "recent", label: t(($) => $.page.recent_sort) },
  ];
  const scopes: Array<{ value: MarketplaceTemplateScope; label: string }> = [
    { value: "all", label: t(($) => $.page.all_scopes) },
    { value: "public", label: t(($) => $.page.public_scope) },
    { value: "workspace", label: t(($) => $.page.workspace_scope) },
    { value: "private", label: t(($) => $.page.private_scope) },
  ];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={LayoutTemplate}
        title={t(($) => $.page.title)}
        count={data?.total}
        description={t(($) => $.page.description)}
        actions={<CollectionPageHeaderAction icon={Plus} label={t(($) => $.page.create)} onClick={() => setCreateOpen(true)} />}
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-7xl flex-col gap-5 px-4 py-5 md:px-6">
          <div className="flex flex-col gap-3 rounded-xl border bg-card p-3 lg:flex-row lg:items-center">
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t(($) => $.page.search_placeholder)} className="pl-9" />
            </div>
            <Select items={typeItems} value={sourceType || "all"} onValueChange={(value) => setSourceType(value === "all" ? "" : value as MarketplaceTemplateSourceType)}>
              <SelectTrigger className="w-full lg:w-48"><SelectValue /></SelectTrigger>
              <SelectContent>{typeItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
            </Select>
            <Select items={sortItems} value={sort} onValueChange={(value) => value && setSort(value as MarketplaceTemplateSort)}>
              <SelectTrigger className="w-full lg:w-48"><SelectValue /></SelectTrigger>
              <SelectContent>{sortItems.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
            </Select>
          </div>

          <div className="flex flex-wrap gap-2">
            {scopes.map((item) => (
              <Button key={item.value} size="sm" variant={scope === item.value ? "default" : "outline"} onClick={() => setScope(item.value)}>
                {item.label}
              </Button>
            ))}
          </div>

          {isPending ? (
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="h-72 rounded-xl" />)}
            </div>
          ) : isError ? (
            <CollectionPageState icon={LayoutTemplate} title={t(($) => $.page.empty_title)} description={t(($) => $.page.empty_description)} tone="destructive" />
          ) : templates.length === 0 ? (
            <CollectionPageState icon={LayoutTemplate} title={t(($) => $.page.empty_title)} description={t(($) => $.page.empty_description)} actions={<Button onClick={() => setCreateOpen(true)}><Plus className="size-4" />{t(($) => $.page.create)}</Button>} />
          ) : (
            <>
              <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                {templates.map((template) => <TemplateCard key={template.id} template={template} onUse={(item) => setApplyTemplateId(item.id)} />)}
              </div>
              <div className="flex items-center justify-between gap-3 border-t pt-4">
                <p className="text-caption text-muted-foreground">{t(($) => $.page.result_count, { count: data?.total ?? 0 })}</p>
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage((current) => Math.max(1, current - 1))}><ChevronLeft className="size-4" />{t(($) => $.page.previous)}</Button>
                  <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage((current) => Math.min(totalPages, current + 1))}>{t(($) => $.page.next)}<ChevronRight className="size-4" /></Button>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
      <CreateTemplateDialog open={createOpen} onOpenChange={setCreateOpen} wsId={wsId} />
      <ApplyTemplateDialog templateId={applyTemplateId} onOpenChange={(open) => !open && setApplyTemplateId(null)} wsId={wsId} />
    </div>
  );
}
