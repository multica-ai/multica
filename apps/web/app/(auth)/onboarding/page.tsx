"use client";

import { useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { useWorkspaceList } from "@multica/core/workspace";
import { CliInstallInstructions, OnboardingFlow } from "@multica/views/onboarding";

/**
 * Web shell for the onboarding flow. The route is the platform chrome on
 * web (matching `WindowOverlay` on desktop); content is the shared
 * `<OnboardingFlow />`. Kept minimal — guard on auth, render, exit.
 *
 * Runtime-connected onboarding opens the Mika session that the final step
 * created and started. Other exits land on the workspace issues list, or root
 * when no workspace exists.
 *
 * `CliInstallInstructions` is passed in as the `runtimeInstructions`
 * slot so the flow can render it inside the CLI dialog. The commands it
 * shows are hardcoded — nothing environmental to thread through.
 */
export default function OnboardingPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();
  const { workspaces, ready: workspacesReady } = useWorkspaceList({
    enabled: !!user,
  });
  // The bootstrap path calls refreshMe() before returning, which flips
  // hasOnboarded to true while the page is still mounted. Without this
  // flag the guard below races onComplete: the guard's router.replace
  // (issues list) can overtake onComplete's router.push (guide issue),
  // dropping the user on the wrong destination. Marking the page as
  // "completing" right before onComplete navigates keeps the guard
  // silent for the in-flight transition.
  const completingRef = useRef(false);

  useEffect(() => {
    if (isLoading || !user) {
      if (!isLoading && !user) router.replace(paths.login());
      return;
    }
    if (!workspacesReady) return;
    if (completingRef.current) return;
    // Bounce out only when onboarding genuinely doesn't apply: the user is
    // already onboarded. We deliberately don't bounce on `workspaces.length`
    // here — the flow creates a workspace mid-onboarding, and a
    // hasWorkspaces bounce here would kick the user out before runtime and
    // Mika setup can run. The new entry-point
    // judgment in callback / login handles "where should this user go on
    // login" so OnboardingPage no longer needs to second-guess it.
    if (hasOnboarded) {
      router.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
    }
  }, [isLoading, user, hasOnboarded, workspacesReady, workspaces, router]);

  if (isLoading || !user || hasOnboarded) return null;

  // Layout: page owns its own scroll (root layout sets `body {
  // overflow: hidden }` for the app-shell convention). OnboardingFlow
  // owns the per-step width constraint internally — Welcome renders a
  // wide two-column hero, all other steps wrap themselves at max-w-xl.
  return (
    <div className="h-full overflow-y-auto bg-background">
      <OnboardingFlow
        onComplete={(ws, destination) => {
          completingRef.current = true;
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
