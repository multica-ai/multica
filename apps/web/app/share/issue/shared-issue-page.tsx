"use client";

import { useEffect, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Link2 } from "@multica/ui/icons";
import { useLocale, useT } from "@multica/views/i18n";

const pendingShareKey = "multica_pending_issue_share";

function subscribeToShareToken(onChange: () => void) {
  window.addEventListener("hashchange", onChange);
  return () => window.removeEventListener("hashchange", onChange);
}

function readShareToken() {
  const token = window.location.hash.slice(1) || window.sessionStorage.getItem(pendingShareKey);
  return token && /^[a-f0-9]{24}$/.test(token) ? token : null;
}

export function SharedIssuePage() {
  const { t } = useT("issues");
  const locale = useLocale();
  const router = useRouter();
  const userId = useAuthStore((state) => state.user?.id);
  const isLoading = useAuthStore((state) => state.isLoading);
  const token = useSyncExternalStore(subscribeToShareToken, readShareToken, () => null);

  useEffect(() => {
    if (isLoading || !token) return;
    if (!userId) {
      // Keep the token out of the login URL and third-party OAuth state.
      window.sessionStorage.setItem(pendingShareKey, token);
      router.replace(`${paths.login()}?next=${encodeURIComponent(paths.sharedIssue())}`);
      return;
    }

    if (!window.location.hash) {
      window.history.replaceState(window.history.state, "", paths.sharedIssue(token));
    }
    window.sessionStorage.removeItem(pendingShareKey);
  }, [isLoading, router, token, userId]);

  const issue = useQuery({
    queryKey: ["shared-issue", userId, token],
    queryFn: ({ signal }) => api.getSharedIssue(token ?? "", signal),
    enabled: !!userId && !!token,
    gcTime: 0,
    retry: false,
  });
  const unavailable = !token || (
    issue.error instanceof ApiError && [403, 404].includes(issue.error.status)
  );
  const loading = isLoading || (!!token && (!userId || issue.isPending));

  return (
    <main className="h-full overflow-y-auto bg-background">
      <div className="mx-auto max-w-3xl px-6 pb-16 pt-12 sm:px-10 sm:pt-20">
        <div className="mb-6 flex items-center gap-2 text-caption text-muted-foreground">
          <Link2 className="size-4" aria-hidden />
          {t(($) => $.sharing.read_only)}
        </div>
        {loading ? (
          <div className="space-y-4" aria-busy="true">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-8 w-2/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
          </div>
        ) : unavailable ? (
          <h1 className="text-title-lg font-semibold">{t(($) => $.sharing.unavailable)}</h1>
        ) : issue.isError ? (
          <div className="space-y-4">
            <p role="alert" className="text-body">{t(($) => $.sharing.load_failed)}</p>
            <Button variant="outline" onClick={() => void issue.refetch()}>
              {t(($) => $.sharing.retry)}
            </Button>
          </div>
        ) : issue.data ? (
          <SharedIssueBody issue={issue.data} locale={locale} />
        ) : null}
      </div>
    </main>
  );
}

function SharedIssueBody({
  issue,
  locale,
}: {
  issue: Awaited<ReturnType<typeof api.getSharedIssue>>;
  locale: string;
}) {
  const { t } = useT("issues");
  const formatDate = new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" });
  const runStatuses = new Map([
    ["completed", t(($) => $.execution_log.status_completed)],
    ["failed", t(($) => $.execution_log.status_failed)],
    ["cancelled", t(($) => $.execution_log.status_cancelled)],
  ]);

  return (
    <article>
      <div className="mb-3 flex flex-wrap items-center gap-3 text-caption text-muted-foreground">
        <span className="font-mono">{issue.identifier}</span>
        <span className="rounded-md border border-surface-border px-2 py-0.5">
          {issue.status_label || issue.status.replaceAll("_", " ")}
        </span>
      </div>
      <h1 className="break-words text-display font-semibold">{issue.title}</h1>
      {issue.description && (
        <p className="mt-6 whitespace-pre-wrap break-words text-body leading-relaxed">
          {issue.description}
        </p>
      )}
      <div className="mt-8 grid gap-8 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
        {issue.comments.length > 0 && (
          <section aria-labelledby="shared-issue-comments">
            <h2 id="shared-issue-comments" className="mb-3 text-label font-semibold">{t(($) => $.sharing.comments)}</h2>
            <ol className="space-y-3">
              {issue.comments.map((comment) => (
                <li key={comment.id} className="rounded-md border border-surface-border p-3">
                  <div className="mb-1 flex flex-wrap items-center gap-x-2 text-caption">
                    <span className="font-medium">{t(($) => $.workflows.handler[comment.author_type])}</span>
                    <time className="text-muted-foreground" dateTime={comment.created_at}>
                      {formatDate.format(new Date(comment.created_at))}
                    </time>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-body">{comment.content}</p>
                </li>
              ))}
            </ol>
          </section>
        )}
        {issue.runs.length > 0 && (
          <section aria-labelledby="shared-issue-runs">
            <h2 id="shared-issue-runs" className="mb-3 text-label font-semibold">{t(($) => $.sharing.agent_runs)}</h2>
            <ol className="space-y-3">
              {issue.runs.map((run) => (
                <li key={run.id} className="rounded-md border border-surface-border p-3">
                  <p className="text-label font-medium">{runStatuses.get(run.status) ?? run.status}</p>
                  <time className="text-caption text-muted-foreground" dateTime={run.completed_at}>
                    {formatDate.format(new Date(run.completed_at))}
                  </time>
                </li>
              ))}
            </ol>
          </section>
        )}
      </div>
    </article>
  );
}
