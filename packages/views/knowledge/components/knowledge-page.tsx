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
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useWorkspacePaths } from "@multica/core/paths";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  knowledgeAnalyticsOptions,
  knowledgeCandidatesOptions,
  knowledgeDetailOptions,
  knowledgeEffectOptions,
  knowledgeGovernanceFindingsOptions,
  knowledgeListOptions,
} from "@multica/core/knowledge/queries";
import {
  useArchiveKnowledge,
  useCreateKnowledgeDraftFromCandidate,
  useCreateKnowledgeDraftFromGovernanceFinding,
  useDismissKnowledgeGovernance,
  usePublishKnowledge,
  useResolveKnowledgeGovernanceFinding,
  useRestoreKnowledge,
  useReviewKnowledge,
  useUpdateKnowledge,
} from "@multica/core/knowledge/mutations";
import type {
  KnowledgeCandidate,
  KnowledgeAnalyticsRow,
  KnowledgeDetail,
  KnowledgeEffectBucket,
  KnowledgeGovernanceFinding,
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
import { KnowledgePublishSkillDialog, KnowledgePublishWikiDialog } from "./knowledge-publish-dialogs";

type KnowledgeStatusView = "all" | KnowledgeLifecycleStatus;
type KnowledgeWorkspaceView = "knowledge" | "review" | "candidates" | "analytics";
type AnalyticsSubView = "items" | "effect";

type CandidateMetadata = {
  knowledge_item_id?: string;
  draft_generation?: {
    status?: string;
    knowledge_item_id?: string;
    generated_at?: string;
    draft_error?: string;
  };
};

const STATUS_OPTIONS: KnowledgeStatusView[] = [
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

function candidateMetadata(candidate: KnowledgeCandidate): CandidateMetadata {
  if (!candidate.metadata || typeof candidate.metadata !== "object") return {};
  return candidate.metadata as CandidateMetadata;
}

function candidateKnowledgeItemId(candidate: KnowledgeCandidate): string | null {
  const metadata = candidateMetadata(candidate);
  return metadata.knowledge_item_id ?? metadata.draft_generation?.knowledge_item_id ?? null;
}

function KnowledgeDetailPanel({ detail }: { detail: KnowledgeDetail | null }) {
  const { t, i18n } = useT("knowledge");
  const paths = useWorkspacePaths();
  const updateKnowledge = useUpdateKnowledge();
  const reviewKnowledge = useReviewKnowledge();
  const publishKnowledge = usePublishKnowledge();
  const dismissGovernance = useDismissKnowledgeGovernance();
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
    action: "review" | "publish" | "archive" | "restore" | "governance_dismiss",
    fn: () => Promise<unknown>,
  ) => {
    try {
      await fn();
      toast.success(t(($) => $.toast[action]));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };
  const hasGovernanceFinding = !!(item.review_needed_at || item.review_reason || item.update_suggestion || item.conflict_group);

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
        <KnowledgePublishWikiDialog detail={detail} />
        <KnowledgePublishSkillDialog detail={detail} />
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
                <Button
                  size="sm"
                  variant="outline"
                  className="mt-3"
                  disabled={dismissGovernance.isPending || !hasGovernanceFinding}
                  onClick={() => runAction("governance_dismiss", () => dismissGovernance.mutateAsync(item.id))}
                >
                  {t(($) => $.governance.dismiss)}
                </Button>
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
                        {source.source_type === "knowledge" && source.source_id && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            render={<AppLink href={paths.knowledgeDetail(source.source_id ?? "")} />}
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
                  {detail.publish_targets.map((target) => (
                    <div key={`${target.target_type}-${target.target_id ?? target.target_url ?? target.id}`} className="rounded-md bg-muted/40 px-2 py-1 text-xs">
                      <div className="flex items-center justify-between gap-2">
                        <span className="truncate font-medium text-foreground">{target.target_title ?? target.target_type}</span>
                        <Badge variant="outline">{target.target_type}</Badge>
                      </div>
                      {(target.target_url || target.updated_at) && (
                        <div className="mt-1 truncate">
                          {target.target_url ?? new Date(target.updated_at).toLocaleString(i18n.language)}
                        </div>
                      )}
                    </div>
                  ))}
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

function CandidateQueue({ candidates }: { candidates: KnowledgeCandidate[] }) {
  const { t, i18n } = useT("knowledge");
  const paths = useWorkspacePaths();
  const createDraft = useCreateKnowledgeDraftFromCandidate();
  const generate = async (candidate: KnowledgeCandidate, regenerate = false) => {
    try {
      await createDraft.mutateAsync({ candidate_id: candidate.id, regenerate });
      toast.success(t(($) => $.toast.draft_created));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };
  if (candidates.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
        <Sparkles className="h-7 w-7 text-muted-foreground/50" />
        <p className="text-sm font-medium">{t(($) => $.candidates.empty_title)}</p>
        <p className="text-sm text-muted-foreground">{t(($) => $.candidates.empty_body)}</p>
      </div>
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto grid max-w-5xl gap-3">
        {candidates.map((candidate) => {
          const itemId = candidateKnowledgeItemId(candidate);
          const metadata = candidateMetadata(candidate);
          return (
            <div key={candidate.id} className="rounded-lg border p-4">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline">{candidate.status}</Badge>
                <Badge variant="secondary">{candidate.signal_strength}</Badge>
                <span className="text-sm font-medium">{candidate.trigger_reason}</span>
                <span className="ml-auto font-mono text-xs text-muted-foreground">{candidate.score}</span>
              </div>
              <div className="mt-2 flex flex-wrap gap-1">
                {candidate.signals.map((signal) => <Badge key={signal} variant="outline">{signal}</Badge>)}
              </div>
              <p className="mt-2 text-xs text-muted-foreground">
                {candidate.source_type} · {new Date(candidate.evaluated_at).toLocaleString(i18n.language)}
                {metadata.draft_generation?.status ? ` · ${metadata.draft_generation.status}` : ""}
              </p>
              <div className="mt-3 flex gap-2">
                {itemId && (
                  <Button size="sm" variant="outline" render={<AppLink href={paths.knowledgeDetail(itemId)} />}>
                    {t(($) => $.candidates.open_draft)}
                  </Button>
                )}
                <Button size="sm" onClick={() => generate(candidate, false)} disabled={createDraft.isPending}>
                  {t(($) => $.candidates.generate_draft)}
                </Button>
                <Button size="sm" variant="outline" onClick={() => generate(candidate, true)} disabled={createDraft.isPending}>
                  {t(($) => $.candidates.regenerate_draft)}
                </Button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function findingEvidenceSummary(finding: KnowledgeGovernanceFinding): string {
  const evidence = finding.evidence;
  if (!evidence || typeof evidence !== "object") return "";
  const record = evidence as Record<string, unknown>;
  const values = [
    ["helpful", record.helpful_count],
    ["not helpful", record.not_helpful_count],
    ["misleading", record.misleading_count],
    ["outdated", record.outdated_count],
    ["injections", record.injection_count],
    ["usage", record.usage_count],
  ]
    .filter(([, value]) => typeof value === "number" && value > 0)
    .map(([label, value]) => `${label}: ${value}`);
  return values.join(" · ");
}

function findingSourceMapSummary(finding: KnowledgeGovernanceFinding): string {
  const sourceMap = finding.source_map;
  if (!sourceMap || typeof sourceMap !== "object") return "";
  const record = sourceMap as Record<string, unknown>;
  const feedback = Array.isArray(record.negative_feedback) ? record.negative_feedback.length : 0;
  const sourceIssues = Array.isArray(record.source_issue_ids) ? record.source_issue_ids.length : 0;
  const sources = Array.isArray(record.sources) ? record.sources.length : 0;
  return [feedback ? `${feedback} feedback` : "", sourceIssues ? `${sourceIssues} issues` : "", sources ? `${sources} sources` : ""]
    .filter(Boolean)
    .join(" · ");
}

function governanceFindingTypeLabel(type: string, t: ReturnType<typeof useT<"knowledge">>["t"]): string {
  switch (type) {
    case "stale":
      return t(($) => $.governance_types.stale);
    case "conflict":
      return t(($) => $.governance_types.conflict);
    case "low_effectiveness":
      return t(($) => $.governance_types.low_effectiveness);
    case "misleading":
      return t(($) => $.governance_types.misleading);
    case "outdated":
      return t(($) => $.governance_types.outdated);
    default:
      return type;
  }
}

function governanceFindingStatusLabel(status: string, t: ReturnType<typeof useT<"knowledge">>["t"]): string {
  switch (status) {
    case "open":
      return t(($) => $.governance_status.open);
    case "drafted":
      return t(($) => $.governance_status.drafted);
    case "accepted":
      return t(($) => $.governance_status.accepted);
    case "rejected":
      return t(($) => $.governance_status.rejected);
    case "dismissed":
      return t(($) => $.governance_status.dismissed);
    case "archived":
      return t(($) => $.governance_status.archived);
    case "deprecated":
      return t(($) => $.governance_status.deprecated);
    default:
      return status;
  }
}

function GovernanceFindingsQueue({ findings }: { findings: KnowledgeGovernanceFinding[] }) {
  const { t, i18n } = useT("knowledge");
  const paths = useWorkspacePaths();
  const createDraft = useCreateKnowledgeDraftFromGovernanceFinding();
  const resolveFinding = useResolveKnowledgeGovernanceFinding();
  const generate = async (finding: KnowledgeGovernanceFinding, regenerate = false) => {
    try {
      await createDraft.mutateAsync({ finding_id: finding.id, regenerate });
      toast.success(t(($) => $.toast.draft_created));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };
  const resolve = async (
    finding: KnowledgeGovernanceFinding,
    action: "accept" | "reject" | "dismiss" | "archive" | "deprecate",
  ) => {
    try {
      await resolveFinding.mutateAsync({ id: finding.id, action });
      toast.success(t(($) => $.toast[`governance_${action}`] ?? $.toast.action_failed));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };
  if (findings.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
        <AlertTriangle className="h-7 w-7 text-muted-foreground/50" />
        <p className="text-sm font-medium">{t(($) => $.governance_queue.empty_title)}</p>
        <p className="text-sm text-muted-foreground">{t(($) => $.governance_queue.empty_body)}</p>
      </div>
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto grid max-w-5xl gap-3">
        {findings.map((finding) => {
          const evidence = findingEvidenceSummary(finding);
          const sourceMap = findingSourceMapSummary(finding);
          const hasDraft = !!finding.draft_knowledge_item_id;
          const isResolved = !["open", "drafted"].includes(finding.status);
          return (
            <div key={finding.id} className="rounded-lg border p-4">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant={finding.finding_type === "conflict" || finding.finding_type === "misleading" ? "destructive" : "outline"}>
                  {governanceFindingTypeLabel(finding.finding_type, t)}
                </Badge>
                <Badge variant={isResolved ? "secondary" : "outline"}>
                  {governanceFindingStatusLabel(finding.status, t)}
                </Badge>
                <span className="font-mono text-xs text-muted-foreground">{finding.severity}</span>
                <span className="ml-auto text-xs text-muted-foreground">
                  {new Date(finding.updated_at).toLocaleString(i18n.language)}
                </span>
              </div>
              <p className="mt-3 text-sm font-medium">{finding.reason}</p>
              {finding.suggested_action && (
                <p className="mt-1 text-sm text-muted-foreground">{finding.suggested_action}</p>
              )}
              <div className="mt-3 grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
                {evidence && <span>{evidence}</span>}
                {sourceMap && <span>{sourceMap}</span>}
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                <Button size="sm" variant="outline" render={<AppLink href={paths.knowledgeDetail(finding.knowledge_item_id)} />}>
                  <ExternalLink className="h-4 w-4" />
                  {t(($) => $.governance_queue.open_original)}
                </Button>
                {hasDraft && (
                  <Button size="sm" variant="outline" render={<AppLink href={paths.knowledgeDetail(finding.draft_knowledge_item_id ?? "")} />}>
                    <FileText className="h-4 w-4" />
                    {t(($) => $.governance_queue.open_draft)}
                  </Button>
                )}
                <Button size="sm" onClick={() => generate(finding, hasDraft)} disabled={createDraft.isPending || isResolved}>
                  <Sparkles className="h-4 w-4" />
                  {hasDraft ? t(($) => $.governance_queue.regenerate_draft) : t(($) => $.governance_queue.generate_draft)}
                </Button>
                <Button size="sm" variant="outline" onClick={() => resolve(finding, "accept")} disabled={resolveFinding.isPending || !hasDraft || isResolved}>
                  <CheckCircle2 className="h-4 w-4" />
                  {t(($) => $.governance_queue.accept)}
                </Button>
                <Button size="sm" variant="outline" onClick={() => resolve(finding, "reject")} disabled={resolveFinding.isPending || isResolved}>
                  <XCircle className="h-4 w-4" />
                  {t(($) => $.governance_queue.reject)}
                </Button>
                <Button size="sm" variant="outline" onClick={() => resolve(finding, "archive")} disabled={resolveFinding.isPending || isResolved}>
                  <Archive className="h-4 w-4" />
                  {t(($) => $.governance_queue.archive_original)}
                </Button>
                <Button size="sm" variant="outline" onClick={() => resolve(finding, "deprecate")} disabled={resolveFinding.isPending || isResolved}>
                  {t(($) => $.governance_queue.deprecate_original)}
                </Button>
                <Button size="sm" variant="ghost" onClick={() => resolve(finding, "dismiss")} disabled={resolveFinding.isPending || isResolved}>
                  {t(($) => $.governance_queue.dismiss)}
                </Button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

type EffectComparisonGroup = {
  task_kind: string;
  model: string;
  with_injection: KnowledgeEffectBucket | null;
  without_injection: KnowledgeEffectBucket | null;
};

function EffectComparisonPanel({ wsId }: { wsId: string }) {
  const { t } = useT("knowledge");
  const [timeRange, setTimeRange] = useState<"7d" | "30d" | "90d">("30d");

  const since = useMemo(() => {
    const d = new Date();
    if (timeRange === "7d") d.setDate(d.getDate() - 7);
    else if (timeRange === "30d") d.setDate(d.getDate() - 30);
    else d.setDate(d.getDate() - 90);
    return d.toISOString();
  }, [timeRange]);

  const effectQuery = useQuery(
    knowledgeEffectOptions(wsId, { since, limit: 500 }),
  );

  const buckets = effectQuery.data?.buckets ?? [];

  const groups = useMemo(() => {
    const map = new Map<string, EffectComparisonGroup>();
    for (const b of buckets) {
      const key = `${b.task_kind}|${b.model || b.provider || "unknown"}`;
      let g = map.get(key);
      if (!g) {
        g = { task_kind: b.task_kind, model: b.model || b.provider || "unknown", with_injection: null, without_injection: null };
        map.set(key, g);
      }
      if (b.has_injection) g.with_injection = b;
      else g.without_injection = b;
    }
    return [...map.values()].sort((a, b) => {
      const aTotal = (a.with_injection?.task_count ?? 0) + (a.without_injection?.task_count ?? 0);
      const bTotal = (b.with_injection?.task_count ?? 0) + (b.without_injection?.task_count ?? 0);
      return bTotal - aTotal;
    });
  }, [buckets]);

  const summary = useMemo(() => {
    const withInj = { tasks: 0, successful: 0, failed: 0, duration_secs: 0, duration_tasks: 0, tokens: 0, reruns: 0, follow_ups: 0 };
    const withoutInj = { ...withInj };
    for (const b of buckets) {
      const target = b.has_injection ? withInj : withoutInj;
      target.tasks += b.task_count;
      target.successful += b.successful_count;
      target.failed += b.failed_count;
      target.duration_secs += b.total_duration_secs;
      target.duration_tasks += b.duration_task_count;
      target.tokens += b.input_tokens + b.output_tokens + b.cache_read_tokens + b.cache_write_tokens;
      target.reruns += b.rerun_count;
      target.follow_ups += b.follow_up_count;
    }
    return { with_injection: withInj, without_injection: withoutInj };
  }, [buckets]);

  const kindLabel = (k: string) => {
    const key = `task_kind_${k}` as keyof ReturnType<typeof t>;
    return t(($) => ($ as any).effect[key] ?? k);
  };

  const formatDuration = (secs: number, count: number) => {
    if (count === 0) return t(($) => $.effect.no_duration_data);
    const avg = secs / count;
    if (avg < 60) return `${Math.round(avg)}s`;
    if (avg < 3600) return `${(avg / 60).toFixed(1)}m`;
    return `${(avg / 3600).toFixed(1)}h`;
  };

  const formatTokens = (n: number) => {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`;
    return String(n);
  };

  if (buckets.length === 0 && !effectQuery.isLoading) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
        <BookOpenCheck className="h-7 w-7 text-muted-foreground/50" />
        <p className="text-sm font-medium">{t(($) => $.effect.empty_title)}</p>
        <p className="text-sm text-muted-foreground">{t(($) => $.effect.empty_body)}</p>
      </div>
    );
  }

  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-5xl space-y-6">
        {/* Filter bar */}
        <div className="flex items-center gap-3">
          <span className="text-xs text-muted-foreground">{t(($) => $.effect.filter_time)}</span>
          <div className="flex gap-1 rounded-md border p-1">
            {(["7d", "30d", "90d"] as const).map((range) => (
              <Button
                key={range}
                size="sm"
                variant={timeRange === range ? "secondary" : "ghost"}
                onClick={() => setTimeRange(range)}
              >
                {t(($) => ($ as any).effect[`time_${range}`])}
              </Button>
            ))}
          </div>
        </div>

        {/* Summary cards */}
        <div className="grid gap-3 md:grid-cols-4">
          <div className="rounded-lg border p-3">
            <p className="text-xs text-muted-foreground">{t(($) => $.effect.tasks)}</p>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-xl font-semibold">{summary.with_injection.tasks}</span>
              <span className="text-xs text-muted-foreground">/</span>
              <span className="text-lg">{summary.without_injection.tasks}</span>
            </div>
            <p className="text-xs text-muted-foreground">{t(($) => $.effect.summary_with_injection)} / {t(($) => $.effect.summary_without_injection)}</p>
          </div>
          <div className="rounded-lg border p-3">
            <p className="text-xs text-muted-foreground">{t(($) => $.effect.avg_duration)}</p>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-xl font-semibold">{formatDuration(summary.with_injection.duration_secs, summary.with_injection.duration_tasks)}</span>
              <span className="text-xs text-muted-foreground">/</span>
              <span className="text-lg">{formatDuration(summary.without_injection.duration_secs, summary.without_injection.duration_tasks)}</span>
            </div>
          </div>
          <div className="rounded-lg border p-3">
            <p className="text-xs text-muted-foreground">{t(($) => $.effect.success_rate)}</p>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-xl font-semibold">
                {summary.with_injection.tasks > 0 ? `${Math.round(summary.with_injection.successful / summary.with_injection.tasks * 100)}%` : "-"}
              </span>
              <span className="text-xs text-muted-foreground">/</span>
              <span className="text-lg">
                {summary.without_injection.tasks > 0 ? `${Math.round(summary.without_injection.successful / summary.without_injection.tasks * 100)}%` : "-"}
              </span>
            </div>
          </div>
          <div className="rounded-lg border p-3">
            <p className="text-xs text-muted-foreground">{t(($) => $.effect.total_tokens)}</p>
            <div className="mt-1 flex items-baseline gap-2">
              <span className="text-xl font-semibold">{formatTokens(summary.with_injection.tokens)}</span>
              <span className="text-xs text-muted-foreground">/</span>
              <span className="text-lg">{formatTokens(summary.without_injection.tokens)}</span>
            </div>
          </div>
        </div>

        {/* Comparison table */}
        <div className="rounded-lg border">
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-2 text-left text-xs font-medium">{t(($) => $.effect.column_dimension)}</th>
                  <th className="px-4 py-2 text-center text-xs font-medium" colSpan={3}>{t(($) => $.effect.column_with)}</th>
                  <th className="px-4 py-2 text-center text-xs font-medium" colSpan={3}>{t(($) => $.effect.column_without)}</th>
                </tr>
                <tr className="border-b bg-muted/30 text-xs text-muted-foreground">
                  <th className="px-4 py-1" />
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.tasks)}</th>
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.avg_duration)}</th>
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.success_rate)}</th>
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.tasks)}</th>
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.avg_duration)}</th>
                  <th className="px-3 py-1 text-right">{t(($) => $.effect.success_rate)}</th>
                </tr>
              </thead>
              <tbody>
                {groups.map((g) => {
                  const w = g.with_injection;
                  const wo = g.without_injection;
                  const wSuccess = w && w.task_count > 0 ? Math.round(w.successful_count / w.task_count * 100) : null;
                  const woSuccess = wo && wo.task_count > 0 ? Math.round(wo.successful_count / wo.task_count * 100) : null;
                  const wDur = w ? formatDuration(w.total_duration_secs, w.duration_task_count) : "-";
                  const woDur = wo ? formatDuration(wo.total_duration_secs, wo.duration_task_count) : "-";
                  return (
                    <tr key={`${g.task_kind}|${g.model}`} className="border-b last:border-b-0 hover:bg-muted/30">
                      <td className="px-4 py-2">
                        <span className="font-medium">{kindLabel(g.task_kind)}</span>
                        <span className="ml-2 text-xs text-muted-foreground">{g.model}</span>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums">{w?.task_count ?? 0}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{wDur}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{wSuccess !== null ? `${wSuccess}%` : "-"}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{wo?.task_count ?? 0}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{woDur}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{woSuccess !== null ? `${woSuccess}%` : "-"}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  );
}

function AnalyticsPanel({ rows }: { rows: KnowledgeAnalyticsRow[] }) {
  const { t } = useT("knowledge");
  const paths = useWorkspacePaths();
  const totals = rows.reduce((acc, row) => ({
    retrievals: acc.retrievals + row.retrieval_count,
    injections: acc.injections + row.injection_count,
    usage: acc.usage + row.usage_count,
    negative: acc.negative + row.not_helpful_count + row.misleading_count + row.outdated_count,
  }), { retrievals: 0, injections: 0, usage: 0, negative: 0 });
  if (rows.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
        <BookOpenCheck className="h-7 w-7 text-muted-foreground/50" />
        <p className="text-sm font-medium">{t(($) => $.analytics.empty_title)}</p>
        <p className="text-sm text-muted-foreground">{t(($) => $.analytics.empty_body)}</p>
      </div>
    );
  }
  return (
    <div className="min-h-0 flex-1 overflow-y-auto p-6">
      <div className="mx-auto max-w-5xl space-y-4">
        <div className="grid gap-3 md:grid-cols-4">
          <div className="rounded-lg border p-3"><p className="text-xs text-muted-foreground">{t(($) => $.analytics.retrievals)}</p><p className="text-xl font-semibold">{totals.retrievals}</p></div>
          <div className="rounded-lg border p-3"><p className="text-xs text-muted-foreground">{t(($) => $.analytics.injections)}</p><p className="text-xl font-semibold">{totals.injections}</p></div>
          <div className="rounded-lg border p-3"><p className="text-xs text-muted-foreground">{t(($) => $.analytics.usage)}</p><p className="text-xl font-semibold">{totals.usage}</p></div>
          <div className="rounded-lg border p-3"><p className="text-xs text-muted-foreground">{t(($) => $.analytics.negative_feedback)}</p><p className="text-xl font-semibold">{totals.negative}</p></div>
        </div>
        <div className="space-y-2">
          {rows.map((row) => (
            <AppLink key={row.knowledge_item_id} href={paths.knowledgeDetail(row.knowledge_item_id)} className="block rounded-lg border p-4 hover:bg-accent/50">
              <div className="flex min-w-0 items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-sm font-medium">{row.title}</span>
                <Badge variant="outline">{row.lifecycle_status}</Badge>
              </div>
              <div className="mt-3 grid gap-2 text-xs text-muted-foreground md:grid-cols-5">
                <span>{t(($) => $.analytics.retrievals)}: {row.retrieval_count}</span>
                <span>{t(($) => $.analytics.injections)}: {row.injection_count}</span>
                <span>{t(($) => $.analytics.usage)}: {row.usage_count}</span>
                <span>{t(($) => $.analytics.feedback)}: {row.helpful_count}/{row.not_helpful_count + row.misleading_count + row.outdated_count}</span>
                <span>{t(($) => $.analytics.tasks)}: {row.successful_task_count}/{row.failed_task_count}</span>
              </div>
            </AppLink>
          ))}
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
  const [workspaceView, setWorkspaceView] = useState<KnowledgeWorkspaceView>("knowledge");
  const [view, setView] = useState<KnowledgeStatusView>("all");
  const [search, setSearch] = useState("");
  const [analyticsSubView, setAnalyticsSubView] = useState<AnalyticsSubView>("items");
  const listParams = useMemo(
    () => ({
      status: workspaceView === "review" ? undefined : view === "all" ? undefined : view,
      include_inactive: workspaceView === "review" || view === "archived" || view === "deprecated",
      limit: 50,
    }),
    [view, workspaceView],
  );
  const listQuery = useQuery(knowledgeListOptions(wsId, listParams));
  const candidatesQuery = useQuery({
    ...knowledgeCandidatesOptions(wsId, { limit: 50 }),
    enabled: workspaceView === "candidates",
  });
  const governanceFindingsQuery = useQuery({
    ...knowledgeGovernanceFindingsOptions(wsId, { status: "active", limit: 50 }),
    enabled: workspaceView === "review",
  });
  const analyticsQuery = useQuery({
    ...knowledgeAnalyticsOptions(wsId, { limit: 50 }),
    enabled: workspaceView === "analytics",
  });
  const items = useMemo(
    () => (listQuery.data?.items ?? [])
      .filter((item) => workspaceView !== "review" || item.lifecycle_status === "draft" || !!item.review_needed_at || !!item.review_reason)
      .filter((item) => itemMatches(item, search)),
    [listQuery.data?.items, search, workspaceView],
  );
  const selectedId = knowledgeId ?? items[0]?.id ?? null;
  const detailQuery = useQuery({
    ...knowledgeDetailOptions(wsId, selectedId),
    enabled: !!selectedId,
  });

  useEffect(() => {
    if (workspaceView === "knowledge" && !knowledgeId && items[0]?.id) {
      nav.replace(paths.knowledgeDetail(items[0].id));
    }
  }, [items, knowledgeId, nav, paths, workspaceView]);

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
        <div className="flex items-center gap-1 rounded-md border p-1">
          {(["knowledge", "review", "candidates", "analytics"] as KnowledgeWorkspaceView[]).map((mode) => (
            <Button
              key={mode}
              size="sm"
              variant={workspaceView === mode ? "secondary" : "ghost"}
              onClick={() => setWorkspaceView(mode)}
            >
              {t(($) => $.views[mode])}
            </Button>
          ))}
        </div>
        {workspaceView === "analytics" && (
          <div className="flex items-center gap-1 rounded-md border p-1">
            {(["items", "effect"] as AnalyticsSubView[]).map((sub) => (
              <Button
                key={sub}
                size="sm"
                variant={analyticsSubView === sub ? "secondary" : "ghost"}
                onClick={() => setAnalyticsSubView(sub)}
              >
                {sub === "items" ? t(($) => $.analytics.title) : t(($) => $.effect.title)}
              </Button>
            ))}
          </div>
        )}
        {workspaceView === "knowledge" && (
          <Select value={view} onValueChange={(value) => setView(value as KnowledgeStatusView)}>
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
        )}
      </PageHeader>

      <div className="flex min-h-0 flex-1">
        {workspaceView === "knowledge" && (
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
        )}
        <main className="min-w-0 flex-1">
          {workspaceView === "review" ? (
            governanceFindingsQuery.isLoading ? (
              <div className="space-y-4 p-6">
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-24 w-full" />
              </div>
            ) : (
              <GovernanceFindingsQueue findings={governanceFindingsQuery.data?.findings ?? []} />
            )
          ) : workspaceView === "candidates" ? (
            candidatesQuery.isLoading ? (
              <div className="space-y-4 p-6">
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-24 w-full" />
              </div>
            ) : (
              <CandidateQueue candidates={candidatesQuery.data?.candidates ?? []} />
            )
          ) : workspaceView === "analytics" ? (
            analyticsSubView === "effect" ? (
              <EffectComparisonPanel wsId={wsId} />
            ) : analyticsQuery.isLoading ? (
              <div className="space-y-4 p-6">
                <Skeleton className="h-24 w-full" />
                <Skeleton className="h-24 w-full" />
              </div>
            ) : (
              <AnalyticsPanel rows={analyticsQuery.data?.items ?? []} />
            )
          ) : detailQuery.isLoading ? (
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
