"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace";
import { resolvePostAuthDestination, useHasOnboarded } from "@multica/core/paths";
import { isOfficialMarketingHost } from "@/lib/public-host";

/**
 * Client-side fallback redirect for authenticated visitors on the landing page.
 *
 * The primary path for logged-in users hitting an app host's `/` is a
 * server-side redirect in the Next.js proxy/middleware, driven by the
 * `last_workspace_slug` cookie. That cookie is set by the workspace layout on
 * every visit. But on *first login* — before the user has ever visited a
 * workspace — the cookie is absent, so the proxy falls through to the landing
 * page. This component covers that gap on app/self-host origins.
 *
 * On the official marketing origins, `/` must remain public even for logged-in
 * users. Explicit workspace routes still open the app.
 *
 * Renders nothing. Uses `router.replace` so the landing page never enters
 * browser history for authenticated users.
 */
export function RedirectIfAuthenticated() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();

  const { data: list = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading || !user || !isFetched) return;
    if (isOfficialMarketingHost(window.location.hostname)) return;
    router.replace(resolvePostAuthDestination(list, hasOnboarded));
  }, [isLoading, user, isFetched, list, hasOnboarded, router]);

  return null;
}
