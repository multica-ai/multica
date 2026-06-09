"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import {
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "../navigation";
import { useLogout } from "../auth";
import { DragStrip } from "../platform";
import { useT } from "../i18n";

/**
 * Rendered when the workspace slug in the URL does not resolve to a workspace
 * the current user can access. Deliberately doesn't distinguish "workspace
 * doesn't exist" from "workspace exists but I'm not a member" — showing
 * either would let attackers enumerate workspace slugs.
 */
export function NoAccessPage() {
  const { t } = useT("workspace");
  const nav = useNavigation();
  const logout = useLogout();
  const hasOnboarded = useHasOnboarded();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());

  // Clear stale `last_workspace_slug` cookie. The web proxy redirects `/` to
  // `/<lastSlug>/issues` based on this cookie alone (no access check). When the
  // cookie points at a workspace the user has just lost access to, any hit on
  // `/` — manual navigation, a browser Back into `/`, or a fresh page load —
  // bounces the user straight back to the bad slug and re-traps them on
  // NoAccessPage. The recovery button no longer routes through `/` (recover()
  // resolves a concrete destination directly), but clearing the cookie here
  // keeps those other `/` entry points from re-triggering the loop.
  // No-op outside the browser (desktop renderer also has document, harmless).
  useEffect(() => {
    if (typeof document === "undefined") return;
    document.cookie = "last_workspace_slug=; path=/; max-age=0; SameSite=Lax";
  }, []);

  // replace, not push: the failed `/<bad-slug>` URL must not stay in history,
  // or a browser Back would land the user right back on this NoAccessPage.
  const recover = () => {
    nav.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
  };

  return (
    <div className="flex min-h-svh flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6 pb-12 text-center">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">
            {t(($) => $.no_access.title)}
          </h1>
          <p className="max-w-md text-muted-foreground">
            {t(($) => $.no_access.description)}
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button onClick={recover}>
            {t(($) => $.no_access.go_to_workspaces)}
          </Button>
          <Button variant="outline" onClick={logout}>
            {t(($) => $.no_access.sign_in_different)}
          </Button>
        </div>
      </div>
    </div>
  );
}
