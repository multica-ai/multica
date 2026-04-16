import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace/queries";

/**
 * Renders `onboarding` as a full-screen takeover when the user has no
 * workspaces, otherwise renders `children`.
 *
 * Reads the workspace list directly from React Query — this works regardless
 * of whether a WorkspaceSlugProvider is mounted, unlike useCurrentWorkspace()
 * which depends on slug context from the router tree.
 *
 * The onboarding decision is frozen at first mount via the lazy useState
 * initializer: this way the onboarding wizard controls its own exit by
 * calling the `onComplete` callback, instead of being unmounted the moment
 * the workspace list updates mid-flow (e.g. after the user creates their
 * first workspace in step 1 but still has steps 2-3 to complete).
 *
 * The frozen decision only triggers when the initial query has settled AND
 * the list is empty. While the list is loading, children are rendered
 * (the shell shows its own loading state).
 */
export function OnboardingGate({
  onboarding,
  children,
}: {
  onboarding: (onComplete: () => void) => ReactNode;
  children: ReactNode;
}) {
  const { data: workspaces, isFetched } = useQuery(workspaceListOptions());
  const hasWorkspaces = !isFetched || (workspaces?.length ?? 0) > 0;

  const [initialNeedsOnboarding] = useState(() => !hasWorkspaces);
  const [onboardingDone, setOnboardingDone] = useState(false);

  if (initialNeedsOnboarding && !onboardingDone) {
    return <>{onboarding(() => setOnboardingDone(true))}</>;
  }
  return <>{children}</>;
}
