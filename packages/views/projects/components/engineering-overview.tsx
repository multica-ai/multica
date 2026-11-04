"use client";

import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import { AlertCircle, LoaderCircle, Settings2 } from "@multica/ui/icons";
import { toast } from "sonner";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { api, type BaselineChapter, type BaselineDecision, type BaselineRevision, type EngineeringStage as EngineeringStageConfig } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useDagGraph } from "@multica/core/issues/dag/use-dag-data";
import { issueTimelineOptions } from "@multica/core/issues/queries";
import { paths } from "@multica/core/paths";
import { projectBaselineHistoryOptions, projectDetailOptions, projectEngineeringDecisionIssuesOptions, projectEngineeringOptions, projectKeys } from "@multica/core/projects/queries";
import { useAdoptProjectBaseline, useRejectProjectBaseline, useUpdateProjectEngineering } from "@multica/core/projects/mutations";
import { engineeringCandidateOptions } from "@multica/core/projects/engineering-candidate";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { Issue, IssueTableQuerySpec } from "@multica/core/types";
import { useT } from "../../i18n";
import { BreadcrumbHeader } from "../../layout/breadcrumb-header";
import { CollectionPageState } from "../../layout/collection-page";
import { useNavigation } from "../../navigation";
import { EngineeringBaseline } from "./engineering-baseline";
import { EngineeringDecisions, type EngineeringDecisionTimeline } from "./engineering-decisions";
import { EngineeringSettings } from "./engineering-settings";
import { EngineeringSkills } from "./engineering-skills";
import { EngineeringStage } from "./engineering-stage";
import { buildEngineeringRoadmap, ENGINEERING_CHAPTERS, engineeringPermissions, selectEngineeringComment, strixnovaSkillBindings } from "./engineering-utils";

interface EngineeringOverviewProps {
  workspaceSlug: string;
  projectId: string;
}

type EngineeringView = "overview" | "baseline";
type ConfigUpdate = {
  enabled?: boolean;
  currentStage?: number;
  modeSummary?: string;
  deciderMemberIds?: string[];
  stages?: EngineeringStageConfig[];
};

