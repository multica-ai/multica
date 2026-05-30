"use client";

import { Suspense, useCallback, useEffect } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { sanitizeNextUrl, useAuthStore } from "@multica/core/auth";
import { needsSourceBackfill } from "@multica/core/onboarding";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import {
  SourceBackfillView,
  useSourceBackfillDismissCount,
} from "@multica/views/onboarding";

/**
 * Web shell for the source-backfill prompt. Mirrors the
 * `/onboarding` page (chrome owned by the route, content owned by the
 * shared view).
 *
 * Entry contract:
 *   - Triggered by `/auth/callback` when the freshly logged-in user
 *     matches `needsSourceBackfill`. Carries the intended post-auth
 *     destination as `?next=`.
 *   - Also reachable via direct deep-link; we re-check the predicate
 *     here and bounce out if the user no longer qualifies (already
 *     answered on another device, dismiss cap hit).
 *
 * Exit:
 *   - Submit / Skip → navigate to `?next=` or the resolver default.
 *   - Close X / ESC → bump the per-user dismiss counter and navigate to
 *     the same destination, so closing isn't a dead-end.
 */
function SourcePageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();
  const [dismissCount, bumpDismissCount] = useSourceBackfillDismissCount(
    user?.id ?? null,
  );
  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  const nextUrl = sanitizeNextUrl(searchParams.get("next"));

  const navigateOnward = useCallback(() => {
    const dest =
      nextUrl ?? resolvePostAuthDestination(workspaces, hasOnboarded);
    router.replace(dest);
  }, [nextUrl, workspaces, hasOnboarded, router]);

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      router.replace(paths.login());
      return;
    }
    if (!workspacesFetched) return;
    // Predicate re-check: the user may have answered on another device
    // since the redirect was decided, or hit the dismiss cap.
    if (!needsSourceBackfill(user, dismissCount)) {
      navigateOnward();
    }
  }, [
    isLoading,
    user,
    workspacesFetched,
    dismissCount,
    navigateOnward,
    router,
  ]);

  if (isLoading || !user) return null;
  if (!needsSourceBackfill(user, dismissCount)) return null;

  return (
    <div className="h-full overflow-y-auto bg-background">
      <SourceBackfillView
        onComplete={() => navigateOnward()}
        onClose={() => {
          bumpDismissCount();
          navigateOnward();
        }}
      />
    </div>
  );
}

export default function SourceBackfillPage() {
  return (
    <Suspense fallback={null}>
      <SourcePageContent />
    </Suspense>
  );
}
