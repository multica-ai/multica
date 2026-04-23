"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  completeOnboarding,
  type OnboardingCompletionPath,
} from "@multica/core/onboarding";

/**
 * Step 5 — the final onboarding beat.
 *
 * All this step does now is flip `onboarded_at` on the server. The former
 * in-flight bootstrap (welcome issue + Getting Started project + sub-issues)
 * moved out of onboarding entirely: it's a post-landing opt-in dialog
 * (`StarterContentPrompt`) that runs inside the workspace after navigation.
 * Two consequences of that move:
 *
 *   1. This step can't fail in user-visible ways any more. `completeOnboarding`
 *      is one PATCH to `/api/me`; the only failure mode is a network error,
 *      which we surface as a toast + Retry, not a full error screen.
 *   2. The sub-issue "Unknown" assignee race is gone for free — by the time
 *      the import runs, the user has already landed in the workspace, so
 *      `listMembers` has resolved and the current user's member_id is in
 *      the query cache.
 */
export function StepFirstIssue({
  onFinished,
  completionPath,
}: {
  /** Called after `onboarded_at` is set server-side. Parent handles
   *  navigation to the workspace landing page. */
  onFinished: () => void;
  /** Which exit label the server should record on `onboarding_completed`.
   *  Computed in the parent shell where runtime + waitlist state are
   *  both in scope. */
  completionPath: OnboardingCompletionPath;
}) {
  const [error, setError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState(false);
  const started = useRef(false);
  const onFinishedRef = useRef(onFinished);
  onFinishedRef.current = onFinished;
  const completionPathRef = useRef(completionPath);
  completionPathRef.current = completionPath;

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    (async () => {
      try {
        await completeOnboarding(completionPathRef.current);
        onFinishedRef.current();
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to finish onboarding",
        );
      }
    })();
  }, []);

  const retry = async () => {
    if (retrying) return;
    setRetrying(true);
    setError(null);
    try {
      await completeOnboarding(completionPathRef.current);
      onFinishedRef.current();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Retry failed");
      toast.error(err instanceof Error ? err.message : "Retry failed");
    } finally {
      setRetrying(false);
    }
  };

  if (error) {
    return (
      <div className="animate-onboarding-enter flex w-full flex-col items-center gap-6 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
          <AlertCircle className="h-6 w-6" />
        </div>
        <div className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold tracking-tight">
            Something went wrong
          </h1>
          <p className="text-sm text-muted-foreground">{error}</p>
        </div>
        <Button onClick={retry} disabled={retrying}>
          {retrying && <Loader2 className="h-4 w-4 animate-spin" />}
          Retry
        </Button>
      </div>
    );
  }

  return (
    <div className="animate-onboarding-enter flex w-full flex-col items-center gap-6 text-center">
      <Loader2 className="h-10 w-10 animate-spin text-primary" />
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">
          Finishing up
        </h1>
        <p className="text-sm text-muted-foreground">
          Almost there — opening your workspace.
        </p>
      </div>
    </div>
  );
}
