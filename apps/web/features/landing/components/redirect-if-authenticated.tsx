"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace";
import { paths } from "@multica/core/paths";

/**
 * Client-side fallback redirect for authenticated visitors on the landing page.
 *
 * The primary path for logged-in users hitting `/` is a server-side redirect
 * in the Next.js proxy/middleware, driven by the `last_workspace_slug` cookie.
 * That cookie is set by the workspace layout on every visit. But on *first
 * login* — before the user has ever visited a workspace — the cookie is
 * absent, so the proxy falls through to the landing page. This component
 * covers that gap: once auth is resolved and the workspace list has loaded,
 * push the user into their workspace (or /workspaces/new if they have none).
 *
 * Renders nothing. Uses `router.replace` so the landing page never enters
 * browser history for authenticated users.
 */
export function RedirectIfAuthenticated() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  const { data: list } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading || !user || !list) return;
    const [first] = list;
    if (!first) {
      router.replace(paths.newWorkspace());
      return;
    }
    router.replace(paths.workspace(first.slug).issues());
  }, [isLoading, user, list, router]);

  return null;
}
