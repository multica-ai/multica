"use client";

// Cerebro: route-level 404 gate for the issue-detail URL. Without this
// guard, when /api/issues/<id> returns 404 (e.g. a private issue viewed
// by a non-owner), TanStack Query retries 3× with exponential backoff
// while every child query (timeline, subscribers, usage, labels, ...)
// fires independently and also retries on its own 404. Roughly 30s of
// concurrent retry storm followed by a Chromium renderer crash. The
// guard pre-fetches the issue with retry-off on 4xx, renders a stable
// 404 immediately, and only mounts IssueDetail when the issue exists
// — so child fetches never fire for inaccessible issues. (JEH-1761)

import { useQuery } from "@tanstack/react-query";
import { ChevronLeft } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { IssueDetail } from "@multica/views/issues/components";
import { useT } from "@multica/views/i18n";
import { useNavigation } from "@multica/views/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

export function CerebroIssueDetailRoute({ id }: { id: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("issues");
  const router = useNavigation();
  const paths = useWorkspacePaths();

  const { isLoading, error, data } = useQuery({
    ...issueDetailOptions(wsId, id),
    retry: (failureCount, err) => {
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        return false;
      }
      return failureCount < 3;
    },
  });

  if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
    return (
      <div
        role="alert"
        data-testid="issue-detail-not-found"
        className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-sm text-muted-foreground"
      >
        <p>{t(($) => $.detail.not_found)}</p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => router.push(paths.issues())}
        >
          <ChevronLeft className="mr-1 h-3.5 w-3.5" />
          {t(($) => $.detail.back_to_issues)}
        </Button>
      </div>
    );
  }

  if (isLoading || !data) {
    return <RouteSkeleton />;
  }

  return <IssueDetail issueId={id} />;
}

function RouteSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-4 w-4" />
        <Skeleton className="h-4 w-24" />
      </div>
      <div className="flex flex-1 min-h-0">
        <div className="flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-4xl px-3 sm:px-6 md:px-8 py-4 sm:py-6 md:py-8 space-y-6">
            <Skeleton className="h-8 w-3/4" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-5/6" />
              <Skeleton className="h-4 w-2/3" />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
