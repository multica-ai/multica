"use client";

import { AlertTriangle, CheckCircle2, History as FileClock, LoaderCircle, XCircle } from "@multica/ui/icons";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import type { BaselineChange, BaselineChapter, BaselineDecision, BaselineDiff, BaselineHistory, BaselineRevision, ChapterState, OpenIssue } from "@multica/core/api";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
import { RichContent } from "../../rich-content";
import { revisionConflictRows } from "../../issues/components/revision-conflict-compare";
import { useStatusLabel } from "../../issues/utils/status-label";
import { ENGINEERING_CHAPTERS } from "./engineering-utils";

type DecisionAction = "adopt" | "reject";

interface EngineeringBaselineProps {
  workspaceId: string;
  chapters: readonly ChapterState[];
  selectedChapter: BaselineChapter;
  onChapterChange: (chapter: BaselineChapter) => void;
  revision: BaselineRevision | null;
  currentCandidate: boolean;
  candidateLoading: boolean;
  candidateNotFound: boolean;
  canDecide: boolean;
  decisionPending: boolean;
  decisionError: boolean;
  decisionResult: BaselineDecision | null;
  onDecision: (action: DecisionAction, revision: BaselineRevision) => void;
  history: BaselineHistory | null | undefined;
  historyLoading: boolean;
  historyError: boolean;
  onHistoryMore: () => void;
}

