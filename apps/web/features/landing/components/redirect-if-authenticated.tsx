"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { workspaceListOptions } from "@multica/core/workspace";
import { resolvePostAuthDestination, useHasOnboarded } from "@multica/core/paths";

/**
 * Client-side fallback redirect for authenticated visitors on the landing page.
 *
 * The primary path for logged-in users hitting `/` is a server-side redirect
 * in the Next.js proxy/middleware, driven by the `last_workspace_slug` cookie.
 * That cookie is set by the workspace layout on every visit. But on *first
 * login* — before the user has ever visited a workspace — the cookie is
 * absent, so the proxy falls through to the landing page. This component
 * covers that gap: once auth is resolved and the workspace list has loaded,
 * push the user into their workspace (or /onboarding if they have none).
 *
 * Renders nothing. Uses `router.replace` so the landing page never enters
 * browser history for authenticated users.
 */
export function RedirectIfAuthenticated() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  // Single-user self-host: the marketing landing page is dead UI — every
  // visitor IS the local user. As soon as the config resolves we can
  // bounce, even before getMe completes (the auth-initializer fires both
  // requests in parallel; either one being ready is enough to pick the
  // post-auth destination).
  const singleUser = useConfigStore((s) => s.singleUser);
  const hasOnboarded = useHasOnboarded();

  const { data: list = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user || singleUser,
  });

  useEffect(() => {
    if (singleUser) {
      // We do not need user/isFetched here: a freshly created local user
      // has no workspaces, so resolvePostAuthDestination will route to
      // /onboarding, which is the right place. Once the workspace exists,
      // the proxy's last_workspace_slug cookie short-circuits before we
      // ever reach this component.
      if (!isFetched) return;
      router.replace(resolvePostAuthDestination(list, hasOnboarded));
      return;
    }
    if (isLoading || !user || !isFetched) return;
    router.replace(resolvePostAuthDestination(list, hasOnboarded));
  }, [isLoading, user, isFetched, list, hasOnboarded, router, singleUser]);

  return null;
}
