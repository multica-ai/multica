"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  Archive,
  BookOpenCheck,
  CheckCircle2,
  Clock3,
  ExternalLink,
  FileText,
  History,
  Search,
  Sparkles,
} from "lucide-react";
import { toast } from "sonner";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  knowledgeDetailOptions,
  knowledgeListOptions,
} from "@multica/core/knowledge/queries";
import {
  useArchiveKnowledge,
  usePublishKnowledge,
  useRestoreKnowledge,
  useReviewKnowledge,
  useUpdateKnowledge,
} from "@multica/core/knowledge/mutations";
import type {
  KnowledgeDetail,
  KnowledgeItem,
  KnowledgeLifecycleStatus,
  UpdateKnowledgeRequest,
} from "@multica/core/knowledge/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Separator } from "@multica/ui/components/ui/separator";
import { cn } from "@multica/ui/lib/utils";

type KnowledgeView = "all" | KnowledgeLifecycleStatus;

const STATUS_OPTIONS: KnowledgeView[] = [
  "all",
  "draft",
  "reviewed",
  "published",
  "archived",
  "deprecated",
];

const EDIT_FIELDS: Array<{
  key: keyof Pick<
    UpdateKnowledgeRequest,
    | "problem_pattern"
    | "trigger_conditions"
    | "diagnostic_steps"
    | "recommended_practice"
    | "anti_patterns"
    | "applicability"
  >;
  labelKey:
    | "problem_pattern"
    | "trigger_conditions"
    | "diagnostic_steps"
    | "recommended_practice"
    | "anti_patterns"
    | "applicability";
}> = [
  { key: "problem_pattern", labelKey: "problem_pattern" },
  { key: "trigger_conditions", labelKey: "trigger_conditions" },
  { key: "diagnostic_steps", labelKey: "diagnostic_steps" },
  { key: "recommended_practice", labelKey: "recommended_practice" },
  { key: "anti_patterns", labelKey: "anti_patterns" },
  { key: "applicability", labelKey: "applicability" },
];

function itemMatches(item: KnowledgeItem, query: string) {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [
    item.title,
    item.type,
    item.problem_pattern,
    item.trigger_conditions,
    item.recommended_practice,
    item.applicability,
    ...item.domain_labels,
  ].some((value) => value.toLowerCase().includes(q));
}

function statusBadgeVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  switch (status) {
    case "published":
      return "default";
    case "archived":
    case "deprecated":
      return "outline";
    case "draft":
      return "secondary";
    default:
      return "outline";
  }
}

function statusTimestamp(item: KnowledgeItem): string | null {
  return item.published_at ?? item.reviewed_at ?? item.archived_at ?? item.deprecated_at ?? item.updated_at;
}

function feedbackLabel(value: string, t: ReturnType<typeof useT<"knowledge">>["t"]): string {
  switch (value) {
    case "helpful":
      return t(($) => $.feedback.helpful);
    case "not_helpful":
      return t(($) => $.feedback.not_helpful);
    case "misleading":
      return t(($) => $.feedback.misleading);
    case "outdated":
      return t(($) => $.feedback.outdated);
    default:
      return value;
  }
}

function governanceBadges(item: KnowledgeItem, t: ReturnType<typeof useT<"knowledge">>["t"]) {
  const badges: Array<{ key: string; label: string; variant: "outline" | "secondary" | "destructive" }> = [];
  if (item.conflict_group) {
    badges.push({ key: "conflict", label: t(($) => $.governance.conflict), variant: "destructive" });
  }
  if (item.stale_score >= 70) {
    badges.push({ key: "stale", label: t(($) => $.governance.stale), variant: "outline" });
  }
  if (item.effectiveness_score <= 50) {
    badges.push({ key: "low_effectiveness", label: t(($) => $.governance.low_effectiveness), variant: "outline" });
  }
  if (item.review_needed_at && badges.length === 0) {
    badges.push({ key: "review_needed", label: t(($) => $.governance.review_needed), variant: "secondary" });
  }
  return badges;
}

function KnowledgeListSkeleton() {
  return (
    <div className="space-y-2 p-3">
      {Array.from({ length: 6 }).map((_, index) => (
        <div key={index} className="rounded-lg border p-3">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="mt-3 h-3 w-1/2" />
        </div>
      ))}
    </div>
  );
}

