"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useAuthStore } from "@multica/core/auth";
import { OnboardingWizard } from "@multica/views/onboarding";

export default function OnboardingPage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!isLoading && !user) router.replace("/login");
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return (
    <OnboardingWizard onComplete={() => router.push("/issues")} />
  );
}
