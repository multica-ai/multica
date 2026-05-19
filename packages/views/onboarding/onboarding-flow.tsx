"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { captureEvent } from "@multica/core/analytics";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import {
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  saveQuestionnaire,
  type OnboardingCompletionPath,
  type OnboardingStep,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import { workspaceListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Agent, AgentRuntime, Workspace } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "./components/step-header";
import { StepWelcome } from "./steps/step-welcome";
import { StepSource } from "./steps/step-source";
import { StepRole } from "./steps/step-role";
import { StepUseCase } from "./steps/step-use-case";
import { StepWorkspace } from "./steps/step-workspace";
import { StepRuntimeConnect } from "./steps/step-runtime-connect";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { StepAgent } from "./steps/step-agent";
import { StepFirstIssue } from "./steps/step-first-issue";
import { useT } from "../i18n";

const EMPTY_QUESTIONNAIRE: QuestionnaireAnswers = {
  source: null,
  source_other: null,
  source_skipped: false,
  role: null,
  role_other: null,
  role_skipped: false,
  use_case: null,
  use_case_other: null,
  use_case_skipped: false,
  version: 2,
};

/**
 * Merge persisted answers into the empty default. Re-entry pre-fills
 * answered slots but treats `*_skipped` as fresh (the user can answer
 * this time) — the v1 skip marker is dropped on read, the analytics
 * record of the prior skip stays in the DB.
 */
