"use client";

import { AlertCircle, CheckCircle2, MessageSquareText } from "@multica/ui/icons";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { paths } from "@multica/core/paths";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { CollectionPageState } from "../../layout/collection-page";
import { RichContent } from "../../rich-content";
import { useT } from "../../i18n";

export interface EngineeringDecisionTimeline {
  comment: TimelineEntry | null;
  loading: boolean;
  error: boolean;
}

interface EngineeringDecisionsProps {
  workspaceSlug: string;
  issues: readonly Issue[];
  timelines: Readonly<Record<string, EngineeringDecisionTimeline>>;
}

export function EngineeringDecisions({ workspaceSlug, issues, timelines }: EngineeringDecisionsProps) {
  const { t } = useT("engineering");
  return (
    <Card data-engineering-region="decisions">
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle>{t(($) => $.decisions.title)}</CardTitle>
        <Badge variant="secondary">{t(($) => $.decisions.count, { count: issues.length })}</Badge>
      </CardHeader>
      <CardContent className="space-y-4">
        {issues.length === 0 ? (
          <CollectionPageState icon={CheckCircle2} title={t(($) => $.decisions.emptyTitle)} />
        ) : issues.map((issue) => {
          const timeline = timelines[issue.id];
          return (
            <article key={issue.id} className="rounded-lg border" data-decision-issue={issue.identifier}>
              <div className="border-b px-4 py-3">
                <AppLink href={paths.workspace(workspaceSlug).issueDetail(issue.id)} className="font-medium hover:underline">
                  <span className="mr-2 text-caption text-muted-foreground">{issue.identifier}</span>
                  {issue.title}
                </AppLink>
              </div>
              <div className="p-4">
                {timeline?.loading ? <Skeleton className="h-40 w-full" /> : null}
                {timeline?.error ? (
                  <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.timeline)}</AlertDescription></Alert>
                ) : null}
                {timeline && !timeline.loading && !timeline.error ? (
                  timeline.comment ? (
                    <RichContent content={timeline.comment.content ?? ""} attachments={timeline.comment.attachments} />
                  ) : (
                    <div className="flex items-center gap-2 text-muted-foreground"><MessageSquareText className="size-4" aria-hidden="true" />{t(($) => $.decisions.noComment)}</div>
                  )
                ) : null}
              </div>
            </article>
          );
        })}
      </CardContent>
    </Card>
  );
}
