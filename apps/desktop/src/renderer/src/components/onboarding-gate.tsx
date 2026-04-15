import { useState, type ReactNode } from "react";

/**
 * Renders `onboarding` as a full-screen takeover when the user logs in
 * without a workspace, otherwise renders `children`.
 *
 * The "needs onboarding" decision is frozen at first mount via the lazy
 * useState initializer — creating a workspace mid-wizard (step 0) must not
 * unmount the wizard, because steps 1-3 (runtime, agent, get started) still
 * need to run. Calling the `onComplete` passed to `onboarding` flips the
 * gate to `children`.
 *
 * Assumes `hasWorkspace` is definitive at first render: desktop only mounts
 * DesktopShell after AppContent's bootstrapping flag resolves, so the first
 * render of this component reflects the actual server state.
 */
export function OnboardingGate({
  hasWorkspace,
  onboarding,
  children,
}: {
  hasWorkspace: boolean;
  onboarding: (onComplete: () => void) => ReactNode;
  children: ReactNode;
}) {
  const [initialNeedsOnboarding] = useState(() => !hasWorkspace);
  const [onboardingDone, setOnboardingDone] = useState(false);

  if (initialNeedsOnboarding && !onboardingDone) {
    return <>{onboarding(() => setOnboardingDone(true))}</>;
  }
  return <>{children}</>;
}
