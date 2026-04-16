"use client";

import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Agent, Workspace } from "@multica/core/types";
import { StepWorkspace } from "./step-workspace";
import { StepRuntime } from "./step-runtime";
import { StepAgent } from "./step-agent";
import { StepComplete } from "./step-complete";

const STEPS = [
  { label: "Workspace" },
  { label: "Runtime" },
  { label: "Agent" },
  { label: "Get Started" },
] as const;

export interface OnboardingWizardProps {
  /**
   * Called when the user finishes the wizard. The just-configured workspace is
   * passed so the caller can navigate into it (/{slug}/issues). Onboarding is a
   * pre-workspace global route, so the URL has no slug while it runs.
   */
  onComplete: (workspace: Workspace) => void;
}

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  // Canonical source for workspace existence: the React Query list cache. The
  // onboarding route itself is global (no slug in URL), so useCurrentWorkspace
  // can't help here — we read the list directly. `useCreateWorkspace` adds the
  // new workspace to this cache in its onSuccess, so step 0 → step 1 happens
  // once the list query is populated.
  const { data: wsList = [] } = useQuery(workspaceListOptions());
  // A user arriving at /onboarding normally has 0 workspaces. After the first
  // step they have exactly one. In the rare case the list already has entries
  // (e.g. the user manually navigated to /onboarding), pick the most recent —
  // that's the one the onboarding flow should configure.
  const workspace: Workspace | null = wsList[wsList.length - 1] ?? null;
  const wsId = workspace?.id ?? null;

  const [step, setStep] = useState(() => (workspace ? 1 : 0));
  const [createdAgent, setCreatedAgent] = useState<Agent | null>(null);

  useEffect(() => {
    if (step === 0 && wsId) {
      setStep(1);
    }
  }, [step, wsId]);

  const startWorkspaceSetup = useCallback(() => setStep(1), []);

  const next = useCallback(
    () => setStep((s) => Math.min(s + 1, STEPS.length - 1)),
    [],
  );

  return (
    <div className="flex min-h-svh flex-col bg-background">
      {/* Progress bar */}
      <div className="flex items-center justify-center gap-2 px-6 pt-8">
        {STEPS.map((s, i) => (
          <div key={s.label} className="flex items-center gap-2">
            <div className="flex items-center gap-1.5">
              <div
                className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-medium transition-colors ${
                  i <= step
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted text-muted-foreground"
                }`}
              >
                {i < step ? (
                  <svg
                    className="h-3.5 w-3.5"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="3"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                ) : (
                  i + 1
                )}
              </div>
              <span
                className={`text-sm ${
                  i <= step
                    ? "text-foreground font-medium"
                    : "text-muted-foreground"
                }`}
              >
                {s.label}
              </span>
            </div>
            {i < STEPS.length - 1 && (
              <div
                className={`h-px w-8 ${i < step ? "bg-primary" : "bg-border"}`}
              />
            )}
          </div>
        ))}
      </div>

      {/* Step content */}
      <div className="flex flex-1 items-center justify-center px-6 py-12">
        {step === 0 && <StepWorkspace onNext={startWorkspaceSetup} />}
        {step === 1 && wsId && (
          <StepRuntime wsId={wsId} onNext={next} />
        )}
        {step === 2 && wsId && (
          <StepAgent
            wsId={wsId}
            onNext={next}
            onAgentCreated={setCreatedAgent}
          />
        )}
        {step === 3 && workspace && (
          <StepComplete
            wsId={workspace.id}
            agent={createdAgent}
            onEnter={() => onComplete(workspace)}
          />
        )}
      </div>
    </div>
  );
}
