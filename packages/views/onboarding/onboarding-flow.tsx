"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import {
  api,
  ApiError,
  DuplicateIssueErrorBodySchema,
  type DuplicateIssueErrorBody,
  parseWithFallback,
} from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  completeOnboarding,
  createPersonalAgent,
  ONBOARDING_STEP_ORDER,
  recordOnboardingStarted,
  type CodingToolsBridge,
  type OnboardingStep,
} from "@multica/core/onboarding";
import { useWorkspaceList } from "@multica/core/workspace";
import type { Workspace } from "@multica/core/types";
import { StepWelcome } from "./steps/step-welcome";
import { StepShell } from "./components/step-shell";
import { StepWorkspace } from "./steps/step-workspace";
import { StepAgentSetup, type CreatePersonalAgentRequest } from "./steps/step-agent-setup";
import { StepPlatformFork } from "./steps/step-platform-fork";
import { useRuntimePicker } from "./components/use-runtime-picker";
import { OnboardingLogoutButton } from "./components/onboarding-logout-button";
import {
  INSTALL_RUNTIME_ISSUE_BODY,
  INSTALL_RUNTIME_ISSUE_TITLE,
  pickContentLang,
} from "./templates";
import { useT } from "../i18n";
import { issueRouteRef, type IssueRouteRef } from "@multica/core/paths";

/**
 * Shell's onComplete contract carries the workspace plus an optional
 * destination. Creating an agent opens a chat with it; skipping Step 3 opens
 * the guide issue it created; the Welcome skip lands on the workspace issue
 * list.
 *
 * Three exit shapes feed onComplete:
 *   - Skip-existing (Welcome): completeOnboarding marks onboarded; navigate
 *     to the existing workspace's issue list.
 *   - Runtime-skipped (no tool on Step 3): create one install-runtime guide
 *     issue assigned to the member, mark onboarding complete, and open that
 *     issue. Once a tool is connected, the Runtimes page offers to create the
 *     member's agent.
 *   - Agent created: create the member's private agent on the chosen runtime,
 *     open a chat with it, mark onboarding complete, and land in that chat.
 *
 * Both workspace-provisioning exits finish their writes BEFORE
 * completeOnboarding: it refreshes the user, and the web shell unmounts this
 * flow and redirects as soon as `onboarded_at` is set.
 */
interface OnboardingFlowProps {
  onComplete: (
    workspace?: Workspace,
    destination?: OnboardingDestination,
  ) => void;
  /** "new_workspace" is the same flow run by someone who already uses
   *  Multica: it starts at the workspace step, because the intro only makes
   *  sense once per person, and it always creates a workspace rather than
   *  offering to continue with an existing one. */
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
  /** Desktop wires this to the local daemon's live status so Step 3 does not
   *  restart a daemon that is still booting or probing CLI versions
   *  (MUL-5119). Web omits it. */
  runtimesPending?: boolean;
  /** Desktop only: installs and signs in to AI coding tools on this machine. */
  codingTools?: CodingToolsBridge;
  /** Desktop only: the bundled daemon, whose runtimes are this machine's. */
  localDaemonId?: string | null;
}

