"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import {
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  saveQuestionnaire,
  type OnboardingStep,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Agent, AgentRuntime, Workspace } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "./components/step-header";
import { StepWelcome } from "./steps/step-welcome";
import { StepQuestionnaire } from "./steps/step-questionnaire";
import { StepWorkspace } from "./steps/step-workspace";
import { StepRuntimeConnect } from "./steps/step-runtime-connect";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { StepAgent } from "./steps/step-agent";
import { StepFirstIssue } from "./steps/step-first-issue";

const EMPTY_QUESTIONNAIRE: QuestionnaireAnswers = {
  team_size: null,
  team_size_other: null,
  role: null,
  role_other: null,
  use_case: null,
  use_case_other: null,
};

function mergeQuestionnaire(
  raw: Record<string, unknown>,
): QuestionnaireAnswers {
  return { ...EMPTY_QUESTIONNAIRE, ...(raw as Partial<QuestionnaireAnswers>) };
}

/**
 * Shell's onComplete contract:
 *   onComplete(workspace?) — if present, navigate into its issues list;
 *   if omitted, fall back to root. A Starter-content opt-in dialog runs
 *   on the issues page itself (see `StarterContentPrompt`), so the flow
 *   doesn't carry `firstIssueId` any more — there is no welcome issue
 *   created by onboarding.
 */
