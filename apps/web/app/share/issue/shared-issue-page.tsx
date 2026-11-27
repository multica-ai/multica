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
import { useT } from "@multica/views/i18n";
import { SharedIssueBody } from "./shared-issue-body";

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
      <div className="mx-auto max-w-5xl px-6 pb-16 pt-12 sm:px-10 sm:pt-20">
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
        ) : issue.data && userId && token ? (
          <SharedIssueBody key={JSON.stringify([userId, token])} issue={issue.data} userId={userId} token={token} />
        ) : null}
      </div>
    </main>
  );
}
