"use client";

import { useEffect } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { paths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { useLogout } from "../auth";
import { DragStrip } from "../platform";

/**
 * Rendered when the workspace slug in the URL does not resolve to a workspace
 * the current user can access. Deliberately doesn't distinguish "workspace
 * doesn't exist" from "workspace exists but I'm not a member" — showing
 * either would let attackers enumerate workspace slugs.
 */
export function NoAccessPage() {
  const nav = useNavigation();
  const logout = useLogout();

  // Clear stale `last_workspace_slug` cookie. The web proxy redirects `/` to
  // `/<lastSlug>/issues` based on this cookie alone (no access check). When
  // the cookie points at a workspace the user has just lost access to, the
  // user gets trapped in a loop: NoAccessPage → click "Go to my workspaces"
  // → `/` → proxy redirects back to the same bad slug → NoAccessPage.
  // Clearing the cookie here lets the proxy fall through to the landing page,
  // which then resolves the correct destination via the workspace list.
  // No-op outside the browser (desktop renderer also has document, harmless).
  useEffect(() => {
    if (typeof document === "undefined") return;
    document.cookie = "last_workspace_slug=; path=/; max-age=0; SameSite=Lax";
  }, []);
  return (
    <div className="flex min-h-svh flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6 pb-12 text-center">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">
            Workspace not available
          </h1>
          <p className="max-w-md text-muted-foreground">
            This workspace doesn't exist or you don't have access.
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button onClick={() => nav.push(paths.root())}>
            Go to my workspaces
          </Button>
          <Button variant="outline" onClick={logout}>
            Sign in as a different user
          </Button>
        </div>
      </div>
    </div>
  );
}
