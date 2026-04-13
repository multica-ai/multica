"use client";

import { useState, useCallback } from "react";
import { useWorkspaceStore } from "@multica/core/workspace";
import type { Agent } from "@multica/core/types";
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
  onComplete: () => void;
}

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  const [step, setStep] = useState(0);
  const [createdAgent, setCreatedAgent] = useState<Agent | null>(null);

  const wsId = useWorkspaceStore((s) => s.workspace?.id) ?? null;

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
        {step === 0 && <StepWorkspace onNext={next} />}
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
        {step === 3 && wsId && (
          <StepComplete
            wsId={wsId}
            agent={createdAgent}
            onEnter={onComplete}
          />
        )}
      </div>
    </div>
  );
}
