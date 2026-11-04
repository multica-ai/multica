"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import type { IssueGraphNode } from "@multica/core/api";
import { paths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { useStatusLabel } from "../../issues/utils/status-label";
import { isClosedEngineeringNode, type EngineeringStageSummary } from "./engineering-utils";

interface EngineeringProgressProps {
  workspaceSlug: string;
  workspaceId: string;
  summary: EngineeringStageSummary;
  outside: readonly IssueGraphNode[];
}

function EngineeringTaskRow({ node, workspaceSlug, workspaceId }: {
  node: IssueGraphNode;
  workspaceSlug: string;
  workspaceId: string;
}) {
  const statusLabel = useStatusLabel(workspaceId);
  const { getActorName } = useActorName();
  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-start gap-3 border-b border-border/70 py-2.5 last:border-b-0" data-engineering-task={node.identifier}>
      <AppLink href={paths.workspace(workspaceSlug).issueDetail(node.id)} className="min-w-0 text-body hover:underline">
        <span className="mr-2 font-mono text-caption text-muted-foreground">{node.identifier}</span>
        <span className={isClosedEngineeringNode(node) ? "text-muted-foreground" : undefined}>{node.title}</span>
      </AppLink>
      <span className="flex shrink-0 items-center gap-2 text-caption text-muted-foreground">
        {node.assignee ? <span className="hidden sm:inline">{getActorName(node.assignee.type, node.assignee.id)}</span> : null}
        <span>{statusLabel(node.status)}</span>
      </span>
    </li>
  );
}

export function EngineeringProgress({ workspaceSlug, workspaceId, summary, outside }: EngineeringProgressProps) {
  const { t } = useT("engineering");
  const [showCompleted, setShowCompleted] = useState(false);
  const [showOutside, setShowOutside] = useState(false);
  const open = summary.tasks.filter((node) => !isClosedEngineeringNode(node));
  const completed = summary.tasks.filter(isClosedEngineeringNode);
  const groups = [
    { key: "blocked", label: t(($) => $.progress.blockedGroup), tasks: open.filter((node) => node.status === "blocked") },
    { key: "review", label: t(($) => $.progress.reviewGroup), tasks: open.filter((node) => node.status === "in_review") },
    { key: "progress", label: t(($) => $.progress.progressGroup), tasks: open.filter((node) => node.statusCategory === "started" && node.status !== "blocked" && node.status !== "in_review") },
    { key: "todo", label: t(($) => $.progress.todoGroup), tasks: open.filter((node) => node.statusCategory !== "started" && node.status !== "blocked" && node.status !== "in_review") },
  ];
  return (
    <div className="space-y-5">
      <div data-engineering-region="progress" data-stage={summary.stage}>
        {groups.map((group) => group.tasks.length ? (
          <section key={group.key} className="mt-5" data-task-group={group.key}>
            <h4 className={group.key === "blocked" ? "text-caption font-medium text-destructive" : "text-caption font-medium text-muted-foreground"}>
              {group.label} <span className="ml-1 tabular-nums">{group.tasks.length}</span>
            </h4>
            <ul className="mt-1">
              {group.tasks.map((node) => <EngineeringTaskRow key={node.id} node={node} workspaceSlug={workspaceSlug} workspaceId={workspaceId} />)}
            </ul>
          </section>
        ) : null)}
        {summary.total === 0 ? <p className="text-body text-muted-foreground">{t(($) => $.progress.emptyStage)}</p> : null}
        {completed.length ? (
          <div className="mt-5">
            <Button variant="ghost" size="sm" className="-ml-2 text-muted-foreground" aria-expanded={showCompleted} onClick={() => setShowCompleted((value) => !value)}>
              {showCompleted ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
              {showCompleted ? t(($) => $.progress.hideCompleted, { count: completed.length }) : t(($) => $.progress.showCompleted, { count: completed.length })}
            </Button>
            {showCompleted ? (
              <ul data-completed-tasks>
                {completed.map((node) => <EngineeringTaskRow key={node.id} node={node} workspaceSlug={workspaceSlug} workspaceId={workspaceId} />)}
              </ul>
            ) : null}
          </div>
        ) : null}
      </div>
      <section className="border-t pt-5" data-engineering-region="outside">
        <Button variant="ghost" size="sm" className="-ml-2 w-full justify-start" aria-expanded={showOutside} onClick={() => setShowOutside((value) => !value)}>
          {showOutside ? <ChevronDown aria-hidden="true" /> : <ChevronRight aria-hidden="true" />}
          <span className="font-medium">{t(($) => $.progress.outside)}</span>
          <Badge variant="secondary">{outside.length}</Badge>
        </Button>
        <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.progress.outsideHint)}</p>
        {showOutside ? (
          outside.length ? (
            <ul className="mt-3" data-outside-tasks>
              {outside.map((node) => <EngineeringTaskRow key={node.id} node={node} workspaceSlug={workspaceSlug} workspaceId={workspaceId} />)}
            </ul>
          ) : <p className="mt-3 text-body text-muted-foreground">{t(($) => $.progress.outsideEmpty)}</p>
        ) : null}
      </section>
    </div>
  );
}
