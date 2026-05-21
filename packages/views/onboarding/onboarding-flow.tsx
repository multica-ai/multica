"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { captureEvent } from "@multica/core/analytics";
import { setCurrentWorkspace } from "@multica/core/platform";
import { useAuthStore } from "@multica/core/auth";
import {
  bootstrapNoRuntimeOnboarding,
  bootstrapRuntimeOnboarding,
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  saveQuestionnaire,
  type OnboardingStep,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import { issueKeys } from "@multica/core/issues/queries";
import { workspaceListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { AgentRuntime, Workspace } from "@multica/core/types";
import { StepWelcome } from "./steps/step-welcome";
import { StepSource } from "./steps/step-source";
import { StepRole } from "./steps/step-role";
import { StepUseCase } from "./steps/step-use-case";
import { StepWorkspace } from "./steps/step-workspace";
import { StepRuntimeConnect } from "./steps/step-runtime-connect";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { StepTeammate } from "./steps/step-teammate";
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
 *   onComplete(workspace?, issueId?) — if an issue id is present, navigate
 *   straight into that onboarding issue; otherwise navigate into the
 *   workspace issues list. Runtime-connected onboarding creates one
 *   Multica Helper agent plus one issue; runtime-skipped onboarding creates one
 *   self-serve install-runtime issue.
 */
export function OnboardingFlow({
  onComplete,
  runtimeInstructions,
  onRuntimeRefresh,
}: {
  onComplete: (workspace?: Workspace, issueId?: string) => void;
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
  // server-side (idempotent via COALESCE on onboarded_at); when the
  // target workspace has no runtime yet, the server seeds the same
  // install-runtime issue as Step 3 Skip so the user lands on a
  // concrete next step.
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

  const handleRuntimeNext = useCallback(
    async (rt: AgentRuntime | null) => {
      if (!workspace) return;
      if (!rt) {
        // No runtime -> no agent execution yet. Create one focused
        // install-runtime onboarding issue so the user lands on a
        // concrete next step.
        try {
          const result = await bootstrapNoRuntimeOnboarding(workspace.id);
          await qc.invalidateQueries({ queryKey: issueKeys.all(workspace.id) });
          onComplete(workspace, result.issue_id || undefined);
        } catch (err) {
          toast.error(
            err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
          );
        }
        return;
      }

      setRuntime(rt);
      advanceFrom("runtime");
    },
    [workspace, qc, onComplete, t, advanceFrom],
  );

  const handleCreateTeammate = useCallback(async () => {
    if (!workspace || !runtime) return;

    try {
      const result = await bootstrapRuntimeOnboarding(workspace.id, runtime.id);
      await Promise.all([
        qc.invalidateQueries({ queryKey: workspaceKeys.agents(workspace.id) }),
        qc.invalidateQueries({ queryKey: issueKeys.all(workspace.id) }),
      ]);
      onComplete(workspace, result.issue_id || undefined);
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.step_teammate.create_failed),
      );
      throw err;
    }
  }, [workspace, runtime, qc, onComplete, t]);

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

  // Welcome, Questionnaire, and Workspace own full-bleed two-column
  // layouts (hero / side panel) with their own DragStrip + StepHeader.
  // The runtime step owns its own full-bleed shell.
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

  if (step === "teammate" && workspace && runtime) {
    return (
      <StepTeammate
        runtime={runtime}
        onCreate={handleCreateTeammate}
        onBack={() => handleBack("teammate")}
      />
    );
  }

  return null;
}

export type { OnboardingStep };