function KnowledgeListItem({
  item,
  selected,
  href,
}: {
  item: KnowledgeItem;
  selected: boolean;
  href: string;
}) {
  const { t } = useT("knowledge");
  const badges = governanceBadges(item, t);
  return (
    <AppLink
      href={href}
      className={cn(
        "block rounded-lg border px-3 py-3 transition-colors hover:bg-accent/50",
        selected && "border-primary/50 bg-accent",
      )}
    >
      <div className="flex min-w-0 items-center gap-2">
        <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">{item.title || t(($) => $.detail.untitled)}</span>
        <Badge variant={statusBadgeVariant(item.lifecycle_status)}>
          {t(($) => $.status[item.lifecycle_status] ?? item.lifecycle_status)}
        </Badge>
      </div>
      <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
        {item.problem_pattern || item.recommended_practice || t(($) => $.list.no_summary)}
      </p>
      <div className="mt-2 flex flex-wrap gap-1">
        <Badge variant="outline">{t(($) => $.type[item.type] ?? item.type)}</Badge>
        {badges.slice(0, 2).map((badge) => (
          <Badge key={badge.key} variant={badge.variant}>{badge.label}</Badge>
        ))}
        {item.domain_labels.slice(0, 3).map((label) => (
          <Badge key={label} variant="secondary">{label}</Badge>
        ))}
      </div>
    </AppLink>
  );
}

function makeDraft(detail: KnowledgeDetail | null): UpdateKnowledgeRequest {
  const item = detail?.item;
  return {
    title: item?.title ?? "",
    type: item?.type ?? "lesson",
    domain_labels: item?.domain_labels ?? [],
    problem_pattern: item?.problem_pattern ?? "",
    trigger_conditions: item?.trigger_conditions ?? "",
    diagnostic_steps: item?.diagnostic_steps ?? "",
    recommended_practice: item?.recommended_practice ?? "",
    anti_patterns: item?.anti_patterns ?? "",
    applicability: item?.applicability ?? "",
    confidence_status: item?.confidence_status ?? "medium",
  };
}

