"use client";

import { AlertCircle } from "@multica/ui/icons";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@multica/ui/components/ui/card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import type { BaselineRevision } from "@multica/core/api";
import { useT } from "../../i18n";
import { useFormatRelativeDate } from "./labels";

interface EngineeringRecentChangesProps {
  revisions: readonly BaselineRevision[];
  loading: boolean;
  error: boolean;
}

export function EngineeringRecentChanges({ revisions, loading, error }: EngineeringRecentChangesProps) {
  const { t } = useT("engineering");
  const formatRelativeDate = useFormatRelativeDate();
  return (
    <Card data-engineering-region="recent-changes">
      <CardHeader><CardTitle>{t(($) => $.recent.title)}</CardTitle></CardHeader>
      <CardContent>
        {error ? <Alert variant="destructive"><AlertCircle /><AlertTitle>{t(($) => $.errors.title)}</AlertTitle><AlertDescription>{t(($) => $.errors.history)}</AlertDescription></Alert> : null}
        {loading && revisions.length === 0 ? <Skeleton className="h-40 w-full" /> : null}
        {!loading && !error && revisions.length === 0 ? <div className="text-caption text-muted-foreground">{t(($) => $.recent.empty)}</div> : null}
        <ul className="space-y-3">
          {revisions.map((revision) => (
            <li key={revision.id} className="space-y-2 border-b pb-3 last:border-b-0 last:pb-0">
              <div className="font-medium leading-snug">{revision.changeSummary || t(($) => $.recent.noSummary)}</div>
              <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                <span>{t(($) => $.chapters[revision.chapter])}</span>
                <span>{t(($) => $.history.revision, { number: revision.revisionNumber })}</span>
                <Badge variant="outline">{t(($) => $.status[revision.status] ?? $.status.unknown)}</Badge>
                {revision.decisionCredential ? <Badge variant="secondary">{t(($) => $.credentials[revision.decisionCredential!] ?? $.credentials.unknown)}</Badge> : null}
              </div>
              {revision.decidedAt ? <time dateTime={revision.decidedAt} className="block text-caption text-muted-foreground">{formatRelativeDate(revision.decidedAt)}</time> : null}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
