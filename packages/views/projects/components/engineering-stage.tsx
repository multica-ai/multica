"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ArrowDown, ArrowUp, Check, LoaderCircle, Plus } from "@multica/ui/icons";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import type { EngineeringStage as EngineeringStageConfig } from "@multica/core/api";
import { useT } from "../../i18n";
import { EngineeringProgress } from "./engineering-progress";
import type { EngineeringRoadmap, EngineeringStageSummary } from "./engineering-utils";

interface EngineeringStageProps {
  workspaceSlug: string;
  workspaceId: string;
  roadmap: EngineeringRoadmap;
  configuredStages: readonly EngineeringStageConfig[];
  currentStage: number;
  selectedStage: number;
  modeSummary: string;
  canDecide: boolean;
  pending: boolean;
  reviewOpen: boolean;
  onReviewOpenChange: (open: boolean) => void;
  onSelectStage: (stage: number) => void;
  onStageChange: (stage: number) => Promise<boolean>;
  onStagesChange: (stages: EngineeringStageConfig[]) => Promise<boolean>;
  onSummarySave: (summary: string) => Promise<boolean>;
  waiting?: ReactNode;
}

export function EngineeringStage({
  workspaceSlug,
  workspaceId,
  roadmap,
  configuredStages,
  currentStage,
  selectedStage,
  modeSummary,
  canDecide,
  pending,
  reviewOpen,
  onReviewOpenChange,
  onSelectStage,
  onStageChange,
  onStagesChange,
  onSummarySave,
  waiting,
}: EngineeringStageProps) {
  const { t } = useT("engineering");
  const [summary, setSummary] = useState(modeSummary);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const [summaryEditing, setSummaryEditing] = useState(false);
  const [editing, setEditing] = useState<{ stage: number; title: string; goal: string; isNew: boolean } | null>(null);
  useEffect(() => setSummary(modeSummary), [modeSummary]);

  const selected = roadmap.stages.find((stage) => stage.stage === selectedStage)
    ?? roadmap.stages.find((stage) => stage.stage === currentStage)
    ?? roadmap.stages[0];
  const current = roadmap.stages.find((stage) => stage.stage === currentStage) ?? selected;
  const stageTitle = (stage: EngineeringStageSummary | undefined) => stage?.title || t(($) => $.stage.default, { number: stage?.stage ?? currentStage });
  const earlierStages = roadmap.stages.filter((stage) => current && stage.stage < current.stage);
  const maxConfiguredStage = Math.max(0, ...configuredStages.map((stage) => stage.stage));
  const allComplete = !!current && current.total === 0 && earlierStages.length > 0
    && current.stage > maxConfiguredStage
    && !configuredStages.some((stage) => stage.stage === current.stage)
    && earlierStages.every((stage) => stage.total === stage.closed);
  const nextStage = Math.max(currentStage, ...roadmap.stages.map((stage) => stage.stage)) + 1;
  const currentTitle = stageTitle(current);
  const statusLabels = useMemo(() => ({
    completed: t(($) => $.stage.status.completed),
    review: t(($) => $.stage.status.review),
    current: t(($) => $.stage.status.current),
    locked: t(($) => $.stage.status.locked),
  }), [t]);

  if (!selected || !current) return null;

  const saveStage = async () => {
    if (!editing?.title.trim()) return;
    const next = editing.isNew
      ? [...configuredStages, { stage: editing.stage, title: editing.title.trim(), goal: editing.goal.trim() }]
      : configuredStages.some((stage) => stage.stage === editing.stage)
        ? configuredStages.map((stage) => stage.stage === editing.stage ? { ...stage, title: editing.title.trim(), goal: editing.goal.trim() } : stage)
        : [...configuredStages, { stage: editing.stage, title: editing.title.trim(), goal: editing.goal.trim() }];
    next.sort((a, b) => a.stage - b.stage);
    if (await onStagesChange(next)) setEditing(null);
  };
  const sortedConfiguredStages = [...configuredStages].sort((a, b) => a.stage - b.stage);
  const selectedConfigIndex = sortedConfiguredStages.findIndex((stage) => stage.stage === selected.stage);
  const canMoveStage = (direction: -1 | 1) => {
    const target = sortedConfiguredStages[selectedConfigIndex + direction];
    const targetSummary = target ? roadmap.stages.find((stage) => stage.stage === target.stage) : undefined;
    return selectedConfigIndex >= 0 && selected.stage > currentStage && selected.total === 0
      && !!targetSummary && targetSummary.stage > currentStage && targetSummary.total === 0;
  };
  const moveStage = async (direction: -1 | 1) => {
    const targetIndex = selectedConfigIndex + direction;
    if (!canMoveStage(direction)) return;
    const selectedNumber = sortedConfiguredStages[selectedConfigIndex]!.stage;
    const targetNumber = sortedConfiguredStages[targetIndex]!.stage;
    const next = sortedConfiguredStages.map((stage) => ({
      ...stage,
      stage: stage.stage === selectedNumber ? targetNumber : stage.stage === targetNumber ? selectedNumber : stage.stage,
    })).sort((a, b) => a.stage - b.stage);
    if (await onStagesChange(next)) onSelectStage(targetNumber);
  };
  const advance = async () => {
    if (await onStageChange(current.stage + 1)) {
      onReviewOpenChange(false);
      onSelectStage(current.stage + 1);
    }
  };

  return (
    <div className="space-y-8">
      <section aria-label={t(($) => $.stage.summaryLabel)}>
        {summaryEditing ? (
          <div className="max-w-2xl space-y-2">
            <Textarea aria-label={t(($) => $.stage.summaryLabel)} value={summary} onChange={(event) => setSummary(event.target.value)} maxLength={280} rows={3} disabled={pending} autoFocus />
            <div className="flex gap-2">
              <Button size="sm" disabled={pending} onClick={async () => { if (await onSummarySave(summary.trim())) setSummaryEditing(false); }}>
                {pending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.actions.save)}
              </Button>
              <Button size="sm" variant="ghost" disabled={pending} onClick={() => { setSummary(modeSummary); setSummaryEditing(false); }}>{t(($) => $.actions.cancel)}</Button>
            </div>
          </div>
        ) : (
          <div className="max-w-2xl">
            <p className={cn("whitespace-pre-wrap text-body text-muted-foreground", !summaryOpen && "line-clamp-2")}>{summary || t(($) => $.stage.summaryEmpty)}</p>
            <div className="mt-1 flex gap-3">
              {summary.length > 120 || summary.includes("\n") ? <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setSummaryOpen((value) => !value)}>{summaryOpen ? t(($) => $.actions.collapse) : t(($) => $.actions.expand)}</Button> : null}
              {canDecide ? <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setSummaryEditing(true)}>{t(($) => $.stage.editSummary)}</Button> : null}
            </div>
          </div>
        )}
      </section>

      <section className="pt-4" aria-live="polite">
        {allComplete ? (
          <h1 className="max-w-3xl text-title-lg font-semibold leading-snug">{t(($) => $.hero.allComplete)} <span className="font-normal text-muted-foreground">{t(($) => $.hero.allCompleteHint)}</span></h1>
        ) : current.status === "review" ? (
          <h1 className="max-w-3xl text-title-lg font-semibold leading-snug">{t(($) => $.hero.review, { stage: current.stage, count: current.total })} <span className="font-normal text-muted-foreground">{t(($) => $.hero.reviewHint)}</span></h1>
        ) : current.total === 0 && earlierStages.length === 0 ? (
          <h1 className="max-w-3xl text-title-lg font-semibold leading-snug">{t(($) => $.hero.empty, { stage: current.stage })} <span className="font-normal text-muted-foreground">{t(($) => $.hero.emptyHint)}</span></h1>
        ) : (
          <h1 className="max-w-3xl text-title-lg font-semibold leading-snug">
            {t(($) => $.hero.current, { stage: current.stage, title: currentTitle, closed: current.closed, total: current.total })} <span className="font-normal text-muted-foreground">{current.blocked ? t(($) => $.hero.blocked, { count: current.blocked }) : t(($) => $.hero.unblocked)}</span>
          </h1>
        )}
        {current.status === "review" && canDecide ? <Button className="mt-4" onClick={() => onReviewOpenChange(true)}>{t(($) => $.stage.reviewAction, { stage: current.stage })}</Button> : null}
      </section>

      {waiting}

      <section id="engineering-roadmap" className="scroll-mt-24" aria-label={t(($) => $.stage.flowLabel)}>
        <div className="mb-3 flex items-center justify-between gap-3">
          <h2 className="text-caption font-medium uppercase tracking-wide text-muted-foreground">{t(($) => $.stage.flowLabel)}</h2>
          {canDecide ? (
            <Button variant="ghost" size="sm" disabled={pending || editing?.isNew} onClick={() => {
              onSelectStage(nextStage);
              setEditing({ stage: nextStage, title: "", goal: "", isNew: true });
            }}><Plus aria-hidden="true" />{t(($) => $.stage.add)}</Button>
          ) : null}
        </div>
        <ol className="flex snap-x overflow-x-auto pb-3" aria-label={t(($) => $.stage.flowLabel)}>
          {roadmap.stages.map((stage) => {
            const percent = stage.total ? Math.round(stage.closed / stage.total * 100) : 0;
            const viewing = stage.stage === selected.stage;
            return (
              <li key={stage.stage} className="min-w-40 flex-1 snap-start">
                <button
                  type="button"
                  className={cn("relative w-full border-t-2 px-1 pb-2 pt-5 text-left before:absolute before:left-0 before:top-[-6px] before:size-2.5 before:rounded-full before:border-2 before:bg-background before:content-['']", stage.status === "completed" ? "border-foreground before:border-foreground before:bg-foreground" : stage.status === "review" ? "border-destructive before:border-destructive before:bg-destructive" : viewing ? "border-foreground before:border-foreground" : "border-border before:border-border", viewing && "font-medium")}
                  aria-current={viewing ? "step" : undefined}
                  data-stage-node={stage.stage}
                  data-stage-status={stage.status}
                  onClick={() => { setEditing(null); onSelectStage(stage.stage); }}
                >
                  {stage.status === "current" || stage.status === "review" ? <span className="absolute left-0 top-[-2px] h-0.5 bg-foreground" style={{ width: `${percent}%` }} /> : null}
                  <span className="block text-caption text-muted-foreground">{t(($) => $.stage.label, { number: stage.stage })}</span>
                  <span className={cn("mt-0.5 block truncate text-body", viewing ? "text-foreground underline underline-offset-4" : "text-muted-foreground")} title={stageTitle(stage)}>{stageTitle(stage)}</span>
                  <span className="mt-1 block text-caption text-muted-foreground tabular-nums">
                    {stage.closed}/{stage.total} · {statusLabels[stage.status]}
                    {stage.status === "completed" && stage.total > stage.closed ? ` · ${t(($) => $.stage.leftover, { count: stage.total - stage.closed })}` : ""}
                  </span>
                </button>
              </li>
            );
          })}
          {editing?.isNew ? <li className="min-w-40 flex-1"><div className="border-t-2 border-foreground px-1 pb-2 pt-5"><span className="block text-caption text-muted-foreground">{t(($) => $.stage.label, { number: editing.stage })}</span><span className="mt-0.5 block text-body">{t(($) => $.stage.newStage)}</span></div></li> : null}
        </ol>

        <section className="mt-5 border-t pt-5" data-engineering-region="stage" data-stage={selected.stage}>
          {editing ? (
            <div className="max-w-xl space-y-3">
              <div className="text-caption text-muted-foreground">{t(($) => $.stage.label, { number: editing.stage })}</div>
              <Input aria-label={t(($) => $.stage.nameLabel)} value={editing.title} maxLength={80} disabled={pending} autoFocus onChange={(event) => setEditing({ ...editing, title: event.target.value })} />
              <Textarea aria-label={t(($) => $.stage.goalLabel)} value={editing.goal} maxLength={280} rows={3} disabled={pending} onChange={(event) => setEditing({ ...editing, goal: event.target.value })} />
              <div className="flex gap-2">
                <Button size="sm" disabled={pending || !editing.title.trim()} onClick={saveStage}>{pending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.actions.save)}</Button>
                <Button size="sm" variant="ghost" disabled={pending} onClick={() => setEditing(null)}>{t(($) => $.actions.cancel)}</Button>
              </div>
            </div>
          ) : (
            <div>
              <div className="text-caption text-muted-foreground">{t(($) => $.stage.label, { number: selected.stage })}</div>
              <h3 className="mt-1 text-title font-semibold">{stageTitle(selected)}</h3>
              {selected.goal ? <p className="mt-1 max-w-2xl whitespace-pre-wrap text-body text-muted-foreground">{selected.goal}</p> : null}
              <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-muted-foreground">
                <span>{statusLabels[selected.status]}</span>
                <span className="tabular-nums">{t(($) => $.stage.completedCount, { closed: selected.closed, total: selected.total })}</span>
                {canDecide ? <Button variant="link" size="sm" className="h-auto p-0" onClick={() => setEditing({ stage: selected.stage, title: selected.title, goal: selected.goal, isNew: false })}>{t(($) => $.stage.edit)}</Button> : null}
                {canDecide && selectedConfigIndex >= 0 && sortedConfiguredStages.length > 1 ? <span className="inline-flex flex-wrap items-center gap-1" data-reorder-hint={t(($) => $.stage.reorderHint)}>
                  <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.stage.moveEarlier)} disabled={pending || selectedConfigIndex === 0 || !canMoveStage(-1)} onClick={() => void moveStage(-1)}><ArrowUp aria-hidden="true" /></Button>
                  <Button variant="ghost" size="icon-xs" aria-label={t(($) => $.stage.moveLater)} disabled={pending || selectedConfigIndex === sortedConfiguredStages.length - 1 || !canMoveStage(1)} onClick={() => void moveStage(1)}><ArrowDown aria-hidden="true" /></Button>
                  <span className="ml-1">{t(($) => $.stage.reorderHint)}</span>
                </span> : null}
                {canDecide && selected.stage !== currentStage ? <Button variant="link" size="sm" className="h-auto p-0" disabled={pending} onClick={() => void onStageChange(selected.stage)}>{t(($) => $.stage.makeCurrent)}</Button> : null}
              </div>
            </div>
          )}
          {editing?.isNew ? null : <div className="mt-4">
            <EngineeringProgress workspaceSlug={workspaceSlug} workspaceId={workspaceId} summary={selected} outside={roadmap.outside} />
          </div>}
        </section>
      </section>

      <AlertDialog open={reviewOpen} onOpenChange={onReviewOpenChange}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.stage.reviewDialogTitle, { stage: current.stage })}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.stage.reviewDialogDescription, { title: currentTitle, count: current.total, next: current.stage + 1 })}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center gap-2 rounded-md border p-3 text-body"><Check className="size-4 text-success" aria-hidden="true" />{t(($) => $.stage.reviewTasksComplete, { closed: current.closed, total: current.total })}</div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>{t(($) => $.actions.cancel)}</AlertDialogCancel>
            <AlertDialogAction disabled={pending} onClick={(event) => { event.preventDefault(); void advance(); }}>{pending ? <LoaderCircle className="animate-spin" aria-hidden="true" /> : null}{t(($) => $.stage.confirmAdvance, { next: current.stage + 1 })}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
