"use client";

import { useCallback, useRef, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useConfigStore } from "@multica/core/config";
import { matchLocale } from "@multica/core/i18n";
import {
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  useGettingStartedStore,
  WEB_ONBOARDING_STEP_ORDER,
  type OnboardingStep,
} from "@multica/core/onboarding";
import { useWorkspaceList } from "@multica/core/workspace";
import { useCreateWorkspace } from "@multica/core/workspace/mutations";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { useLocale, useT } from "../i18n";
import { useLocalDaemonStatus } from "../platform";
import { OnboardingPage } from "./components/onboarding-page";
import { EMPTY_TOOL_SELECTION, type ToolSelection } from "./components/tool-picker";
import { useOnboardingRuntimes } from "./components/use-onboarding-runtimes";
import {
  createDefaultWorkspace,
  createFirstTask,
  defaultWorkspaceSlugBase,
  tightenCjkSpacing,
  ensureToolAgent,
  type FirstTaskFolder,
} from "./provisioning";
import { StepConnect } from "./steps/step-connect";
import { StepFirstTask } from "./steps/step-first-task";
import { StepTool } from "./steps/step-tool";
import { StepWelcome } from "./steps/step-welcome";
import { StepWorkspace } from "./steps/step-workspace";

/**
 * Shell's onComplete contract: the workspace to land in, and the issue to
 * open when onboarding made one.
 *
 *   - Welcome skip ("I've used Multica before"): mark onboarded, land on the
 *     existing workspace's issues.
 *   - Tool or connect skipped: mark onboarded, land on the issues list.
 *   - Browse first: the agent exists, no task; land on the issues list.
 *   - First task: the agent and the task exist; open the task.
 *
 * Every exit finishes its writes BEFORE completeOnboarding: it refreshes the
 * user, and the web shell unmounts this flow and redirects as soon as
 * `onboarded_at` is set.
 */
interface OnboardingFlowProps {
  onComplete: (workspace?: Workspace, destination?: OnboardingDestination) => void;
  /** "new_workspace" is the same flow run by someone who already uses
   *  Multica: it names the workspace instead of creating one for them, and
   *  skips the welcome, which only makes sense once per person. */
  mode?: OnboardingMode;
  /** Required in "new_workspace" mode: someone creating a second workspace
   *  must be able to change their mind and return to where they were. */
  onCancel?: () => void;
  /** Web only — how to connect a computer from a terminal. Its presence is
   *  also the web signal: desktop bundles the daemon and never passes it. */
  runtimeInstructions?: ReactNode;
}

