"use client";

import { useEffect, useRef } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { CliInstallInstructions, OnboardingFlow } from "@multica/views/onboarding";

/**
 * Web shell for the onboarding flow. The route is the platform chrome on
 * web (matching `WindowOverlay` on desktop); content is the shared
 * `<OnboardingFlow />`. Kept minimal — guard on auth, render, exit.
 *
 * On complete: runtime-connected onboarding may provide a guide issue id;
 * navigate there. Otherwise land on the workspace issues list, or root if
 * the flow never produced a workspace.
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
  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
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
    if (!workspacesFetched) return;
    if (completingRef.current) return;
    // Bounce out only when onboarding genuinely doesn't apply: the user is
    // already onboarded. We deliberately don't bounce on `workspaces.length`
    // here — Step 3 of the flow creates a workspace mid-onboarding, and a
    // hasWorkspaces bounce here would kick the user out before Steps 4–5
    // (runtime / agent / first issue) can run. The new entry-point
    // judgment in callback / login handles "where should this user go on
    // login" so OnboardingPage no longer needs to second-guess it.
    if (hasOnboarded) {
      router.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
    }
  }, [isLoading, user, hasOnboarded, workspacesFetched, workspaces, router]);

  if (isLoading || !user || hasOnboarded) return null;

  // Layout: page owns its own scroll (root layout sets `body {
  // overflow: hidden }` for the app-shell convention). OnboardingFlow
  // owns the per-step width constraint internally — Welcome renders a
  // wide two-column hero, all other steps wrap themselves at max-w-xl.
  return (
    <div className="h-full overflow-y-auto bg-background">
      <OnboardingFlow
        onComplete={(ws, issueId) => {
          // Runtime-connected onboarding now creates one focused
          // onboarding issue. Skip/runtime-less exits still land on the
          // workspace issues list.
          completingRef.current = true;
          if (ws && issueId) {
            router.push(paths.workspace(ws.slug).issueDetail(issueId));
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