function BaselineLineDiff({ change }: { change: BaselineChange }) {
  const { t } = useT("engineering");
  const rows = revisionConflictRows(change.before, change.after);
  return (
    <div className="overflow-hidden rounded-md border bg-background">
      <div className="border-b bg-muted/40 px-3 py-2 font-medium">{change.anchor} · {change.title}</div>
      <div className="grid grid-cols-2 border-b bg-muted/30 text-caption text-muted-foreground">
        <div className="px-3 py-1.5">{t(($) => $.baseline.adoptedLabel)}</div>
        <div className="border-l px-3 py-1.5">{t(($) => $.baseline.candidateLabel)}</div>
      </div>
      <div className="max-h-72 overflow-y-auto">
        {rows.map((row, index) => (
          <div key={index} className="grid grid-cols-2 border-b last:border-b-0">
            {(["server", "local"] as const).map((side) => {
              const line = side === "server" ? row.server : row.local;
              return (
                <div key={side} className={cn("grid min-w-0 grid-cols-[1.25rem_minmax(0,1fr)] px-2 py-1 font-mono text-micro", side === "local" && "border-l", line?.changed && side === "server" && "bg-destructive/10 text-destructive", line?.changed && side === "local" && "bg-success/10 text-success")}>
                  <span className="select-none text-center opacity-70">{line?.changed ? side === "server" ? "−" : "+" : ""}</span>
                  <span className="whitespace-pre-wrap break-words">{line?.text || " "}</span>
                </div>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function BaselineDiffView({ diff }: { diff: BaselineDiff }) {
  const { t } = useT("engineering");
  const changes = [
    ...diff.added.map((change) => ({ change, kind: "added" as const })),
    ...diff.modified.map((change) => ({ change, kind: "modified" as const })),
    ...diff.removed.map((change) => ({ change, kind: "removed" as const })),
  ];
  return (
    <div className="space-y-3">
      {diff.warnings.length ? (
        <Alert variant="destructive" data-baseline-warnings>
          <AlertTriangle />
          <AlertTitle>{t(($) => $.baseline.warningTitle)}</AlertTitle>
          <AlertDescription>
            <ul className="list-disc pl-5">
              {diff.warnings.map((warning, index) => <li key={`${warning.code}-${warning.anchor}-${index}`}>{t(($) => $.warnings[warning.code] ?? $.warnings.unknown, { anchor: warning.anchor })}</li>)}
            </ul>
          </AlertDescription>
        </Alert>
      ) : null}
      <div className="text-caption text-muted-foreground">{t(($) => $.baseline.unchanged, { count: diff.unchangedCount })}</div>
      {changes.length ? changes.map(({ change, kind }) => (
        <section key={`${kind}-${change.anchor}`} data-diff-kind={kind}>
          <Badge variant={kind === "removed" ? "destructive" : "secondary"} className="mb-2">{t(($) => $.baseline[kind])}</Badge>
          <BaselineLineDiff change={change} />
        </section>
      )) : <div className="rounded-md border border-dashed p-6 text-center text-muted-foreground">{t(($) => $.baseline.noChanges)}</div>}
    </div>
  );
}

function OpenIssueGroup({ title, issues, workspaceId }: { title: string; issues: readonly OpenIssue[]; workspaceId: string }) {
  const statusLabel = useStatusLabel(workspaceId);
  return (
    <div>
      <h4 className="mb-2 font-medium">{title}</h4>
      {issues.length ? <ul className="space-y-1 text-caption">{issues.map((issue) => <li key={issue.id} className="flex gap-2"><span className="text-muted-foreground">{issue.identifier}</span><span className="min-w-0 flex-1">{issue.title}</span><span className="text-muted-foreground">{statusLabel(issue.status)}</span></li>)}</ul> : <div className="text-caption text-muted-foreground">—</div>}
    </div>
  );
}

function DecisionOpenIssues({ result, workspaceId }: { result: BaselineDecision; workspaceId: string }) {
  const { t } = useT("engineering");
  return (
    <div className="space-y-3 rounded-lg border bg-success/5 p-3" data-open-issues>
      <div className="flex items-center gap-2 font-medium text-success"><CheckCircle2 className="size-4" aria-hidden="true" />{t(($) => $.baseline.openIssuesTitle)}</div>
      <OpenIssueGroup title={t(($) => $.baseline.inFlight)} issues={result.openIssues.inFlight} workspaceId={workspaceId} />
      <OpenIssueGroup title={t(($) => $.baseline.queued)} issues={result.openIssues.queued} workspaceId={workspaceId} />
      {result.openIssues.truncated ? <div className="text-caption text-muted-foreground">{t(($) => $.baseline.openIssuesTruncated)}</div> : null}
    </div>
  );
}

function BaselineHistoryList({ history, loading, error, onMore }: { history: BaselineHistory | null | undefined; loading: boolean; error: boolean; onMore: () => void }) {
  const { t } = useT("engineering");
  const { getActorName } = useActorName();
  return (
    <section className="space-y-3 border-t pt-4">
      <h3 className="flex items-center gap-2 font-medium"><FileClock className="size-4" aria-hidden="true" />{t(($) => $.history.title)}</h3>
      {error ? <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.history)}</AlertDescription></Alert> : null}
      {loading && !history ? <Skeleton className="h-24 w-full" /> : null}
      {history && history.revisions.length === 0 ? <div className="text-caption text-muted-foreground">{t(($) => $.history.empty)}</div> : null}
      <ul className="space-y-2">
        {history?.revisions.map((revision) => (
          <li key={revision.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-2 text-caption">
            <span>{t(($) => $.history.revision, { number: revision.revisionNumber })}</span>
            <span className="flex items-center gap-2">
              <Badge variant="outline">{t(($) => $.status[revision.status] ?? $.status.unknown)}</Badge>
              {revision.decisionCredential ? <Badge variant="secondary">{t(($) => $.credentials[revision.decisionCredential!] ?? $.credentials.unknown)}</Badge> : null}
              {revision.decidedByType && revision.decidedById ? <span className="text-muted-foreground">{getActorName(revision.decidedByType, revision.decidedById)}</span> : null}
            </span>
          </li>
        ))}
      </ul>
      {history?.hasMore ? <Button variant="outline" size="sm" onClick={onMore} disabled={loading}>{loading ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.history.more)}</Button> : null}
    </section>
  );
}

export function EngineeringBaseline(props: EngineeringBaselineProps) {
  const { t } = useT("engineering");
  const chapter = props.chapters.find((item) => item.chapter === props.selectedChapter);
  const revision = props.revision;
  return (
    <Card data-engineering-region="baseline">
      <CardHeader><CardTitle>{t(($) => $.baseline.title)}</CardTitle></CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap gap-2" role="tablist" aria-label={t(($) => $.baseline.chaptersLabel)}>
          {ENGINEERING_CHAPTERS.map((key) => (
            <Button key={key} type="button" role="tab" aria-selected={props.selectedChapter === key} size="sm" variant={props.selectedChapter === key ? "brand" : "outline"} onClick={() => props.onChapterChange(key)}>
              {t(($) => $.chapters[key])}
            </Button>
          ))}
        </div>
        {!chapter ? <Alert variant="destructive"><XCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.chapter)}</AlertDescription></Alert> : (
          <div className="grid gap-5 xl:grid-cols-2">
            <section className="min-w-0">
              <div className="mb-3 flex items-center justify-between gap-2">
                <h3 className="font-medium">{t(($) => $.baseline.adoptedTitle)}</h3>
                {chapter.adopted ? <Badge variant="outline">{t(($) => $.history.revision, { number: chapter.adopted.revisionNumber })}</Badge> : null}
              </div>
              {chapter.adopted ? <RichContent content={chapter.adopted.contentMarkdown} /> : <CollectionPageState icon={FileClock} title={t(($) => $.baseline.noAdopted)} />}
            </section>
            <section className="min-w-0">
              <div className="mb-3 flex items-center justify-between gap-2">
                <h3 className="font-medium">{t(($) => $.baseline.candidateTitle)}</h3>
                {revision ? <span className="flex gap-2"><Badge variant="outline">{t(($) => $.history.revision, { number: revision.revisionNumber })}</Badge><Badge variant={revision.status === "candidate" ? "default" : "secondary"}>{t(($) => $.status[revision.status] ?? $.status.unknown)}</Badge></span> : null}
              </div>
              {props.candidateLoading ? <Skeleton className="h-72 w-full" /> : null}
              {!props.candidateLoading && props.decisionError ? <Alert variant="destructive"><AlertTriangle /><AlertTitle>{t(($) => $.errors.decisionTitle)}</AlertTitle><AlertDescription>{t(($) => $.errors.decision)}</AlertDescription></Alert> : null}
              {!props.candidateLoading && props.candidateNotFound ? <CollectionPageState icon={XCircle} tone="warning" title={t(($) => $.baseline.candidateMissing)} description={t(($) => $.baseline.candidateMissingDescription)} /> : null}
              {!props.candidateLoading && !props.candidateNotFound && !revision && !props.decisionResult && !props.decisionError ? <CollectionPageState icon={CheckCircle2} title={t(($) => $.baseline.noCandidate)} /> : null}
              {!props.candidateLoading && revision ? (
                <div className="space-y-4" data-baseline-revision-status={revision.status}>
                  {revision.changeSummary ? <p className="text-caption text-muted-foreground">{revision.changeSummary}</p> : null}
                  <BaselineDiffView diff={revision.anchorsDiff} />
                  {props.currentCandidate && props.canDecide ? (
                    <div className="flex flex-wrap justify-end gap-2">
                      <Button variant="outline" disabled={props.decisionPending} onClick={() => props.onDecision("reject", revision)}>{t(($) => $.actions.reject)}</Button>
                      <Button disabled={props.decisionPending} aria-busy={props.decisionPending} onClick={() => props.onDecision("adopt", revision)}>{props.decisionPending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.actions.adopt)}</Button>
                    </div>
                  ) : null}
                  {!props.currentCandidate ? <div className="text-caption text-muted-foreground">{t(($) => $.baseline.readOnlyRevision)}</div> : null}
                </div>
              ) : null}
              {props.decisionResult ? <div className="mt-4"><DecisionOpenIssues result={props.decisionResult} workspaceId={props.workspaceId} /></div> : null}
            </section>
          </div>
        )}
        <BaselineHistoryList history={props.history} loading={props.historyLoading} error={props.historyError} onMore={props.onHistoryMore} />
      </CardContent>
    </Card>
  );
}