export function OnboardingFlow({
  onComplete,
  mode = "first_run",
  onCancel,
  runtimeInstructions,
}: OnboardingFlowProps) {
  const { t } = useT("onboarding");
  const locale = useLocale();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error("OnboardingFlow requires an authenticated user");
  }
  const creationDisabled = useConfigStore((s) => s.workspaceCreationDisabled);
  const createWorkspace = useCreateWorkspace();
  const daemon = useLocalDaemonStatus();

  const isNewWorkspace = mode === "new_workspace";
  const isWeb = !!runtimeInstructions;
  const [step, setStep] = useState<OnboardingStep>(isNewWorkspace ? "workspace" : "welcome");
  const [workspace, setWorkspace] = useState<Workspace | null>(null);
  const [workspaceBusy, setWorkspaceBusy] = useState(false);

  const { workspaces, ready: workspacesReady } = useWorkspaceList({
    enabled: step === "welcome",
  });
  const canSkipWelcome = !isNewWorkspace && workspacesReady && workspaces.length > 0;

  const order: OnboardingStep[] = [
    ...(isNewWorkspace ? (["workspace"] as const) : []),
    ...(isWeb ? WEB_ONBOARDING_STEP_ORDER : ONBOARDING_STEP_ORDER),
  ];
  const progressIndex = order.indexOf(step);
  const progress =
    progressIndex >= 0 ? { step: progressIndex + 1, total: order.length } : undefined;

  const enterWorkspace = useCallback(
    (ws: Workspace) => {
      // Deliberately NOT setCurrentWorkspace: that singleton is also written by
      // the desktop tab system, which reclaims it whenever the new workspace
      // has no tab group yet. Every call from here on names its target
      // workspace instead, and the switch happens once, in onComplete.
      setWorkspace(ws);
      setStep(isWeb ? "connect" : "tool");
    },
    [isWeb],
  );

  // First run names the workspace for the member. Someone who left an earlier
  // run part-way already has one, and continues in it.
  const handleStart = useCallback(async () => {
    if (workspace) {
      enterWorkspace(workspace);
      return;
    }
    const existing = (await qc.fetchQuery(workspaceListOptions()))[0];
    if (existing) {
      enterWorkspace(existing);
      return;
    }
    if (creationDisabled) {
      setStep("workspace");
      return;
    }
    const ownerName = user.name.trim().split(/\s+/)[0] || user.email.split("@")[0] || "";
    try {
      const ws = await createDefaultWorkspace({
        name: tightenCjkSpacing(t(($) => $.defaults.workspace_name, { name: ownerName })),
        slugBase: defaultWorkspaceSlugBase(user, matchLocale([locale])),
        create: (input) => createWorkspace.mutateAsync(input),
      });
      enterWorkspace(ws);
    } catch (err) {
      // The instance may refuse creation for reasons the config did not
      // announce; the workspace step explains that and offers the way out.
      if (err instanceof ApiError && err.status === 403) {
        setStep("workspace");
        return;
      }
      toast.error(err instanceof Error ? err.message : t(($) => $.errors.workspace_failed));
    }
  }, [workspace, qc, creationDisabled, user, t, locale, createWorkspace, enterWorkspace]);

  const handleWelcomeSkip = useCallback(async () => {
    try {
      await completeOnboarding("skip_existing", workspaces[0]?.id);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.errors.skip_failed));
      return;
    }
    onComplete(workspaces[0] ?? undefined);
  }, [workspaces, onComplete, t]);

  if (step === "welcome") {
    return (
      <OnboardingPage>
        <StepWelcome
          onStart={handleStart}
          onSkip={canSkipWelcome ? handleWelcomeSkip : undefined}
          isWeb={isWeb}
          detectedProviders={daemon.agents}
          detecting={daemon.starting}
        />
      </OnboardingPage>
    );
  }

  if (step === "workspace" || !workspace) {
    return (
      <OnboardingPage
        progress={progress}
        showLogout={!isNewWorkspace}
        onBack={isNewWorkspace ? onCancel : () => setStep("welcome")}
        backDisabled={workspaceBusy}
      >
        <StepWorkspace
          existing={null}
          onCreated={enterWorkspace}
          onBusyChange={setWorkspaceBusy}
        />
      </OnboardingPage>
    );
  }

  return (
    <WorkspaceSteps
      key={workspace.id}
      workspace={workspace}
      step={step}
      setStep={setStep}
      order={order}
      progress={progress}
      isNewWorkspace={isNewWorkspace}
      cliInstructions={runtimeInstructions}
      daemonId={daemon.daemonId}
      onComplete={onComplete}
    />
  );
}

/**
 * The steps that run inside the workspace being set up. Split out because the
 * runtime list is workspace-scoped: the daemon registers per workspace, so
 * there is nothing to list before the workspace exists.
 */
