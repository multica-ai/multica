"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { paths } from "@multica/core/paths";
import { CliInstallInstructions, OnboardingFlow } from "@multica/views/onboarding";

/**
 * Web shell for the onboarding flow. The route is the platform chrome on
 * web (matching `WindowOverlay` on desktop); content is the shared
 * `<OnboardingFlow />`. Kept minimal — guard on auth, render, exit.
 *
 * On complete: if a workspace was just created, navigate into it;
 * otherwise fall back to root (proxy / landing picks the user's first ws
 * or bounces to onboarding if still zero).
 *
 * The CLI install card is wired here so its `multica setup` command
 * points at THIS server — dev landing on localhost gets a localhost
 * self-host command, prod cloud gets the plain `multica setup`, prod
 * self-host gets one with explicit URLs. `appUrl` lives in useState
 * so SSR doesn't error on `window` — it fills in on mount.
 */
export default function OnboardingPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const [appUrl, setAppUrl] = useState<string | undefined>(undefined);

  useEffect(() => {
    setAppUrl(window.location.origin);
  }, []);

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  // Layout: the page owns its own scroll because the root layout
  // sets `body { overflow: hidden }` for the app-shell convention
  // (sidebar / topbar fixed, content scrolls inside). The outermost
  // `h-full overflow-y-auto` is our scroll container; the inner
  // `min-h-full flex flex-col items-center` lets the content claim
  // full viewport height when short; `my-auto` on the content block
  // then centers it vertically. When content is taller than the
  // viewport the flex auto-margin harmlessly resolves to 0 (per the
  // flex spec — auto margins absorb positive free space only, never
  // negative), so Continue/Skip always remain reachable via scroll.
  return (
    <div className="h-full overflow-y-auto bg-background">
      <div className="flex min-h-full flex-col items-center px-6 py-12">
        <div className="my-auto w-full max-w-xl">
          <OnboardingFlow
            onComplete={(ws) => {
              if (ws) router.push(paths.workspace(ws.slug).issues());
              else router.push(paths.root());
            }}
            runtimeInstructions={
              <CliInstallInstructions
                apiUrl={process.env.NEXT_PUBLIC_API_URL}
                appUrl={appUrl}
              />
            }
          />
        </div>
      </div>
    </div>
  );
}