function mergeQuestionnaire(
  raw: Record<string, unknown>,
): QuestionnaireAnswers {
  const merged = { ...EMPTY_QUESTIONNAIRE, ...(raw as Partial<QuestionnaireAnswers>) };
  return {
    ...merged,
    source_skipped: false,
    role_skipped: false,
    use_case_skipped: false,
  };
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
  onRuntimeRefresh,
}: {
  onComplete: (workspace?: Workspace) => void;
  runtimeInstructions?: React.ReactNode;
  /** Desktop wires this to restart the bundled daemon so a freshly
   *  installed agent CLI gets picked up on the runtime step. Web omits
   *  it — its CLI install flow already runs on the user's machine and
   *  the embedded picker reacts to daemon:register events. */
  onRuntimeRefresh?: () => void | Promise<void>;
}) {
  const { t } = useT("onboarding");
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error("OnboardingFlow requires an authenticated user");
  }

  // Questionnaire answers are server-persisted and pre-fill the per-
  // question steps on re-entry. That's the only piece of onboarding
  // state persisted across sessions — which step the user is on is
  // deliberately not saved, so every entry starts at Welcome.
  const storedQuestionnaire = mergeQuestionnaire(user.onboarding_questionnaire);
  const [answers, setAnswers] = useState<QuestionnaireAnswers>(storedQuestionnaire);

  const qc = useQueryClient();

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
  const startedEmittedRef = useRef(false);
  useEffect(() => {
    if (startedEmittedRef.current || !workspacesFetched) return;
    startedEmittedRef.current = true;
    captureEvent("onboarding_started", {
      source: "onboarding",
      ...(existingWorkspace ? { workspace_id: existingWorkspace.id } : {}),
    });
  }, [existingWorkspace, workspacesFetched]);

  // The `runtimeInstructions` slot is only plumbed by the web shell
  // (desktop bundles a daemon, so a CLI install card would be noise
  // there). We reuse its presence as the web signal rather than
  // introducing a redundant prop.
  const isWeb = !!runtimeInstructions;

  // Derive "what comes after `from`" from ONBOARDING_STEP_ORDER so
  // inserting/reordering a persisted step only requires editing the
  // canonical array. Returns null if `from` is the last persisted step
  // or not in the array (callers fall back to bespoke routing).
  const nextStep = useCallback((from: OnboardingStep): OnboardingStep | null => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx < 0 || idx >= ONBOARDING_STEP_ORDER.length - 1) return null;
    return ONBOARDING_STEP_ORDER[idx + 1]!;
  }, []);

  const advanceFrom = useCallback(
    (from: OnboardingStep) => {
      const next = nextStep(from);
      if (next) setStep(next);
    },
    [nextStep],
  );

  const handleWelcomeNext = useCallback(() => {
    // Welcome is intentionally not in ONBOARDING_STEP_ORDER (it's a
    // product intro, not a persisted step), so the first persisted
    // step is hard-coded as the entry point.
    setStep(ONBOARDING_STEP_ORDER[0]!);
  }, []);

  // Apply an in-memory patch and fire-and-forget a PATCH to persist
  // it. We never block UI on the request — the next step's render is
  // what matters; a transient save failure surfaces as a toast but
  // does not roll the user back.
  const applyAnswers = useCallback(
    (patch: Partial<QuestionnaireAnswers>) => {
      setAnswers((a) => {
        const next = { ...a, ...patch };
        void saveQuestionnaire(next).catch((err) => {
          if (err instanceof Error) toast.error(err.message);
        });
        return next;
      });
    },
    [],
  );

  // "I've done this before" path — returning user who already has a
  // workspace and just wants to land there. Marks onboarding complete
  // server-side (idempotent via COALESCE on onboarded_at) and navigates
  // to their first workspace. Because starter_content_state is NULL for
  // any user reaching this button (it's freshly added), they'll see the
  // StarterContentPrompt dialog on arrival — which is correct, since
  // they never got a starter project and may want one now.
  const handleWelcomeSkip = useCallback(async () => {
    try {
      await completeOnboarding("skip_existing", workspaces[0]?.id);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
      );
      return;
    }
    onComplete(workspaces[0] ?? undefined);
  }, [workspaces, onComplete]);

  const handleWorkspaceCreated = useCallback(
    (ws: Workspace) => {
      setWorkspace(ws);
      setCurrentWorkspace(ws.slug, ws.id);
      advanceFrom("workspace");
    },
    [advanceFrom],
  );

  const handleRuntimeNext = useCallback((rt: AgentRuntime | null) => {
    setRuntime(rt);
    // Custom branch — not derived from ONBOARDING_STEP_ORDER because no
    // runtime → no agent possible, so we skip Step 4 ("agent") and go
    // straight to the finalizer. The post-landing StarterContentPrompt
    // will detect "no agent in this workspace" and offer the self-serve
    // template.
    setStep(rt ? "agent" : "first_issue");
  }, []);

  const handleAgentCreated = useCallback(
    (created: Agent) => {
      setAgent(created);
      // Mark the workspace's agent list stale so the dashboard's first
      // mount refetches and includes the just-created agent. Without
      // this, anything resolving an agent ID from the cached list (the
      // welcome issue's assignee in particular) renders as "Unknown
      // Agent" until something else triggers a refetch.
      if (workspace) {
        qc.invalidateQueries({
          queryKey: workspaceKeys.agents(workspace.id),
        });
      }
      advanceFrom("agent");
    },
    [workspace, qc, advanceFrom],
  );

  const handleBack = useCallback((from: OnboardingStep) => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx <= 0) {
      // Source (the first persisted step) returns to Welcome.
      setStep("welcome");
      return;
    }
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
        isWeb={isWeb}
      />
    );
  }

  if (step === "source") {
    return (
      <StepSource
        answers={answers}
        onChange={applyAnswers}
        onAdvance={() => advanceFrom("source")}
        onSkip={() => advanceFrom("source")}
        onBack={() => handleBack("source")}
      />
    );
  }

  if (step === "role") {
    return (
      <StepRole
        answers={answers}
        onChange={applyAnswers}
        onAdvance={() => advanceFrom("role")}
        onSkip={() => advanceFrom("role")}
        onBack={() => handleBack("role")}
      />
    );
  }

  if (step === "use_case") {
    return (
      <StepUseCase
        answers={answers}
        onChange={applyAnswers}
        onAdvance={() => advanceFrom("use_case")}
        onSkip={() => advanceFrom("use_case")}
        onBack={() => handleBack("use_case")}
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
          onRefresh={onRuntimeRefresh}
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
        questionnaire={answers}
        onCreated={handleAgentCreated}
        onBack={() => handleBack("agent")}
      />
    );
  }

  // Derive the completion-path label for Step 5. The cloud-waitlist
  // exit was removed from Step 3 (replaced with a "Coming soon" badge)
  // so this is now a binary: runtime → "full", no runtime → "runtime_skipped".
  const completionPath: OnboardingCompletionPath = runtime
    ? "full"
    : "runtime_skipped";

  return (
    <div className="animate-onboarding-enter flex min-h-full flex-col">
      <DragStrip />
      <div className="flex flex-1 flex-col items-center px-6 pb-12">
        <div className="flex w-full max-w-xl flex-col gap-8">
          <StepHeader currentStep={step} />
          {step === "first_issue" && (
            <StepFirstIssue
              onFinished={handleFinished}
              completionPath={completionPath}
              workspaceId={workspace?.id}
            />
          )}
        </div>
      </div>
    </div>
  );
}

export type { OnboardingStep };