export function OnboardingFlow({
  onComplete,
  runtimeInstructions,
}: {
  onComplete: (workspace?: Workspace) => void;
  runtimeInstructions?: React.ReactNode;
}) {
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error("OnboardingFlow requires an authenticated user");
  }

  // Questionnaire answers are server-persisted and pre-fill Step 1
  // on re-entry. That's the only piece of onboarding state persisted
  // across sessions — which step the user is on is deliberately not
  // saved, so every entry starts at Welcome.
  const storedQuestionnaire = mergeQuestionnaire(user.onboarding_questionnaire);

  const [step, setStep] = useState<OnboardingStep>("welcome");
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [runtime, setRuntime] = useState<AgentRuntime | null>(null);
  const [, setAgent] = useState<Agent | null>(null);

  // Fetched at Step 0 + Step 2. Step 2 uses it to detect a pre-existing
  // workspace from an earlier abandoned onboarding (so StepWorkspace shows
  // "Continue with {name}" instead of CreateWorkspaceForm — avoiding the
  // slug conflict that creation would hit). Step 0 uses it to decide
  // whether to render the "I've done this before" skip button — only
  // shown when the user already has at least one workspace, otherwise
  // skipping would land them in limbo.
  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: step === "welcome" || step === "workspace",
  });
  const existingWorkspace = workspace ?? workspaces[0] ?? null;
  const canSkipWelcome = workspacesFetched && workspaces.length > 0;

  const handleWelcomeNext = useCallback(() => {
    setStep("questionnaire");
  }, []);

  // "I've done this before" path — returning user who already has a
  // workspace and just wants to land there. Marks onboarding complete
  // server-side (idempotent via COALESCE on onboarded_at) and navigates
  // to their first workspace. Because starter_content_state is NULL for
  // any user reaching this button (it's freshly added), they'll see the
  // StarterContentPrompt dialog on arrival — which is correct, since
  // they never got a starter project and may want one now.
  const handleWelcomeSkip = useCallback(async () => {
    try {
      await completeOnboarding();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to finish onboarding",
      );
      return;
    }
    onComplete(workspaces[0] ?? undefined);
  }, [workspaces, onComplete]);

  const handleQuestionnaireSubmit = useCallback(
    async (answers: QuestionnaireAnswers) => {
      await saveQuestionnaire(answers);
      setStep("workspace");
    },
    [],
  );

  const handleWorkspaceCreated = useCallback((ws: Workspace) => {
    setWorkspace(ws);
    setCurrentWorkspace(ws.slug, ws.id);
    setStep("runtime");
  }, []);

  const handleRuntimeNext = useCallback((rt: AgentRuntime | null) => {
    setRuntime(rt);
    // No runtime → no agent possible; skip Step 4 and go straight to
    // the finalizer. The post-landing StarterContentPrompt will detect
    // "no agent in this workspace" and offer the self-serve template.
    setStep(rt ? "agent" : "first_issue");
  }, []);

  const handleAgentCreated = useCallback((created: Agent) => {
    setAgent(created);
    setStep("first_issue");
  }, []);

  const handleBack = useCallback((from: OnboardingStep) => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx <= 0) return;
    const prev = ONBOARDING_STEP_ORDER[idx - 1]!;
    setStep(prev);
  }, []);

  // Step 5 fired `completeOnboarding` itself. Here we just route the
  // user to their workspace — the starter-content decision happens
  // inside the workspace via the `StarterContentPrompt` dialog.
  const handleFinished = useCallback(() => {
    onComplete(workspace ?? undefined);
  }, [workspace, onComplete]);

  // Welcome, Questionnaire, and Workspace own full-bleed two-column
  // layouts (hero / side panel) with their own DragStrip + StepHeader.
  // The remaining steps (runtime / agent / first_issue) still render
  // inside a narrow legacy single-column shell below — they'll each
  // move out as they get redesigned.
  if (step === "welcome") {
    return (
      <StepWelcome
        onNext={handleWelcomeNext}
        onSkip={canSkipWelcome ? handleWelcomeSkip : undefined}
      />
    );
  }

  if (step === "questionnaire") {
    return (
      <StepQuestionnaire
        initial={storedQuestionnaire}
        onSubmit={handleQuestionnaireSubmit}
      />
    );
  }

  if (step === "workspace") {
    return (
      <StepWorkspace
        existing={existingWorkspace}
        onCreated={handleWorkspaceCreated}
        onBack={() => handleBack("workspace")}
      />
    );
  }

  // Step 3. Both paths own full-bleed two-column layouts.
  //   - Desktop (no cliInstructions slot) → StepRuntimeConnect drives
  //     the local daemon's runtime list directly.
  //   - Web → StepPlatformFork offers Download / CLI / Cloud paths.
  //     Under the CLI path it embeds StepRuntimeConnect for the live
  //     probe; the Cloud path is a soft exit via the waitlist.
  if (step === "runtime" && workspace) {
    if (!runtimeInstructions) {
      return (
        <StepRuntimeConnect
          wsId={workspace.id}
          onNext={handleRuntimeNext}
          onBack={() => handleBack("runtime")}
        />
      );
    }
    return (
      <StepPlatformFork
        wsId={workspace.id}
        onNext={handleRuntimeNext}
        onBack={() => handleBack("runtime")}
        cliInstructions={runtimeInstructions}
      />
    );
  }

  // Step 4 owns the same full-bleed editorial shell as Workspace /
  // Questionnaire. `questionnaire` is threaded through so StepAgent
  // can recommend a template based on the user's Q1–Q3 answers.
  // No skip path: reaching Step 4 means a runtime was picked at
  // Step 3, so creating the agent IS the step's purpose. Users who
  // want a runtime-less workspace bypass at Step 3 and skip Step 4
  // entirely (flow routes runtime=null → first_issue directly).
  if (step === "agent" && runtime) {
    return (
      <StepAgent
        runtime={runtime}
        questionnaire={storedQuestionnaire}
        onCreated={handleAgentCreated}
        onBack={() => handleBack("agent")}
      />
    );
  }

  return (
    <div className="animate-onboarding-enter flex min-h-full flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center px-6 pb-12">
        <div className="flex w-full max-w-xl flex-col gap-8">
          <StepHeader currentStep={step} />
          {step === "first_issue" && (
            <StepFirstIssue onFinished={handleFinished} />
          )}
        </div>
      </div>
    </div>
  );
}

export type { OnboardingStep };
