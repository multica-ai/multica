"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace";
import { resolvePostAuthDestination, useHasOnboarded } from "@multica/core/paths";
import { getPreferredStartPage } from "@multica/cerebro-preferences/views";

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
  const hasOnboarded = useHasOnboarded();

  const { data: list = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading || !user || !isFetched) return;
    // CEREBRO-PATCH(jeh-1642-start-page-boot): mobile start page preference
    const fallback = resolvePostAuthDestination(list, hasOnboarded);
    const isMobile =
      typeof window !== "undefined" && window.innerWidth < 768;
    const firstWs = list[0];
    if (hasOnboarded && firstWs && isMobile) {
      const page = getPreferredStartPage(user, "mobile");
      router.replace(page ? `/${firstWs.slug}/${page}` : fallback);
      return;
    }
    router.replace(fallback);
  }, [isLoading, user, isFetched, list, hasOnboarded, router]);

  return null;
}
