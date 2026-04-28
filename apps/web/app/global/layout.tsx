"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { ModalRegistry } from "@multica/views/modals/registry";
import { WorkspaceRail } from "@multica/views/workspace/workspace-rail";

/**
 * Layout for the cross-workspace meta view at `/global`.
 *
 * Unlike `/[workspaceSlug]/(dashboard)/layout.tsx`, this route has no
 * current workspace, so it deliberately does NOT mount `DashboardLayout`
 * (which gates rendering behind workspace resolution via `DashboardGuard`).
 * Auth gating happens here directly — same pattern as `/workspaces/new`.
 *
 * The `<WorkspaceRail />` is hoisted in so workspace switching stays a
 * one-click affordance for users on the global page (ADR D6: existing
 * dropdown is workspace-scoped only and does not apply here).
 */
export default function GlobalLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading) {
    return (
      <div className="flex h-svh items-center justify-center">
        <MulticaIcon className="size-6 animate-pulse" />
      </div>
    );
  }
  if (!user) return null;

  return (
    <div className="flex h-svh w-full">
      <WorkspaceRail />
      <main className="relative flex min-w-0 flex-1 flex-col bg-background">
        {children}
        <ModalRegistry />
      </main>
    </div>
  );
}