function KnowledgeDetailPanel({ detail }: { detail: KnowledgeDetail | null }) {
  const { t, i18n } = useT("knowledge");
  const paths = useWorkspacePaths();
  const updateKnowledge = useUpdateKnowledge();
  const reviewKnowledge = useReviewKnowledge();
  const publishKnowledge = usePublishKnowledge();
  const archiveKnowledge = useArchiveKnowledge();
  const restoreKnowledge = useRestoreKnowledge();
  const [draft, setDraft] = useState<UpdateKnowledgeRequest>(() => makeDraft(detail));

  useEffect(() => {
    setDraft(makeDraft(detail));
  }, [detail?.item.id]);

  if (!detail) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 px-6 text-center">
        <BookOpenCheck className="h-8 w-8 text-muted-foreground/50" />
        <p className="text-sm font-medium">{t(($) => $.detail.empty_title)}</p>
        <p className="max-w-sm text-sm text-muted-foreground">{t(($) => $.detail.empty_body)}</p>
      </div>
    );
  }

  const item = detail.item;
  const isInactive = item.lifecycle_status === "archived" || item.lifecycle_status === "deprecated";
  const governance = governanceBadges(item, t);
  const setField = <K extends keyof UpdateKnowledgeRequest>(key: K, value: UpdateKnowledgeRequest[K]) => {
    setDraft((prev) => ({ ...prev, [key]: value }));
  };
  const save = async () => {
    try {
      await updateKnowledge.mutateAsync({ id: item.id, ...draft });
      toast.success(t(($) => $.toast.saved));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.save_failed));
    }
  };
  const runAction = async (
    action: "review" | "publish" | "archive" | "restore",
    fn: () => Promise<unknown>,
  ) => {
    try {
      await fn();
      toast.success(t(($) => $.toast[action]));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Badge variant={statusBadgeVariant(item.lifecycle_status)}>
              {t(($) => $.status[item.lifecycle_status] ?? item.lifecycle_status)}
            </Badge>
            <Badge variant="outline">{t(($) => $.type[item.type] ?? item.type)}</Badge>
            {governance.map((badge) => (
              <Badge key={badge.key} variant={badge.variant}>{badge.label}</Badge>
            ))}
            {statusTimestamp(item) && (
              <span className="text-xs text-muted-foreground">
                {new Date(statusTimestamp(item)!).toLocaleString(i18n.language)}
              </span>
            )}
          </div>
        </div>
        <Button
          size="sm"
          variant="outline"
          disabled={reviewKnowledge.isPending || item.lifecycle_status !== "draft"}
          onClick={() => runAction("review", () => reviewKnowledge.mutateAsync(item.id))}
        >
          <CheckCircle2 className="h-4 w-4" />
          {t(($) => $.detail.review)}
        </Button>
        <Button
          size="sm"
          disabled={publishKnowledge.isPending || item.lifecycle_status === "published"}
          onClick={() => runAction("publish", () => publishKnowledge.mutateAsync(item.id))}
        >
          <Sparkles className="h-4 w-4" />
          {t(($) => $.detail.publish)}
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={archiveKnowledge.isPending || restoreKnowledge.isPending}
          onClick={() =>
            isInactive
              ? runAction("restore", () => restoreKnowledge.mutateAsync(item.id))
              : runAction("archive", () => archiveKnowledge.mutateAsync(item.id))
          }
        >
          <Archive className="h-4 w-4" />
          {isInactive ? t(($) => $.detail.restore) : t(($) => $.detail.archive)}
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 px-6 py-6">
          <div className="space-y-3">
            <Input
              value={draft.title ?? ""}
              onChange={(event) => setField("title", event.target.value)}
              className="h-10 text-lg font-semibold"
              placeholder={t(($) => $.detail.title_placeholder)}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Select
                value={draft.type ?? "lesson"}
                onValueChange={(value) => setField("type", value ?? "lesson")}
              >
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="lesson">{t(($) => $.type.lesson)}</SelectItem>
                  <SelectItem value="playbook">{t(($) => $.type.playbook)}</SelectItem>
                  <SelectItem value="reference">{t(($) => $.type.reference)}</SelectItem>
                </SelectContent>
              </Select>
              <Select
                value={draft.confidence_status ?? "medium"}
                onValueChange={(value) => setField("confidence_status", value ?? "medium")}
              >
                <SelectTrigger size="sm">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="low">{t(($) => $.confidence.low)}</SelectItem>
                  <SelectItem value="medium">{t(($) => $.confidence.medium)}</SelectItem>
                  <SelectItem value="high">{t(($) => $.confidence.high)}</SelectItem>
                </SelectContent>
              </Select>
              <Input
                value={(draft.domain_labels ?? []).join(", ")}
                onChange={(event) =>
                  setField(
                    "domain_labels",
                    event.target.value
                      .split(",")
                      .map((label) => label.trim())
                      .filter(Boolean),
                  )
                }
                className="max-w-sm"
                placeholder={t(($) => $.detail.labels_placeholder)}
              />
              <Button
                size="sm"
                onClick={save}
                disabled={updateKnowledge.isPending}
                className="ml-auto"
              >
                {t(($) => $.detail.save)}
              </Button>
            </div>
          </div>

          <div className="grid gap-4 lg:grid-cols-2">
            {EDIT_FIELDS.map((field) => (
              <label key={field.key} className="space-y-1.5">
                <span className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.fields[field.labelKey])}
                </span>
                <Textarea
                  value={String(draft[field.key] ?? "")}
                  onChange={(event) => setField(field.key, event.target.value)}
                  className="min-h-28 resize-y text-sm"
                />
              </label>
            ))}
          </div>

          <Separator />

          <section className="space-y-3">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-muted-foreground" />
              <h3 className="text-sm font-semibold">{t(($) => $.governance.title)}</h3>
            </div>
            <div className="grid gap-3 md:grid-cols-3">
              <div className="rounded-lg border px-3 py-2">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Clock3 className="h-3.5 w-3.5" />
                  {t(($) => $.governance.stale_score)}
                </div>
                <p className="mt-1 text-lg font-semibold">{Math.round(item.stale_score)}</p>
              </div>
              <div className="rounded-lg border px-3 py-2">
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Sparkles className="h-3.5 w-3.5" />
                  {t(($) => $.governance.effectiveness_score)}
                </div>
                <p className="mt-1 text-lg font-semibold">{Math.round(item.effectiveness_score)}</p>
              </div>
              <div className="rounded-lg border px-3 py-2">
                <div className="text-xs text-muted-foreground">{t(($) => $.governance.review_reason)}</div>
                <p className="mt-1 truncate text-sm font-medium">
                  {item.review_reason || t(($) => $.governance.no_review_reason)}
                </p>
              </div>
            </div>
            {(item.conflict_group || item.update_suggestion || item.review_needed_at) && (
              <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-sm">
                {item.conflict_group && (
                  <p className="font-medium">{t(($) => $.governance.conflict_group, { group: item.conflict_group ?? "" })}</p>
                )}
                {item.update_suggestion && (
                  <p className="mt-1 text-muted-foreground">{item.update_suggestion}</p>
                )}
                {item.review_needed_at && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {t(($) => $.governance.review_needed_since, { date: new Date(item.review_needed_at).toLocaleString(i18n.language) })}
                  </p>
                )}
              </div>
            )}
          </section>

          <section className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]">
            <div>
              <h3 className="text-sm font-semibold">{t(($) => $.detail.sources)}</h3>
              <div className="mt-3 space-y-2">
                {detail.sources.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_sources)}</p>
                ) : (
                  detail.sources.map((source) => (
                    <div key={source.id} className="rounded-lg border px-3 py-2 text-sm">
                      <div className="flex min-w-0 items-center gap-2">
                        <Badge variant="outline">{source.source_type}</Badge>
                        <span className="min-w-0 flex-1 truncate">
                          {source.source_title ?? source.source_id ?? source.source_url ?? t(($) => $.detail.source_fallback)}
                        </span>
                        {source.source_type === "issue" && source.source_id && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            render={<AppLink href={paths.issueDetail(source.source_id ?? "")} />}
                          >
                            <ExternalLink className="h-4 w-4" />
                          </Button>
                        )}
                      </div>
                      {source.source_excerpt && (
                        <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">
                          {source.source_excerpt}
                        </p>
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
            <div>
              <h3 className="text-sm font-semibold">{t(($) => $.detail.effect)}</h3>
              <div className="mt-3 rounded-lg border p-3">
                {detail.feedback_summary.length === 0 ? (
                  <p className="text-sm text-muted-foreground">{t(($) => $.detail.no_feedback)}</p>
                ) : (
                  <div className="space-y-2">
                    {detail.feedback_summary.map((row) => (
                      <div key={row.value} className="flex items-center justify-between text-sm">
                        <span>{feedbackLabel(row.value, t)}</span>
                        <span className="font-mono text-xs text-muted-foreground">{row.count}</span>
                      </div>
                    ))}
                  </div>
                )}
                <Separator className="my-3" />
                <div className="space-y-2 text-sm text-muted-foreground">
                  <div className="flex items-center justify-between">
                    <span>{t(($) => $.detail.publish_targets)}</span>
                    <span>{detail.publish_targets.length}</span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span>{t(($) => $.detail.embeddings)}</span>
                    <span>{detail.embeddings.length}</span>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}

export function KnowledgePage({ knowledgeId }: { knowledgeId?: string }) {
  const { t } = useT("knowledge");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const [view, setView] = useState<KnowledgeView>("all");
  const [search, setSearch] = useState("");
  const listParams = useMemo(
    () => ({
      status: view === "all" ? undefined : view,
      include_inactive: view === "archived" || view === "deprecated",
      limit: 50,
    }),
    [view],
  );
  const listQuery = useQuery(knowledgeListOptions(wsId, listParams));
  const items = useMemo(
    () => (listQuery.data?.items ?? []).filter((item) => itemMatches(item, search)),
    [listQuery.data?.items, search],
  );
  const selectedId = knowledgeId ?? items[0]?.id ?? null;
  const detailQuery = useQuery({
    ...knowledgeDetailOptions(wsId, selectedId),
    enabled: !!selectedId,
  });

  useEffect(() => {
    if (!knowledgeId && items[0]?.id) {
      nav.replace(paths.knowledgeDetail(items[0].id));
    }
  }, [items, knowledgeId, nav, paths]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader className="gap-3">
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-sm font-semibold">{t(($) => $.page.title)}</h1>
        </div>
        <div className="relative hidden w-72 md:block">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t(($) => $.page.search)}
            className="pl-8"
          />
        </div>
        <Select value={view} onValueChange={(value) => setView(value as KnowledgeView)}>
          <SelectTrigger size="sm">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {STATUS_OPTIONS.map((status) => (
              <SelectItem key={status} value={status}>
                {status === "all" ? t(($) => $.status.all) : t(($) => $.status[status])}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        <aside className="flex w-96 shrink-0 flex-col border-r">
          <div className="border-b p-3 md:hidden">
            <div className="relative">
              <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t(($) => $.page.search)}
                className="pl-8"
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            {listQuery.isLoading ? (
              <KnowledgeListSkeleton />
            ) : items.length === 0 ? (
              <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
                <History className="h-7 w-7 text-muted-foreground/50" />
                <p className="text-sm font-medium">{t(($) => $.list.empty_title)}</p>
                <p className="text-sm text-muted-foreground">{t(($) => $.list.empty_body)}</p>
              </div>
            ) : (
              <div className="space-y-2 p-3">
                {items.map((item) => (
                  <KnowledgeListItem
                    key={item.id}
                    item={item}
                    selected={item.id === selectedId}
                    href={paths.knowledgeDetail(item.id)}
                  />
                ))}
              </div>
            )}
          </div>
        </aside>
        <main className="min-w-0 flex-1">
          {detailQuery.isLoading ? (
            <div className="space-y-4 p-6">
              <Skeleton className="h-8 w-2/3" />
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-24 w-full" />
            </div>
          ) : (
            <KnowledgeDetailPanel detail={detailQuery.data ?? null} />
          )}
        </main>
      </div>
    </div>
  );
}
