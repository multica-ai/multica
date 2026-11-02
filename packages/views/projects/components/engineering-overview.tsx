"use client";

import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, LoaderCircle, Settings2 } from "@multica/ui/icons";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { api, type BaselineChapter, type BaselineDecision, type BaselineRevision } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useDagGraph } from "@multica/core/issues/dag/use-dag-data";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { paths } from "@multica/core/paths";
import { projectBaselineHistoryOptions, projectDetailOptions, projectEngineeringDecisionIssuesOptions, projectEngineeringOptions, projectKeys } from "@multica/core/projects/queries";
import { useAdoptProjectBaseline, useRejectProjectBaseline, useUpdateProjectEngineering } from "@multica/core/projects/mutations";
import { engineeringCandidateOptions } from "@multica/core/projects/engineering-candidate";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { IssueTableQuerySpec } from "@multica/core/types";
import { useT } from "../../i18n";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { CollectionPageState } from "../../layout/collection-page";
import { useNavigation } from "../../navigation";
import { EngineeringBaseline } from "./engineering-baseline";
import { EngineeringDecisions, type EngineeringDecisionTimeline } from "./engineering-decisions";
import { EngineeringProgress } from "./engineering-progress";
import { EngineeringRecentChanges } from "./engineering-recent-changes";
import { EngineeringSettings } from "./engineering-settings";
import { EngineeringStage } from "./engineering-stage";
import { ENGINEERING_CHAPTERS, engineeringPermissions, engineeringStageTitles, selectEngineeringComment } from "./engineering-utils";

interface EngineeringOverviewProps {
  workspaceSlug: string;
  projectId: string;
}