export function OnboardingFlow({
  onComplete,
  mode = "first_run",
  onCancel,
  runtimeInstructions,
  onRuntimeRefresh,
  runtimesPending,
  codingTools,
  localDaemonId,
}: OnboardingFlowProps) {
  const { t, i18n } = useT("onboarding");
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error("OnboardingFlow requires an authenticated user");
  }

  const isNewWorkspace = mode === "new_workspace";

  const startRecorded = useRef(isNewWorkspace);
  useEffect(() => {
    if (startRecorded.current) return;
    startRecorded.current = true;
    void recordOnboardingStarted(user).catch(() => undefined);
  }, [user]);
  const [step, setStep] = useState<OnboardingStep>(
    isNewWorkspace ? "workspace" : "welcome",
  );
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const qc = useQueryClient();
  // Raised by whichever step has a request in flight; locks Back and the
  // rail. Only the workspace step sets it today.
  const [stepBusy, setStepBusy] = useState(false);
  // Keyed by workspace so retrying Skip after completeOnboarding failed reuses
  // the guide issue instead of creating a second one.
  const guideIssueRef = useRef<{ workspaceId: string; issueRef: IssueRouteRef } | null>(
    null,
  );

  // Fetched at Welcome and the workspace step. The workspace step uses it to
  // detect a workspace from an earlier abandoned onboarding (so StepWorkspace
  // shows "Continue with {name}" instead of CreateWorkspaceForm — avoiding the
  // slug conflict that creation would hit). Welcome uses it to decide whether
  // to render the "I've done this before" skip button — only shown when the
  // user already has at least one workspace, otherwise skipping would land
  // them in limbo.
  const {
    workspaces,
    ready: workspacesReady,
    unavailable: workspacesUnavailable,
  } = useWorkspaceList({
    enabled: step === "welcome" || step === "workspace",
  });

  // Someone who made a workspace but never finished — on another computer, or
  // in the browser before opening Desktop — picks up at Step 3 instead of
  // walking the intro again. Decided once, on arrival; a failed workspace
  // load decides "nothing to resume" rather than holding Welcome back.
  const [resumeDecided, setResumeDecided] = useState(isNewWorkspace);
  useEffect(() => {
    if (resumeDecided || (!workspacesReady && !workspacesUnavailable)) return;
    setResumeDecided(true);
    const existing = workspaces[0];
    if (existing && step === "welcome") {
      setWorkspace(existing);
      setStep("runtime");
    }
  }, [resumeDecided, step, workspaces, workspacesReady, workspacesUnavailable]);
  const existingWorkspace = isNewWorkspace
    ? workspace
    : (workspace ?? workspaces[0] ?? null);
  const canSkipWelcome = workspacesReady && workspaces.length > 0;

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
      // has no tab group yet. Racing it sent the rest of this flow — the
      // agent and its chat — into the previously-active workspace. Every call
      // from here on names its target workspace instead, and the switch happens
      // once, on the navigation in onComplete.
      advanceFrom("workspace");
    },
    [advanceFrom],
  );

  const handleCreateAgent = useCallback(
    async ({ runtime, name, description, avatarUrl }: CreatePersonalAgentRequest) => {
      if (!workspace) return;
      const { chatSession } = await createPersonalAgent({
        workspaceSlug: workspace.slug,
        workspaceId: workspace.id,
        queryClient: qc,
        runtimeId: runtime.id,
        name,
        description,
        avatarUrl,
      });
      await completeOnboarding("full", workspace.id);
      onComplete(workspace, { kind: "chat", sessionId: chatSession.id });
    },
    [workspace, onComplete, qc],
  );

  const handleRuntimeSkip = useCallback(async () => {
    if (!workspace) return;
    let issueRef =
      guideIssueRef.current?.workspaceId === workspace.id
        ? guideIssueRef.current.issueRef
        : null;
    if (!issueRef) {
      const contentLang = pickContentLang(i18n.language);
      try {
        const issue = await api.createIssue(
          {
            title: INSTALL_RUNTIME_ISSUE_TITLE[contentLang],
            description: INSTALL_RUNTIME_ISSUE_BODY[contentLang],
            status: "in_progress",
            priority: "high",
            assignee_type: "member",
            assignee_id: user.id,
          },
          workspace.slug,
        );
        issueRef = issueRouteRef(issue);
      } catch (err) {
        // A reload, or a create whose response was lost, leaves the guide
        // from an earlier attempt in place; the duplicate guard then
        // answers with that issue, which is the one to open.
        const duplicate =
          err instanceof ApiError && err.status === 409
            ? parseWithFallback<DuplicateIssueErrorBody | null>(
                err.body,
                DuplicateIssueErrorBodySchema,
                null,
                { endpoint: "POST /api/issues (active_duplicate_issue)" },
              )
            : null;
        if (!duplicate) {
          toast.error(t(($) => $.errors.guide_failed));
          return;
        }
        issueRef = issueRouteRef(duplicate.issue);
      }
      guideIssueRef.current = { workspaceId: workspace.id, issueRef };
    }
    try {
      await completeOnboarding("runtime_skipped", workspace.id);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
      );
      return;
    }
    onComplete(workspace, { kind: "issue", issueRef });
  }, [i18n.language, workspace, onComplete, t, user.id]);

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
      setStep("welcome");
      return;
    }
    const prev = ONBOARDING_STEP_ORDER[idx - 1]!;
    setStep(prev);
  }, [isNewWorkspace, onCancel]);

  // Once a workspace exists there is nothing left to cancel, so new-workspace
  // mode drops the back affordance on the runtime step. Walking back would
  // reach the workspace step, whose own back button means "leave" — and
  // leaving there would strand a workspace with no runtime, no agent, and no
  // guidance. The remaining exits both end somewhere coherent: Skip runs the
  // runtime-skipped path (create the guide issue and open it), and
  // continuing creates the member's agent.
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
  if (step === "welcome" && !resumeDecided) {
    // Hold Welcome back until the resume check above has the workspace list.
    return null;
  }
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

  const stepBack = step === "workspace" ? () => handleBack("workspace") : runtimeStepBack;

  return (
    <StepShell
      currentStep={step}
      onBack={stepBack}
      backDisabled={stepBusy}
      onStepChange={handleStepChange}
      chromeFooter={headerTrailing}
    >
      {step === "workspace" && (
        <StepWorkspace
          existing={existingWorkspace}
          onCreated={handleWorkspaceCreated}
          onBusyChange={setStepBusy}
        />
      )}

      {/* Step 3: Desktop installs tools on this machine and creates the
          agent in place. Web hands off until one of the member's computers
          is connected, then creates the agent the same way. */}
      {step === "runtime" &&
        workspace &&
        (!runtimeInstructions ? (
          <StepAgentSetup
            wsId={workspace.id}
            wsSlug={workspace.slug}
            userName={user.name}
            codingTools={codingTools}
            localDaemonId={localDaemonId}
            onRuntimeRefresh={onRuntimeRefresh}
            runtimesPending={runtimesPending}
            onCreate={handleCreateAgent}
            onSkip={handleRuntimeSkip}
          />
        ) : (
          <WebAgentStep
            workspace={workspace}
            userName={user.name}
            cliInstructions={runtimeInstructions}
            onCreate={handleCreateAgent}
            onSkip={handleRuntimeSkip}
          />
        ))}
    </StepShell>
  );
}

/** Web Step 3: hand off until a computer is connected, then create the agent. */
function WebAgentStep({
  workspace,
  userName,
  cliInstructions,
  onCreate,
  onSkip,
}: {
  workspace: Workspace;
  userName: string;
  cliInstructions: React.ReactNode;
  onCreate: (request: CreatePersonalAgentRequest) => Promise<void>;
  onSkip: () => Promise<void>;
}) {
  const { runtimes } = useRuntimePicker(workspace.id, workspace.slug);
  if (runtimes.some((runtime) => runtime.status === "online")) {
    return (
      <StepAgentSetup
        wsId={workspace.id}
        wsSlug={workspace.slug}
        userName={userName}
        onCreate={onCreate}
        onSkip={onSkip}
      />
    );
  }
  return <StepPlatformFork onSkip={onSkip} cliInstructions={cliInstructions} />;
}

export type OnboardingMode = "first_run" | "new_workspace";

export type OnboardingDestination =
  | { kind: "issue"; issueRef: IssueRouteRef }
  | {
      kind: "chat";
      sessionId: string;
    };

export type { OnboardingStep };
