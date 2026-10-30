"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { CliInstallInstructions, OnboardingFlow } from "@multica/views/onboarding";

/**
 * Creating a workspace runs the onboarding flow, entered at the workspace
 * step. A second workspace needs everything the first one needed — a runtime
 * connected to it, and its own Mika — so the only honest difference is that
 * this member has already met the product. Running one flow is what keeps the
 * two paths from drifting apart.
 *
 * `/onboarding` keeps its own guard and stays first-run only; this route is
 * the explicit already-onboarded entry point, so nothing has to bypass that
 * guard.
 */
export default function Page() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  // Cancel goes to the root path — the workspace layout redirects from there
  // to the user's default workspace. Only offered when there is somewhere to
  // return to; a user with no workspaces at all has to finish.
  const onCancel =
    wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return (
    <div className="h-full overflow-y-auto bg-background">
      <OnboardingFlow
        mode="new_workspace"
        onCancel={onCancel}
        onComplete={(ws, destination) => {
          if (ws && destination?.kind === "chat") {
            router.push(
              paths.workspace(ws.slug).chatSession(destination.sessionId),
            );
          } else if (ws && destination?.kind === "issue") {
            router.push(
              paths.workspace(ws.slug).issueDetail(destination.issueId),
            );
          } else if (ws) {
            router.push(paths.workspace(ws.slug).issues());
          } else {
            router.push(paths.root());
          }
        }}
        runtimeInstructions={<CliInstallInstructions />}
      />
    </div>
  );
}