function WorkspaceSteps({
  workspace,
  step,
  setStep,
  order,
  progress,
  isNewWorkspace,
  cliInstructions,
  daemonId,
  onComplete,
}: {
  workspace: Workspace;
  step: OnboardingStep;
  setStep: (step: OnboardingStep) => void;
  order: OnboardingStep[];
  progress?: { step: number; total: number };
  isNewWorkspace: boolean;
  cliInstructions?: ReactNode;
  daemonId: string | null;
  onComplete: OnboardingFlowProps["onComplete"];
}) {
  const { t } = useT("onboarding");
  const user = useAuthStore((s) => s.user)!;
  const activateGettingStarted = useGettingStartedStore((s) => s.activate);
  const { runtimes, detecting, canRescan, rescan } = useOnboardingRuntimes(
    workspace.id,
    workspace.slug,
  );
  const [selection, setSelection] = useState<ToolSelection>(EMPTY_TOOL_SELECTION);
  const runtime = runtimes.find((r) => r.id === selection.runtimeId) ?? null;
  // A retry after completeOnboarding failed must open the task it already
  // made rather than file a second one.
  const firstIssueRef = useRef<string | null>(null);
  const [returnedToConnect, setReturnedToConnect] = useState(false);

  // Back never walks into the workspace step: once the workspace exists, a
  // second-workspace run has nothing left to cancel, and leaving would strand
  // it half set up.
  const previous = order[order.indexOf(step) - 1];
  const onBack =
    previous && previous !== "workspace"
      ? () => {
          if (previous === "connect") setReturnedToConnect(true);
          setStep(previous);
        }
      : isNewWorkspace
        ? undefined
        : () => setStep("welcome");

  const finish = async (
    path: "full" | "runtime_skipped",
    destination?: OnboardingDestination,
  ) => {
    activateGettingStarted(workspace.id);
    await completeOnboarding(path, workspace.id);
    onComplete(workspace, destination);
  };

  const fail = (err: unknown) => {
    toast.error(err instanceof Error ? err.message : t(($) => $.errors.setup_failed));
  };

  const skip = async () => {
    try {
      await finish("runtime_skipped");
    } catch (err) {
      fail(err);
    }
  };

  const ensureAgent = () =>
    ensureToolAgent({
      workspace,
      runtime: runtime!,
      model: selection.model,
      thinkingLevel: selection.thinkingLevel,
      user,
      template: "onboarding",
    });

  const submitFirstTask = async (text: string, folder: FirstTaskFolder | null) => {
    try {
      if (!firstIssueRef.current) {
        const agent = await ensureAgent();
        const { issueId } = await createFirstTask({
          workspace,
          agent,
          text,
          folder,
          daemonId,
        });
        firstIssueRef.current = issueId;
      }
      await finish("full", { kind: "issue", issueId: firstIssueRef.current });
    } catch (err) {
      fail(err);
    }
  };

  const browse = async () => {
    try {
      await ensureAgent();
      await finish("full");
    } catch (err) {
      fail(err);
    }
  };

  const toFirstTask = useCallback(() => setStep("first_task"), [setStep]);
  const toTool = useCallback(() => setStep("tool"), [setStep]);

  return (
    <OnboardingPage progress={progress} onBack={onBack} showLogout={!isNewWorkspace}>
      {step === "connect" ? (
        <StepConnect
          runtimes={runtimes}
          returning={returnedToConnect}
          cliInstructions={cliInstructions}
          onNext={toTool}
          onSkip={skip}
        />
      ) : step === "first_task" && runtime ? (
        <StepFirstTask
          runtime={runtime}
          selection={selection}
          canPickFolder={!!daemonId && runtime.daemon_id === daemonId}
          onSubmit={submitFirstTask}
          onBrowse={browse}
        />
      ) : (
        <StepTool
          runtimes={runtimes}
          detecting={detecting}
          canRescan={canRescan}
          onRescan={rescan}
          workspaceSlug={workspace.slug}
          value={selection}
          onChange={setSelection}
          onContinue={toFirstTask}
          onSkip={skip}
        />
      )}
    </OnboardingPage>
  );
}

export type OnboardingMode = "first_run" | "new_workspace";

export type OnboardingDestination = { kind: "issue"; issueId: string };

export type { OnboardingStep };
