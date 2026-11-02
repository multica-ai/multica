"use client";

import { AlertCircle } from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { paths } from "@multica/core/paths";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { CollectionPageState } from "../../layout/collection-page";
import { useStatusLabel } from "../../issues/utils/status-label";
import { summarizeEngineeringStages } from "./engineering-utils";
import type { IssueGraphNode } from "@multica/core/api";

interface EngineeringProgressProps {
  workspaceSlug: string;
  workspaceId: string;
  nodes: readonly IssueGraphNode[];
  stageTitles: ReadonlyMap<number, string>;
}

export function EngineeringProgress({ workspaceSlug, workspaceId, nodes, stageTitles }: EngineeringProgressProps) {
  const { t } = useT("engineering");
  const statusLabel = useStatusLabel(workspaceId);
  const groups = summarizeEngineeringStages(nodes);
  return (
    <Card data-engineering-region="progress">
      <CardHeader><CardTitle>{t(($) => $.progress.title)}</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        {groups.length === 0 ? <CollectionPageState icon={AlertCircle} title={t(($) => $.progress.emptyTitle)} /> : groups.map((group) => {
          const percent = group.total ? Math.round(group.closed / group.total * 100) : 0;
          const title = group.stage === null
            ? t(($) => $.progress.unstaged)
            : stageTitles.get(group.stage) ?? t(($) => $.progress.stage, { number: group.stage });
          return (
            <section key={group.stage ?? "unstaged"} className="rounded-lg border p-3" data-engineering-stage-group={group.stage ?? "unstaged"}>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 className="font-medium">{title}</h3>
                <div className="flex gap-2 text-caption text-muted-foreground">
                  <span>{t(($) => $.progress.closed, { count: group.closed, total: group.total })}</span>
                  {group.blocked ? <Badge variant="destructive">{t(($) => $.progress.blocked, { count: group.blocked })}</Badge> : null}
                </div>
              </div>
              <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted" aria-label={t(($) => $.progress.percent, { percent })}>
                <div className="h-full rounded-full bg-primary" style={{ width: `${percent}%` }} />
              </div>
              {group.open.length ? (
                <ul className="mt-3 space-y-2">
                  {group.open.slice(0, 5).map((node) => (
                    <li key={node.id} className="flex items-start justify-between gap-3 text-caption">
                      <AppLink href={paths.workspace(workspaceSlug).issueDetail(node.id)} className="min-w-0 hover:underline">
                        <span className="mr-2 text-muted-foreground">{node.identifier}</span>{node.title}
                      </AppLink>
                      <span className="shrink-0 text-muted-foreground">{statusLabel(node.status)}</span>
                    </li>
                  ))}
                </ul>
              ) : null}
            </section>
          );
        })}
      </CardContent>
    </Card>
  );
}
