"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace";
import {
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { getPreferredStartPage } from "@multica/cerebro-preferences/views";

/**
 * Client-side redirect for the root URL. The proxy already handles the
 * common cases server-side (anon → /login, logged-in-with-cookie → last
 * workspace). This component only fires for the rare case where the user
 * is authenticated but has no `last_workspace_slug` cookie yet (first
 * login since the slug migration, or after the cookie was cleared).
 *
 * Renders nothing — uses `router.replace` so the root page never enters
 * browser history.
 */
export function RootRedirect() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();

  const { data: list = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading) return;
    if (!user) {
      router.replace("/login");
      return;
    }
    if (!isFetched) return;
    const fallback = resolvePostAuthDestination(list, hasOnboarded);
    const isMobile =
      typeof window !== "undefined" && window.innerWidth < 768;
    const firstWs = list[0];
    if (hasOnboarded && firstWs) {
      const page = getPreferredStartPage(user, isMobile ? "mobile" : "desktop");
      router.replace(page ? `/${firstWs.slug}/${page}` : fallback);
      return;
    }
    router.replace(fallback);
  }, [isLoading, user, isFetched, list, hasOnboarded, router]);

  return null;
}
