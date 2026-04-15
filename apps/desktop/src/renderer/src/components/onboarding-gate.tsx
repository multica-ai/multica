import { useState, type ReactNode } from "react";

/**
 * Renders `onboarding` as a full-screen takeover when the user logs in
 * without a workspace, otherwise renders `children`.
 *
 * The "needs onboarding" decision is frozen at first mount via the lazy
 * useState initializer so the onboarding view controls its own exit: the
 * onboarding component calls the `onComplete` callback when it's ready to
 * hand off to `children`, instead of getting unmounted the moment the
 * workspace store updates.
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