export function EngineeringOverview({ workspaceSlug, projectId }: EngineeringOverviewProps) {
  const { t } = useT("engineering");
  const { t: tProjects } = useT("projects");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const userId = useAuthStore((state) => state.user?.id);
  const projectQuery = useQuery(projectDetailOptions(wsId, projectId));
  const overviewQuery = useQuery(projectEngineeringOptions(wsId, projectId));
  const membersQuery = useQuery(memberListOptions(wsId));
  const overview = overviewQuery.data;
  const config = overview?.engineering;
  const permissions = useMemo(
    () => config ? engineeringPermissions(userId, membersQuery.data ?? [], projectQuery.data, config) : null,
    [config, userId, membersQuery.data, projectQuery.data],
  );
  const enabled = config?.enabled === true;
  const [selectedChapter, setSelectedChapter] = useState<BaselineChapter>("overview");
  const candidateId = navigation.searchParams.get("candidate") ?? "";
  const candidateQuery = useQuery({ ...engineeringCandidateOptions(wsId, projectId, candidateId), enabled: enabled && !!candidateId });
  useEffect(() => {
    if (candidateQuery.data) setSelectedChapter(candidateQuery.data.chapter);
  }, [candidateQuery.data]);

  const decisionQueries = useQueries({
    queries: (permissions?.deciderIds ?? []).map((deciderId) => ({
      ...projectEngineeringDecisionIssuesOptions(wsId, projectId, deciderId),
      enabled: enabled,
    })),
  });
  const decisionIssues = useMemo(() => {
    const byId = new Map();
    for (const query of decisionQueries) for (const issue of query.data ?? []) byId.set(issue.id, issue);
    return [...byId.values()].sort((a, b) => a.identifier.localeCompare(b.identifier, undefined, { numeric: true }));
  }, [decisionQueries]);
  const timelineQueries = useQueries({
    queries: decisionIssues.map((issue) => ({ ...issueTimelineOptions(issue.id), enabled })),
  });
  const timelines = useMemo(() => Object.fromEntries(decisionIssues.map((issue, index) => {
    const query = timelineQueries[index];
    const timeline: EngineeringDecisionTimeline = {
      comment: query?.data ? selectEngineeringComment(query.data) : null,
      loading: !!query?.isLoading,
      error: !!query?.isError,
    };
    return [issue.id, timeline];
  })), [decisionIssues, timelineQueries]);

  const graphSpec = useMemo<IssueTableQuerySpec>(() => ({
    scope: { kind: "project", project_id: projectId },
    filters: {},
    sort: { field: "position", direction: "asc" },
  }), [projectId]);
  const graphQuery = useDagGraph(wsId, graphSpec, enabled);
  const stageTitles = useMemo(() => engineeringStageTitles(overview?.chapters.find((chapter) => chapter.chapter === "overview")?.adopted?.contentMarkdown), [overview]);
  const chapterState = overview?.chapters.find((chapter) => chapter.chapter === selectedChapter);
  const [conflictRevision, setConflictRevision] = useState<BaselineRevision | null>(null);
  const deepLinkChapter = candidateQuery.data?.chapter ?? "overview";
  const showDeepLinkedRevision = !!candidateId && selectedChapter === deepLinkChapter;
  const queriedRevision: BaselineRevision | null = showDeepLinkedRevision ? candidateQuery.data?.revision ?? null : chapterState?.candidate ?? null;
  const revision = conflictRevision ?? queriedRevision;
  const currentCandidate = !conflictRevision && revision !== null && chapterState?.candidate?.id === revision.id && (!showDeepLinkedRevision || candidateQuery.data?.current === true);
  const [decisionResult, setDecisionResult] = useState<BaselineDecision | null>(null);
  const [decisionError, setDecisionError] = useState(false);
  useEffect(() => {
    setDecisionResult(null);
    setDecisionError(false);
    setConflictRevision(null);
  }, [candidateId, selectedChapter]);

  const historyQuery = useInfiniteQuery({
    queryKey: [...projectKeys.engineeringHistoryAll(wsId, projectId), selectedChapter, "infinite"] as const,
    queryFn: ({ pageParam }) => api.getProjectBaselineHistory(wsId, projectId, selectedChapter, { limit: 50, before: pageParam }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) => lastPage?.hasMore ? lastPage.nextBefore : undefined,
    enabled,
  });
  const history = useMemo(() => {
    if (!historyQuery.data) return historyQuery.data;
    const pages = historyQuery.data.pages.filter((page) => page !== null);
    const last = pages.at(-1);
    return { revisions: pages.flatMap((page) => page.revisions), hasMore: last?.hasMore ?? false, nextBefore: last?.nextBefore ?? null };
  }, [historyQuery.data]);
  const recentQueries = useQueries({
    queries: ENGINEERING_CHAPTERS.map((chapter) => ({
      ...projectBaselineHistoryOptions(wsId, projectId, chapter, { limit: 10 }),
      enabled,
    })),
  });
  const recentRevisions = useMemo(() => recentQueries
    .flatMap((query) => query.data?.revisions ?? [])
    .filter((revision) => revision.decidedAt !== null)
    .sort((a, b) => (b.decidedAt ?? "").localeCompare(a.decidedAt ?? "") || b.revisionNumber - a.revisionNumber)
    .slice(0, 5), [recentQueries]);

  const updateEngineering = useUpdateProjectEngineering();
  const adoptBaseline = useAdoptProjectBaseline();
  const rejectBaseline = useRejectProjectBaseline();
  const [configError, setConfigError] = useState(false);
  const saveConfig = async (data: { enabled?: boolean; currentStage?: number; modeSummary?: string; deciderMemberIds?: string[] }) => {
    setConfigError(false);
    try {
      const result = await updateEngineering.mutateAsync({ projectId, ...data });
      if (!result) throw new Error("Invalid engineering response");
      toast.success(t(($) => $.messages.configSaved));
    } catch {
      setConfigError(true);
    }
  };
  const decide = async (action: "adopt" | "reject", target: BaselineRevision) => {
    setDecisionError(false);
    setDecisionResult(null);
    setConflictRevision(null);
    const input = { projectId, revisions: [{ revisionId: target.id, expectedAdoptedRevisionId: target.baseRevisionId }] };
    try {
      const result = await (action === "adopt" ? adoptBaseline.mutateAsync(input) : rejectBaseline.mutateAsync(input));
      if (!result) throw new Error("Invalid decision response");
      setDecisionResult(result);
      toast.success(t(($) => action === "adopt" ? $.messages.adopted : $.messages.rejected));
    } catch {
      setConflictRevision(target);
      setDecisionError(true);
    }
  };

  const header = (
    <BreadcrumbHeader
      segments={[
        { href: paths.workspace(workspaceSlug).projects(), label: tProjects(($) => $.page.title) },
        { href: paths.workspace(workspaceSlug).projectDetail(projectId), label: projectQuery.data?.title ?? "…", className: "flex min-w-0 max-w-72 items-center truncate" },
      ]}
      leaf={<span className="truncate font-medium">{t(($) => $.title)}</span>}
    />
  );
  if (projectQuery.isLoading || overviewQuery.isLoading || membersQuery.isLoading) {
    return <div>{header}<div className="space-y-4 p-6"><Skeleton className="h-28 w-full" /><Skeleton className="h-72 w-full" /></div></div>;
  }
  if (projectQuery.isError || overviewQuery.isError || !overview || !config || !permissions) {
    return <div>{header}<CollectionPageState icon={AlertCircle} tone="destructive" title={t(($) => $.errors.title)} description={t(($) => $.errors.overview)} /></div>;
  }
  if (!enabled) {
    return (
      <div>{header}<CollectionPageState
        icon={Settings2}
        title={t(($) => $.disabled.title)}
        description={permissions.isAdmin ? t(($) => $.disabled.adminDescription) : t(($) => $.disabled.description)}
        actions={permissions.isAdmin ? <Button disabled={updateEngineering.isPending} onClick={() => saveConfig({ enabled: true })}>{updateEngineering.isPending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.actions.enable)}</Button> : undefined}
      />{configError ? <div className="p-4"><Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.configTitle)}</AlertTitle></Alert></div> : null}</div>
    );
  }

  const projectNodes = graphQuery.data?.nodes.filter((node) => node.projectId === projectId) ?? [];
  const blockedCount = projectNodes.filter((node) => node.status === "blocked" && node.statusCategory !== "done" && node.statusCategory !== "closed").length;
  return (
    <div className="min-w-0">
      {header}
      <main className="mx-auto max-w-[1600px] space-y-5 p-4 md:p-6">
        {configError ? <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.configTitle)}</AlertTitle><AlertDescription>{t(($) => $.errors.config)}</AlertDescription></Alert> : null}
        <section className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label={t(($) => $.summary.label)}>
          <Card><CardContent className="p-4"><div className="text-caption text-muted-foreground">{t(($) => $.summary.decisions)}</div><div className="mt-1 text-title-sm font-medium">{decisionIssues.length}</div></CardContent></Card>
          <Card><CardContent className="p-4"><div className="text-caption text-muted-foreground">{t(($) => $.summary.stage)}</div><div className="mt-1 text-title-sm font-medium">{stageTitles.get(config.currentStage) ?? t(($) => $.stage.defaults[String(config.currentStage) as "1" | "2" | "3" | "4" | "5"] ?? $.stage.defaults[1])}</div></CardContent></Card>
          <Card><CardContent className="p-4"><div className="text-caption text-muted-foreground">{t(($) => $.summary.blocked)}</div><div className="mt-1 text-title-sm font-medium">{blockedCount}</div></CardContent></Card>
          <Card><CardContent className="p-4"><div className="text-caption text-muted-foreground">{t(($) => $.summary.recent)}</div><div className="mt-1 text-title-sm font-medium">{recentRevisions[0] ? recentRevisions[0].changeSummary || t(($) => $.recent.noSummary) : t(($) => $.recent.empty)}</div></CardContent></Card>
        </section>
        <EngineeringStage
          currentStage={config.currentStage}
          modeSummary={config.modeSummary}
          stageTitles={stageTitles}
          canDecide={permissions.isDecider}
          pending={updateEngineering.isPending}
          onStageChange={(currentStage) => saveConfig({ currentStage })}
          onSummarySave={(modeSummary) => saveConfig({ modeSummary })}
        />
        {decisionQueries.some((query) => query.isError) ? <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.decisionIssues)}</AlertDescription></Alert> : null}
        {decisionQueries.some((query) => query.isLoading) ? <Skeleton className="h-52 w-full" /> : (
          <EngineeringDecisions workspaceSlug={workspaceSlug} issues={decisionIssues} timelines={timelines} />
        )}
        <div className="grid gap-5 xl:grid-cols-[minmax(0,1.45fr)_minmax(320px,0.75fr)]">
          <div className="min-w-0">
            {graphQuery.isLoading ? <Skeleton className="h-52 w-full" /> : graphQuery.isError ? (
              <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.graph)}</AlertDescription></Alert>
            ) : <EngineeringProgress workspaceSlug={workspaceSlug} workspaceId={wsId} nodes={projectNodes} stageTitles={stageTitles} />}
          </div>
          <EngineeringRecentChanges revisions={recentRevisions} loading={recentQueries.some((query) => query.isLoading)} error={recentQueries.some((query) => query.isError)} />
        </div>
        <EngineeringBaseline
          workspaceId={wsId}
          chapters={overview.chapters}
          selectedChapter={selectedChapter}
          onChapterChange={setSelectedChapter}
          revision={revision}
          currentCandidate={currentCandidate}
          candidateLoading={showDeepLinkedRevision && candidateQuery.isLoading}
          candidateNotFound={showDeepLinkedRevision && !candidateQuery.isLoading && !candidateQuery.isError && !revision}
          canDecide={permissions.isDecider}
          decisionPending={adoptBaseline.isPending || rejectBaseline.isPending}
          decisionError={decisionError || (showDeepLinkedRevision && candidateQuery.isError)}
          decisionResult={decisionResult}
          onDecision={decide}
          history={history}
          historyLoading={historyQuery.isLoading || historyQuery.isFetchingNextPage}
          historyError={historyQuery.isError}
          onHistoryMore={() => historyQuery.fetchNextPage()}
        />
        {permissions.isAdmin ? <EngineeringSettings config={config} members={membersQuery.data ?? []} pending={updateEngineering.isPending} onChange={saveConfig} /> : null}
        <div className="flex items-center gap-2 text-caption text-muted-foreground"><CheckCircle2 className="size-4" aria-hidden="true" />{t(($) => $.messages.serverAuthoritative)}</div>
      </main>
    </div>
  );
}
