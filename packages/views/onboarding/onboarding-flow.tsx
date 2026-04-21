"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { setCurrentWorkspace } from "@multica/core/platform";
import {
  useOnboardingStore,
  type OnboardingStep as StoreStep,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Agent, AgentRuntime, Workspace } from "@multica/core/types";
import { StepHeader } from "./components/step-header";
import { StepWelcome } from "./steps/step-welcome";
import { StepQuestionnaire } from "./steps/step-questionnaire";
import { StepWorkspace } from "./steps/step-workspace";
import { StepRuntimeConnect } from "./steps/step-runtime-connect";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { StepAgent } from "./steps/step-agent";
import { StepComplete } from "./steps/step-complete";

/**
 * Step identifiers for the orchestrator's *render* state.
 *
 *   welcome → questionnaire → workspace → runtime → agent → complete
 *
 * `welcome` is a first-entry-only product intro — not a persisted
 * step in the store. First-time users see it; anyone with stored
 * progress skips it directly to their resume point.
 *
 * `complete` is the current UI placeholder for what will eventually
 * be `first_issue` (the aha-moment step — not yet implemented). The
 * store's `current_step` enum matches the server schema
 * (`questionnaire | workspace | runtime | agent | first_issue`);
 * this local union stays decoupled until Step 5 lands.
 *
 * Branch: no-runtime or skip-runtime jumps past agent straight to
 * complete (can't build a CreateAgent request without a runtime_id).
 */
export type OnboardingStep =
  | "welcome"
  | "questionnaire"
  | "workspace"
  | "runtime"
  | "agent"
  | "complete";

/**
 * Decide which step to open the flow on. First-ever entry (pristine
 * store: sitting at `current_step === "questionnaire"` with no
 * answers) shows Welcome. Anyone with any progress resumes at their
 * saved step.
 */
function pickInitialStep(): OnboardingStep {
  const s = useOnboardingStore.getState().state;
  const pristine =
    s.current_step === "questionnaire" &&
    s.questionnaire.team_size === null &&
    s.questionnaire.role === null &&
    s.questionnaire.use_case === null;
  if (pristine) return "welcome";
  switch (s.current_step) {
    case "questionnaire":
      return "questionnaire";
    case "workspace":
      return "workspace";
    case "runtime":
      return "runtime";
    case "agent":
      return "agent";
    case "first_issue":
      // Step 5 not yet implemented — the closing ceremony screen is
      // the current UI placeholder.
      return "complete";
    default:
      return "welcome";
  }
}

/**
 * Shared onboarding orchestrator. Renders the current step and drives
 * transitions between them. Platform shells (desktop overlay, web page)
 * wrap this component — they own the chrome, not the content.
 *
 * `onComplete` receives the newly-created workspace (if any) so the
 * shell can navigate the user there. For users who already had a
 * workspace and skipped the create step, `onComplete` is called with
 * `undefined` — the shell should just close the overlay without
 * navigating.
 */
