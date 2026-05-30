"use client";

import { use, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { useLogout } from "@multica/views/auth";
import {
  FirtalWelcomePage,
  useFirtalWelcomeEnabledForWorkspace,
} from "@multica/cerebro-firtal-welcome";

/**
 * Workspace-scoped Firtal welcome page (FIR-2490).
 *
 * Rendered outside the dashboard chrome so the experience matches the
 * upstream onboarding full-page takeover. The route is always navigable; if
 * the workspace's `cerebro_firtal_welcome` feature flag is off, this page
 * immediately redirects to the inbox so the route is safe to link to from
 * the invite-accept flow without preflight flag checks.
 */
export default function WorkspaceWelcomeRoute({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const logout = useLogout();
  const firtalWelcomeEnabled = useFirtalWelcomeEnabledForWorkspace();

  useEffect(() => {
    if (!firtalWelcomeEnabled) {
      router.replace(paths.workspace(workspaceSlug).inbox());
    }
  }, [firtalWelcomeEnabled, router, workspaceSlug]);

  if (!user || !firtalWelcomeEnabled) return null;

  return (
    <div className="h-svh overflow-y-auto bg-background">
      <FirtalWelcomePage
        userId={user.id}
        onLogout={logout}
        onComplete={() => {
          router.push(paths.workspace(workspaceSlug).inbox());
        }}
      />
    </div>
  );
}
