"use client";

import { useCallback, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import {
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  saveQuestionnaire,
  useBootstrapMika,
  useWelcomeStore,
  type OnboardingStep,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { AgentRuntime, Workspace } from "@multica/core/types";
import { StepWelcome } from "./steps/step-welcome";
import { StepShell } from "./components/step-shell";
import { StepAboutYou } from "./steps/step-about-you";
import { StepWorkspace } from "./steps/step-workspace";
import { StepRuntimeConnect } from "./steps/step-runtime-connect";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { OnboardingLogoutButton } from "./components/onboarding-logout-button";
import { getMikaOnboarding, pickContentLang } from "./templates";
import { useT } from "../i18n";

const EMPTY_QUESTIONNAIRE: QuestionnaireAnswers = {
  source: [],
  source_other: null,
  source_skipped: false,
  role: null,
  role_other: null,
  role_skipped: false,
  use_case: [],
  use_case_other: null,
  use_case_skipped: false,
  version: 2,
};

/**
 * Coerce a stored questionnaire slot into the array shape used by the
 * current UI. Earlier versions of this app wrote `source` / `use_case`
 * as a single string; tolerate that on read so a user who started
 * onboarding before this change doesn't see their previous answer
 * disappear on re-entry. Empty string and null both collapse to [].
 */
function coerceToArray<T extends string>(value: unknown): T[] {
  if (Array.isArray(value)) {
    return value.filter((v): v is T => typeof v === "string" && v.length > 0);
  }
  if (typeof value === "string" && value.length > 0) {
    return [value as T];
  }
  return [];
}

/**
 * Merge persisted answers into the empty default. Re-entry pre-fills
 * answered slots but treats `*_skipped` as fresh (the user can answer
 * this time) — the v1 skip marker is dropped on read, the analytics
 * record of the prior skip stays in the DB.
 */
function mergeQuestionnaire(
  raw: Record<string, unknown>,
): QuestionnaireAnswers {
  const merged = {
    ...EMPTY_QUESTIONNAIRE,
    ...(raw as Partial<QuestionnaireAnswers>),
  };
  return {
    ...merged,
    source: coerceToArray<QuestionnaireAnswers["source"][number]>(raw.source),
    use_case: coerceToArray<QuestionnaireAnswers["use_case"][number]>(
      raw.use_case,
    ),
    source_skipped: false,
    role_skipped: false,
    use_case_skipped: false,
  };
}

/**
 * Shell's onComplete contract carries the workspace plus an optional
 * destination. Runtime-connected onboarding opens the real Mika conversation
 * started by the final step; other exits land on the workspace issue list.
 *
 * Three exit shapes feed onComplete:
 *   - Skip-existing (Welcome): completeOnboarding marks onboarded; navigate
 *     to the existing workspace's issue list.
 *   - Runtime-skipped (no runtime on Step 3): completeOnboarding marks
 *     onboarded; we push a {choice:"skip"} welcome signal and navigate
 *     to the workspace. The welcome hook in the workspace shell creates
 *     one install-runtime guide issue on landing.
 *   - Runtime-connected: create or repair the workspace's Mika on the selected
 *     runtime, start one hidden onboarding kickoff, mark onboarding complete,
 *     and open Mika's real chat. No fixed specialist team is created.
 *
 * This file never touches createAgent / createIssue. The runtime-skipped
 * guide flow remains in `packages/views/workspace/welcome-after-onboarding.tsx`.
 */
interface OnboardingFlowProps {
  onComplete: (
    workspace?: Workspace,
    destination?: OnboardingDestination,
  ) => void;
  /** "new_workspace" is the same flow run by someone who already uses
   *  Multica: it starts at the workspace step, because the intro and the
   *  questionnaire only make sense once per person, and it always creates a
   *  workspace rather than offering to continue with an existing one. */
  mode?: OnboardingMode;
  /** Required in "new_workspace" mode: first-run onboarding has no way out
   *  except signing out, but someone creating a second workspace must be able
   *  to change their mind and return to where they were. */
  onCancel?: () => void;
  runtimeInstructions?: React.ReactNode;
  /** Desktop wires this to restart the bundled daemon so a freshly
   *  installed agent CLI gets picked up on the runtime step. Web omits
   *  it — its CLI install flow already runs on the user's machine and
   *  the embedded picker reacts to daemon:register events. */
  onRuntimeRefresh?: () => void | Promise<void>;
  /** Desktop wires this to the local daemon's live status so the runtime
   *  step doesn't flash "no runtime found" while the daemon is still booting
   *  or probing CLI versions (MUL-5119). Web omits it. */
  runtimesPending?: boolean;
}

export function OnboardingFlow(props: OnboardingFlowProps) {
  return <OnboardingStepFlow {...props} />;
}

function OnboardingStepFlow({
  onComplete,
  mode = "first_run",
  onCancel,
  runtimeInstructions,
  onRuntimeRefresh,
  runtimesPending,
}: OnboardingFlowProps) {
  const { t, i18n } = useT("onboarding");
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

  const isNewWorkspace = mode === "new_workspace";
  const [step, setStep] = useState<OnboardingStep>(
    isNewWorkspace ? "workspace" : "welcome",
  );
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  // Raised by whichever step has a request in flight; locks Back and the
  // rail. Only the workspace step sets it today.
  const [stepBusy, setStepBusy] = useState(false);
  const bootstrapMika = useBootstrapMika(workspace?.id ?? "");

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
  const existingWorkspace = isNewWorkspace
    ? workspace
    : (workspace ?? workspaces[0] ?? null);
  const canSkipWelcome = workspacesFetched && workspaces.length > 0;

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
  // without creating new workspace content.
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
      // Deliberately NOT setCurrentWorkspace: that singleton is also written by
      // the desktop tab system, which reclaims it whenever the new workspace
      // has no tab group yet. Racing it sent the rest of this flow — Mika, the
      // session, the kickoff — into the previously-active workspace. Every call
      // from here on names its target workspace instead, and the switch happens
      // once, on the navigation in onComplete.
      advanceFrom("workspace");
    },
    [advanceFrom],
  );

  const handleRuntimeNext = useCallback(
    async (rt: AgentRuntime | null, model?: string) => {
      if (!workspace) return;
      // A connected runtime provisions only Mika and immediately opens the
      // real interactive onboarding conversation. Specialists are created
      // later, only when the member's actual workflow justifies them.
      if (rt) {
        const contentLang = pickContentLang(i18n.language);
        try {
          // The earlier questionnaire saves are deliberately optimistic. Flush
          // the latest snapshot here so the server-authored kickoff can read
          // reliable role/use-case context instead of racing the last PATCH.
          await saveQuestionnaire(answers);
          const result = await bootstrapMika.mutateAsync({
            workspaceSlug: workspace.slug,
            runtimeId: rt.id,
            model,
            returning: isNewWorkspace,
            ...getMikaOnboarding(contentLang),
          });
          await completeOnboarding("full", workspace.id);
          onComplete(workspace, {
            kind: "chat",
            sessionId: result.chatSession.id,
          });
        } catch (err) {
          toast.error(
            err instanceof Error
              ? err.message
              : t(($) => $.errors.mika_setup_failed),
          );
          throw err;
        }
        return;
      }
      try {
        await completeOnboarding("runtime_skipped", workspace.id);
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
        );
        return;
      }
      useWelcomeStore.getState().set({
        workspaceId: workspace.id,
        choice: "skip",
      });
      onComplete(workspace, undefined);
    },
    [answers, bootstrapMika, i18n.language, workspace, onComplete, t],
  );

  const handleBack = useCallback((from: OnboardingStep) => {
    // The workspace step is the entry point in new-workspace mode, so its
    // back affordance leaves the flow instead of walking into a step this
    // mode deliberately skips. Only reachable before the workspace exists —
    // see `runtimeStepBack` for why there is no way back into this step
    // afterwards.
    if (isNewWorkspace && from === "workspace") {
      onCancel?.();
      return;
    }
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx <= 0) {
      // About you (the first persisted step) returns to Welcome.
      setStep("welcome");
      return;
    }
    const prev = ONBOARDING_STEP_ORDER[idx - 1]!;
    setStep(prev);
  }, [isNewWorkspace, onCancel]);

  // Once a workspace exists there is nothing left to cancel, so new-workspace
  // mode drops the back affordance on the runtime step. Walking back would
  // reach the workspace step, whose own back button means "leave" — and
  // leaving there would strand a workspace with no runtime, no Mika, and no
  // guidance. The remaining exits both end somewhere coherent: Skip runs the
  // runtime-skipped path (guide issue + land in the new workspace), and
  // continuing provisions Mika.
  // Log out lives at the foot of the progress rail rather than pinned to the
  // window corner: pinned put it outside the measure and above Back, which
  // read as a second header row.
  const headerTrailing = <OnboardingLogoutButton inline />;

  const runtimeStepBack = isNewWorkspace
    ? undefined
    : () => handleBack("runtime");

  // The rail only ever walks backwards — it renders earlier steps as links and
  // later ones as plain text, because moving forward has to run the current
  // step's validation and submit. New-workspace mode gets no rail navigation
  // at all: it enters at the workspace step and, once that workspace exists,
  // every step behind it is gone. Same invariant `runtimeStepBack` enforces.
  const handleStepChange = isNewWorkspace ? undefined : setStep;

  // ONE shell for the whole flow, rendered here rather than by each step.
  // Every step used to render its own <StepShell>, and because each step is a
  // different component type React tore the shell down and built a new one on
  // every transition — remounting the "persistent" rail, restarting its canvas,
  // and replaying the shell's fade from opacity 0. That full-window re-fade is
  // the flash. Hoisting the shell makes the rail actually persistent and leaves
  // only the step body swapping.
  if (step === "welcome") {
    // Welcome has no rail, so the escape hatch stays pinned there.
    return (
      <>
        <OnboardingLogoutButton />
        <StepWelcome
          onNext={handleWelcomeNext}
          onSkip={canSkipWelcome ? handleWelcomeSkip : undefined}
          isWeb={isWeb}
        />
      </>
    );
  }

  const stepBack =
    step === "about_you"
      ? () => handleBack("about_you")
      : step === "workspace"
        ? () => handleBack("workspace")
        : runtimeStepBack;

  return (
    <StepShell
      currentStep={step}
      onBack={stepBack}
      backDisabled={stepBusy}
      onStepChange={handleStepChange}
      chromeFooter={headerTrailing}
    >
      {step === "about_you" && (
        <StepAboutYou
          answers={answers}
          onChange={applyAnswers}
          onAdvance={() => advanceFrom("about_you")}
          onSkip={() => advanceFrom("about_you")}
        />
      )}

      {step === "workspace" && (
        <StepWorkspace
          existing={existingWorkspace}
          onCreated={handleWorkspaceCreated}
          onBusyChange={setStepBusy}
        />
      )}

      {/* Step 3 has two paths:
            - Desktop (no cliInstructions slot) drives the local daemon's
              runtime list directly.
            - Web offers Download / CLI / Cloud; under the CLI path it embeds
              the live probe, and Cloud is a soft exit via the waitlist. */}
      {step === "runtime" &&
        workspace &&
        (!runtimeInstructions ? (
          <StepRuntimeConnect
            wsId={workspace.id}
            wsSlug={workspace.slug}
            onNext={handleRuntimeNext}
            onRefresh={onRuntimeRefresh}
            runtimesPending={runtimesPending}
          />
        ) : (
          <StepPlatformFork
            wsId={workspace.id}
            wsSlug={workspace.slug}
            onNext={handleRuntimeNext}
            cliInstructions={runtimeInstructions}
          />
        ))}
    </StepShell>
  );
}

export type OnboardingMode = "first_run" | "new_workspace";

export type OnboardingDestination =
  | { kind: "issue"; issueId: string }
  | {
      kind: "chat";
      sessionId: string;
    };

export type { OnboardingStep };