export function EngineeringOverview({ workspaceSlug, projectId }: EngineeringOverviewProps) {
  const { t } = useT("engineering");
  const { t: tProjects } = useT("projects");
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const userId = useAuthStore((state) => state.user?.id);
  const candidateId = navigation.searchParams.get("candidate") ?? "";
  const [activeView, setActiveView] = useState<EngineeringView>(candidateId ? "baseline" : "overview");
  const [manualCandidateId, setManualCandidateId] = useState("");
  const [selectedChapter, setSelectedChapter] = useState<BaselineChapter>("overview");
  const [selectedStage, setSelectedStage] = useState(1);
  const [reviewOpen, setReviewOpen] = useState(false);
  const projectQuery = useQuery(projectDetailOptions(wsId, projectId));
  const overviewQuery = useQuery(projectEngineeringOptions(wsId, projectId));
  const membersQuery = useQuery(memberListOptions(wsId));
  const overview = overviewQuery.data;
  const config = overview?.engineering;
  const enabled = config?.enabled === true;
  const permissions = useMemo(
    () => config ? engineeringPermissions(userId, membersQuery.data ?? [], projectQuery.data, config) : null,
    [config, userId, membersQuery.data, projectQuery.data],
  );
  const agentsQuery = useQuery({ ...agentListOptions(wsId), enabled: enabled && activeView === "overview" });
  const skillBindings = useMemo(() => strixnovaSkillBindings(agentsQuery.data ?? []), [agentsQuery.data]);

  useEffect(() => {
    if (candidateId) setActiveView("baseline");
    setManualCandidateId("");
  }, [candidateId]);
  const currentStage = config?.currentStage;
  useEffect(() => {
    if (currentStage) setSelectedStage(currentStage);
  }, [currentStage]);

  const candidateQuery = useQuery({
    ...engineeringCandidateOptions(wsId, projectId, candidateId),
    enabled: enabled && !!candidateId,
  });
  useEffect(() => {
    if (candidateQuery.data) setSelectedChapter(candidateQuery.data.chapter);
  }, [candidateQuery.data]);

  const decisionQueries = useQueries({
    queries: (permissions?.deciderIds ?? []).map((deciderId) => ({
      ...projectEngineeringDecisionIssuesOptions(wsId, projectId, deciderId),
      enabled,
    })),
  });
  const decisionIssues = useMemo(() => {
    const byId = new Map<string, Issue>();
    for (const query of decisionQueries) for (const issue of query.data ?? []) byId.set(issue.id, issue);
    return [...byId.values()].sort((a, b) => a.identifier.localeCompare(b.identifier, undefined, { numeric: true }));
  }, [decisionQueries]);
  const timelineQueries = useQueries({
    queries: decisionIssues.map((issue) => ({ ...issueTimelineOptions(issue.id), enabled })),
  });
  const timelines = useMemo(() => Object.fromEntries(decisionIssues.map((issue, index) => {
    const query = timelineQueries[index];
    const timeline: EngineeringDecisionTimeline = {
      comment: query?.data ? selectEngineeringComment(query.data, permissions?.deciderIds) : null,
      loading: !!query?.isLoading,
      error: !!query?.isError,
    };
    return [issue.id, timeline];
  })), [decisionIssues, timelineQueries, permissions?.deciderIds]);

  const graphSpec = useMemo<IssueTableQuerySpec>(() => ({
    scope: { kind: "project", project_id: projectId },
    filters: {},
    sort: { field: "position", direction: "asc" },
  }), [projectId]);
  const graphQuery = useDagGraph(wsId, graphSpec, enabled);
  const projectNodes = useMemo(() => graphQuery.data?.nodes.filter((node) => node.projectId === projectId) ?? [], [graphQuery.data, projectId]);
  const roadmap = useMemo(
    () => buildEngineeringRoadmap(projectNodes, config?.stages ?? [], config?.currentStage ?? 1),
    [projectNodes, config?.stages, config?.currentStage],
  );
  const pendingCandidates = useMemo(() => (overview?.chapters ?? []).flatMap((chapter) => chapter.candidate ? [chapter.candidate] : []), [overview?.chapters]);

  const chapterState = overview?.chapters.find((chapter) => chapter.chapter === selectedChapter);
  const [conflictRevision, setConflictRevision] = useState<BaselineRevision | null>(null);
  const [decidedRevision, setDecidedRevision] = useState<BaselineRevision | null>(null);
  const deepLinkChapter = candidateQuery.data?.chapter ?? "overview";
  const showDeepLinkedRevision = !manualCandidateId && !!candidateId && selectedChapter === deepLinkChapter;
  const manualRevision = manualCandidateId
    ? overview?.chapters.map((chapter) => chapter.candidate).find((candidate) => candidate?.id === manualCandidateId) ?? null
    : null;
  const queriedRevision: BaselineRevision | null = manualCandidateId
    ? manualRevision
    : showDeepLinkedRevision ? candidateQuery.data?.revision ?? null : chapterState?.candidate ?? null;
  const revision = conflictRevision ?? decidedRevision ?? queriedRevision;
  const currentCandidate = !conflictRevision && revision?.status === "candidate" && chapterState?.candidate?.id === revision.id
    && (manualCandidateId ? true : !showDeepLinkedRevision || candidateQuery.data?.current === true);
  const [decisionResult, setDecisionResult] = useState<BaselineDecision | null>(null);
  const [decisionError, setDecisionError] = useState(false);
  useEffect(() => {
    setDecisionResult(null);
    setDecisionError(false);
    setConflictRevision(null);
    setDecidedRevision(null);
  }, [candidateId, manualCandidateId, selectedChapter]);

  const historyQuery = useInfiniteQuery({
    queryKey: [...projectKeys.engineeringHistoryAll(wsId, projectId), selectedChapter, "infinite"] as const,
    queryFn: ({ pageParam }) => api.getProjectBaselineHistory(wsId, projectId, selectedChapter, { limit: 50, before: pageParam }),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) => lastPage?.hasMore ? lastPage.nextBefore : undefined,
    enabled: enabled && activeView === "baseline",
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
      enabled: enabled && activeView === "baseline",
    })),
  });
  const chapterRevisions = useMemo<Partial<Record<BaselineChapter, readonly BaselineRevision[]>>>(() => Object.fromEntries(
    ENGINEERING_CHAPTERS.map((chapter, index) => [chapter, recentQueries[index]?.data?.revisions ?? []]),
  ), [recentQueries]);

  const updateEngineering = useUpdateProjectEngineering();
  const adoptBaseline = useAdoptProjectBaseline();
  const rejectBaseline = useRejectProjectBaseline();
  const [configError, setConfigError] = useState(false);
  const saveConfig = async (data: ConfigUpdate): Promise<boolean> => {
    setConfigError(false);
    try {
      const result = await updateEngineering.mutateAsync({ projectId, ...data });
      if (!result) throw new Error("Invalid engineering response");
      toast.success(t(($) => $.messages.configSaved));
      return true;
    } catch {
      setConfigError(true);
      return false;
    }
  };
  const decide = async (action: "adopt" | "reject", target: BaselineRevision) => {
    setDecisionError(false);
    setDecisionResult(null);
    setConflictRevision(null);
    setDecidedRevision(null);
    const input = { projectId, revisions: [{ revisionId: target.id, expectedAdoptedRevisionId: target.baseRevisionId }] };
    try {
      const result = await (action === "adopt" ? adoptBaseline.mutateAsync(input) : rejectBaseline.mutateAsync(input));
      if (!result) throw new Error("Invalid decision response");
      setDecidedRevision({ ...target, status: action === "adopt" ? "adopted" : "rejected" });
      setDecisionResult(result);
      toast.success(t(($) => action === "adopt" ? $.messages.adopted : $.messages.rejected));
    } catch {
      setConflictRevision(target);
      setDecisionError(true);
    }
  };

  const clearCandidateParam = () => {
    if (!candidateId) return;
    const params = new URLSearchParams(navigation.searchParams);
    params.delete("candidate");
    const query = params.toString();
    navigation.replace(`${navigation.pathname}${query ? `?${query}` : ""}`);
  };
  const openOverview = () => {
    setManualCandidateId("");
    setActiveView("overview");
    clearCandidateParam();
  };
  const openBaseline = (id?: string) => {
    if (id) {
      const chapter = overview?.chapters.find((item) => item.candidate?.id === id);
      if (chapter) setSelectedChapter(chapter.chapter);
      setManualCandidateId(id);
    } else {
      setManualCandidateId("");
      const selectedCandidate = overview?.chapters.find((item) => item.chapter === selectedChapter)?.candidate;
      const firstCandidate = selectedCandidate ?? pendingCandidates[0];
      const chapter = overview?.chapters.find((item) => item.candidate?.id === firstCandidate?.id);
      if (chapter) setSelectedChapter(chapter.chapter);
    }
    setActiveView("baseline");
  };
  const openOverviewStage = (stage: number) => {
    setSelectedStage(stage);
    openOverview();
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
        actions={permissions.isAdmin ? <Button disabled={updateEngineering.isPending} onClick={() => void saveConfig({ enabled: true })}>{updateEngineering.isPending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.actions.enable)}</Button> : undefined}
      />{configError ? <div className="p-4"><Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.configTitle)}</AlertTitle></Alert></div> : null}</div>
    );
  }

  const currentSummary = roadmap.stages.find((stage) => stage.stage === config.currentStage);
  const waiting = (
    <EngineeringDecisions
      workspaceSlug={workspaceSlug}
      issues={decisionIssues}
      timelines={timelines}
      candidates={pendingCandidates}
      reviewStage={permissions.isDecider && currentSummary?.status === "review" ? config.currentStage : null}
      onOpenCandidate={openBaseline}
      onReview={() => {
        setSelectedStage(config.currentStage);
        setReviewOpen(true);
      }}
    />
  );
  return (
    <div className="min-w-0">
      {header}
      <main className={cn("mx-auto p-4 md:p-6", activeView === "overview" ? "max-w-5xl" : "max-w-7xl")}>
        <nav className="flex gap-5 border-b" role="tablist" aria-label={t(($) => $.views.label)}>
          {(["overview", "baseline"] as const).map((view) => (
            <button
              key={view}
              type="button"
              role="tab"
              aria-label={t(($) => $.views[view])}
              aria-selected={activeView === view}
              className={cn("relative -mb-px border-b-2 px-1 pb-2 pt-1 text-body", activeView === view ? "border-foreground font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground")}
              onClick={() => view === "overview" ? openOverview() : openBaseline()}
            >
              {t(($) => $.views[view])}
              {view === "baseline" && pendingCandidates.length ? <span className="absolute -right-2 top-1 size-1.5 rounded-full bg-destructive" aria-label={t(($) => $.views.pending)} /> : null}
            </button>
          ))}
        </nav>
        {configError ? <Alert className="mt-4" variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.configTitle)}</AlertTitle><AlertDescription>{t(($) => $.errors.config)}</AlertDescription></Alert> : null}
        {activeView === "overview" ? (
          <div className="mt-5 space-y-8">
            {decisionQueries.some((query) => query.isError) ? <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.decisionIssues)}</AlertDescription></Alert> : null}
            {graphQuery.isLoading ? <><Skeleton className="h-32 w-full" /><Skeleton className="h-72 w-full" /></> : graphQuery.isError ? (
              <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.graph)}</AlertDescription></Alert>
            ) : (
              <EngineeringStage
                workspaceSlug={workspaceSlug}
                workspaceId={wsId}
                roadmap={roadmap}
                configuredStages={config.stages}
                currentStage={config.currentStage}
                selectedStage={selectedStage}
                modeSummary={config.modeSummary}
                canDecide={permissions.isDecider}
                pending={updateEngineering.isPending}
                reviewOpen={reviewOpen}
                onReviewOpenChange={setReviewOpen}
                onSelectStage={setSelectedStage}
                onStageChange={async (currentStage) => saveConfig({ currentStage })}
                onStagesChange={async (stages) => saveConfig({ stages })}
                onSummarySave={async (modeSummary) => saveConfig({ modeSummary })}
                waiting={waiting}
              />
            )}
            <EngineeringSkills bindings={skillBindings} loading={agentsQuery.isLoading} error={agentsQuery.isError} />
            {permissions.isAdmin ? <EngineeringSettings config={config} members={membersQuery.data ?? []} pending={updateEngineering.isPending} onChange={(data) => void saveConfig(data)} /> : null}
            <div className="text-caption text-muted-foreground">{t(($) => $.messages.serverAuthoritative)}</div>
          </div>
        ) : (
          <EngineeringBaseline
            workspaceSlug={workspaceSlug}
            workspaceId={wsId}
            projectId={projectId}
            projectTitle={projectQuery.data?.title ?? ""}
            chapters={overview.chapters}
            selectedChapter={selectedChapter}
            onChapterChange={setSelectedChapter}
            revision={revision}
            currentCandidate={currentCandidate}
            candidateLoading={showDeepLinkedRevision && candidateQuery.isLoading}
            candidateNotFound={(showDeepLinkedRevision || !!manualCandidateId) && !candidateQuery.isLoading && !candidateQuery.isError && !revision}
            canDecide={permissions.isDecider}
            decisionPending={adoptBaseline.isPending || rejectBaseline.isPending}
            decisionError={decisionError || (showDeepLinkedRevision && candidateQuery.isError)}
            decisionResult={decisionResult}
            onDecision={decide}
            history={history}
            historyLoading={historyQuery.isLoading || historyQuery.isFetchingNextPage}
            historyError={historyQuery.isError}
            onHistoryMore={() => historyQuery.fetchNextPage()}
            chapterRevisions={chapterRevisions}
            roadmap={roadmap}
            reviewRequested={!!manualCandidateId || !!candidateId}
            onOpenStage={openOverviewStage}
          />
        )}
      </main>
    </div>
  );
}