export function OnboardingFlow({
  onComplete,
  runtimeInstructions,
}: {
  onComplete: (workspace?: Workspace) => void;
  /**
   * Platform-specific instructions rendered inside the runtime step.
   * Web passes `<CliInstallInstructions />` (tells users how to install
   * the CLI). Desktop omits this — its bundled daemon auto-starts, so
   * the same guidance would be noise.
   */
  runtimeInstructions?: React.ReactNode;
}) {
  const [step, setStep] = useState<OnboardingStep>(pickInitialStep);
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [runtime, setRuntime] = useState<AgentRuntime | null>(null);
  const [agent, setAgent] = useState<Agent | null>(null);

  // Persisted questionnaire answers — rendered as the Step 1 initial
  // state so users who hit Back from a later step see their previous
  // answers (resume-after-back per §3.7 of the proposal).
  const storedQuestionnaire = useOnboardingStore(
    (s) => s.state.questionnaire,
  );
  const advance = useOnboardingStore((s) => s.advance);
  const complete = useOnboardingStore((s) => s.complete);

  // Fallback when the user skipped workspace creation (already had one).
  // We use the first workspace in the list as the runtime-step context.
  // If the user has zero and skipped, runtime step won't render and the
  // flow completes without a workspace result.
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const runtimeWorkspace = workspace ?? workspaces[0] ?? null;

  const handleWelcomeNext = useCallback(() => {
    // Welcome is UI-only. Mark the start of real progress by writing
    // `current_step: "questionnaire"` even though the store is already
    // initialized there — this ensures that once backed by the server,
    // the initial upsert lands and the user is considered "started".
    void advance({ current_step: "questionnaire" });
    setStep("questionnaire");
  }, [advance]);

  const handleQuestionnaireSubmit = useCallback(
    (answers: QuestionnaireAnswers) => {
      // Persist the answers + advance the stored current_step. Fire and
      // forget — today `advance` is a synchronous in-memory write; once
      // backed by PATCH /api/me/onboarding the render transition still
      // feels instant because we don't await it (the UI has nothing
      // useful to show while a 50ms network call is in flight).
      void advance({
        questionnaire: answers,
        current_step: "workspace",
      });
      setStep("workspace");
    },
    [advance],
  );

  const handleWorkspaceCreated = useCallback((ws: Workspace) => {
    setWorkspace(ws);
    // Publish the newly-created workspace as "current" so the API client
    // sends `X-Workspace-Slug` on subsequent calls (runtime list, agent
    // create). Onboarding lives outside the WorkspaceRouteLayout that
    // normally owns this singleton, so nothing else sets it for us. The
    // layout will re-set it (idempotently) when the user lands on
    // /<slug>/issues after onComplete.
    setCurrentWorkspace(ws.slug, ws.id);
    setStep("runtime");
  }, []);

  const handleWorkspaceSkip = useCallback(() => {
    // Skip is only exposed when the user already has a workspace — so
    // there's always a runtime context to advance into. The zero-ws
    // case is a hard gate (no Skip button shown), enforced by
    // passing `onSkip={undefined}` below.
    setStep("runtime");
  }, []);

  const handleRuntimeNext = useCallback((rt: AgentRuntime | null) => {
    setRuntime(rt);
    // No runtime → can't build a CreateAgentRequest (runtime_id is
    // required), so skip the agent step entirely.
    setStep(rt ? "agent" : "complete");
  }, []);

  const handleAgentCreated = useCallback((created: Agent) => {
    setAgent(created);
    setStep("complete");
  }, []);

  const handleAgentSkip = useCallback(() => {
    setStep("complete");
  }, []);

  const handleFinish = useCallback(() => {
    onComplete(workspace ?? undefined);
  }, [workspace, onComplete]);

  const handleWaitlist = useCallback(
    async (email: string) => {
      await advance({ cloud_waitlist_email: email });
      await complete({});
      onComplete(workspace ?? undefined);
    },
    [advance, complete, workspace, onComplete],
  );

  // Welcome renders standalone (no progress header, its own vertical
  // centering). All other steps render under a shared wrapper whose
  // job is to provide a stable visual anchor — StepHeader at the top,
  // step content below — so transitioning between steps changes only
  // the content, not the header position or vertical baseline.
  if (step === "welcome") {
    return <StepWelcome onNext={handleWelcomeNext} />;
  }

  const storeStep: StoreStep = mapLocalToStoreStep(step);

  return (
    <div className="flex w-full flex-col gap-8">
      <StepHeader currentStep={storeStep} />
      {step === "questionnaire" && (
        <StepQuestionnaire
          initial={storedQuestionnaire}
          onSubmit={handleQuestionnaireSubmit}
        />
      )}
      {step === "workspace" && (
        <StepWorkspace
          onCreated={handleWorkspaceCreated}
          onSkip={
            workspaces.length > 0 ? handleWorkspaceSkip : undefined
          }
        />
      )}
      {step === "runtime" && runtimeWorkspace && (
        runtimeInstructions ? (
          <StepPlatformFork
            wsId={runtimeWorkspace.id}
            onNext={handleRuntimeNext}
            onWaitlist={handleWaitlist}
            cliInstructions={runtimeInstructions}
          />
        ) : (
          <StepRuntimeConnect
            wsId={runtimeWorkspace.id}
            onNext={handleRuntimeNext}
          />
        )
      )}
      {step === "agent" && runtime && (
        <StepAgent
          runtime={runtime}
          onCreated={handleAgentCreated}
          onSkip={handleAgentSkip}
        />
      )}
      {step === "complete" && (
        <StepComplete agent={agent} onFinish={handleFinish} />
      )}
    </div>
  );
}

/**
 * Bridge the flow's local UI step union to the canonical store enum.
 * `complete` is the current UI placeholder for `first_issue` (Step 5
 * not yet implemented); once it ships, the two unions converge and
 * this mapping collapses into an identity.
 */
function mapLocalToStoreStep(
  step: Exclude<OnboardingStep, "welcome">,
): StoreStep {
  switch (step) {
    case "complete":
      return "first_issue";
    default:
      return step;
  }
}
