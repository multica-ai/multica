"use client";

import { Button } from "@multica/ui/components/ui/button";
import { paths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { useLogout } from "../auth";
import { DragStrip } from "../platform";
import { useWorkspaceT } from "./i18n";

/**
 * Rendered when the workspace slug in the URL does not resolve to a workspace
 * the current user can access. Deliberately doesn't distinguish "workspace
 * doesn't exist" from "workspace exists but I'm not a member" — showing
 * either would let attackers enumerate workspace slugs.
 */
export function NoAccessPage() {
  const t = useWorkspaceT();
  const nav = useNavigation();
  const logout = useLogout();
  return (
    <div className="flex min-h-svh flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6 pb-12 text-center">
        <div className="space-y-2">
          <h1 className="text-2xl font-semibold tracking-tight">
            {t.noAccess.title}
          </h1>
          <p className="max-w-md text-muted-foreground">
            {t.noAccess.description}
          </p>
        </div>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Button onClick={() => nav.push(paths.root())}>
            {t.noAccess.goToWorkspaces}
          </Button>
          <Button variant="outline" onClick={logout}>
            {t.noAccess.signInDifferent}
          </Button>
        </div>
      </div>
    </div>
  );
}
