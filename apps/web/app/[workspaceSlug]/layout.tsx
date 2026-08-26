"use client";

import { use, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useRouter, usePathname } from "next/navigation";
import { WorkspaceSlugProvider, paths } from "@multica/core/paths";
import { workspaceBySlugOptions } from "@multica/core/workspace";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import { NoAccessPage } from "@multica/views/workspace/no-access-page";
import { WelcomeAfterOnboarding } from "@multica/views/workspace/welcome-after-onboarding";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { useWorkspaceSeen } from "@multica/views/workspace/use-workspace-seen";
import { workspaceSlugFromPathname } from "@/lib/workspace-slug-from-pathname";

export default function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const router = useRouter();
  const pathname = usePathname();

  // Workspace routes require auth. If user is unauthenticated (initial visit
  // without a session, token expired, another tab logged out, etc.), bounce
  // to /login. Without this, the layout renders null and the user sees a
  // blank page stuck on /{slug}/...
  useEffect(() => {
    if (!isAuthLoading && !user) router.replace(paths.login());
  }, [isAuthLoading, user, router]);

  // Hard onboarding gate. Authenticated user but onboarded_at NULL means
  // they bypassed /onboarding (typed the URL, deeplink, etc.). Redirect
  // back so the questionnaire + Step 3 finish. The reverse gate lives in
  // `apps/web/app/(auth)/onboarding/page.tsx` — onboarded users hitting
  // /onboarding bounce out to their workspace. Together those two effects
  // make `onboarded_at` the single source of truth for "may access /<slug>/*".
  useEffect(() => {
    if (user && user.onboarded_at == null) {
      router.replace(paths.onboarding());
    }
  }, [user, router]);

  // Resolve workspace by slug through the shared workspace-list query. A
  // warm auth bootstrap reuses its cache; a cold route fetches it directly.
  // Keep it disabled until identity has been verified.
  const { data: workspace } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  // Render-phase sync: feed the URL slug into the platform singleton so
  // the first child query's X-Workspace-Slug header is already correct.
  // setCurrentWorkspace self-dedupes + runs rehydrate as a side effect;
  // safe to call on every render.
  //
  // Gated on the live pathname because this is a module-global write from
  // render, and the App Router can keep a previous workspace's layout mounted
  // beside the incoming one. Both instances re-render on their own query
  // activity, so an unguarded write let them alternate — every render flipping
  // the singleton back to its own slug, indefinitely. Each flip tore down and
  // rebuilt the realtime socket (packages/core/realtime/provider.tsx binds it
  // to this slug) and re-pointed the @mention lookup, which reads the same
  // singleton synchronously and falls back to an empty list when the workspace
  // it names has no warm cache (packages/views/editor/extensions/
  // mention-suggestion.tsx). Realtime went dead and the mention list came back
  // empty for whichever workspace the user was actually looking at.
  //
  // Comparing against the pathname resolves it without an ownership handshake:
  // the URL is the source of truth for workspace identity, and usePathname()
  // is one value shared by every mounted instance, so exactly one layout — the
  // routed one — can define it. Desktop needs its own protocol instead
  // (workspace-route-layout.tsx) because it has no URL bar to appeal to.
  if (workspace && workspaceSlug === workspaceSlugFromPathname(pathname)) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  // Cookie write (last_workspace_slug) — proxy reads it on next page load
  // to redirect unauthenticated-URL hits to the user's last workspace.
  useEffect(() => {
    if (!workspace || typeof document === "undefined") return;
    const oneYear = 60 * 60 * 24 * 365;
    const secure = location.protocol === "https:" ? "; Secure" : "";
    document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${oneYear}; SameSite=Lax${secure}`;
  }, [workspace, workspaceSlug]);

  // Remember whether this slug has resolved before. Used below to avoid
  // flashing NoAccessPage during active workspace removal (delete, leave,
  // or realtime eviction) — in those cases the caller is navigating away
  // and we just need to hold null briefly.
  const hasBeenSeen = useWorkspaceSeen(workspaceSlug, !!workspace);

  const loadingIndicator = (
    <div className="flex h-svh items-center justify-center">
      <MulticaIcon className="size-6 animate-pulse" />
    </div>
  );

  if (isAuthLoading) return loadingIndicator;
  // Don't render children until workspace is resolved. useWorkspaceId()
  // throws when the list hasn't populated or the slug is unknown — gating
  // here makes that invariant hold for every descendant.
  // The selector returns undefined until the list has resolved, including
  // after an initial request failure. It returns null only when an
  // authoritative list does not contain this slug.
  if (workspace === undefined) return loadingIndicator;
  if (workspace === null) {
    // If we've resolved this slug before in this session, it was just
    // removed from our list (deleted/left/evicted). A navigate is almost
    // certainly in flight — render null to avoid a NoAccessPage flash.
    if (hasBeenSeen) return null;
    // Otherwise: the URL points at a workspace the user never had access
    // to. Show explicit feedback instead of silently redirecting. Doesn't
    // distinguish 404 vs 403 to avoid letting attackers enumerate slugs.
    return <NoAccessPage />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
      {/* Reads the welcome-store transient signal parked by
       *  OnboardingFlow.handleRuntimeNext. Runtime path → loading veil →
       *  blocking Modal with Helper + starter cards. Skip path → Modal
       *  with two seeded issues. No signal → null. */}
      <WelcomeAfterOnboarding />
    </WorkspaceSlugProvider>
  );
}
