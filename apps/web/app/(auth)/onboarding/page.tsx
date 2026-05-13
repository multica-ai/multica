"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import {
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace/queries";

/**
 * Legacy onboarding route. First-run onboarding is bypassed, so this route
 * immediately hands users to the standard post-auth destination.
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

  useEffect(() => {
    if (isLoading || !user) {
      if (!isLoading && !user) router.replace(paths.login());
      return;
    }
    if (!workspacesFetched) return;
    router.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
  }, [isLoading, user, hasOnboarded, workspacesFetched, workspaces, router]);

  return null;
}
