import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useExternalStoreSnapshot } from "@tutti-os/ui-react-hooks";
import {
  Button,
  CheckIcon,
  CopyIcon,
  RefreshIcon,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  cn
} from "@tutti-os/ui-system";
import { observer } from "mobx-react-lite";
import type { ReactNode } from "react";
import { useState } from "react";
import { useAgentHostApi } from "../../../agentActivityHost.tsx";
import type { AgentHostAgentTargetSetupSnapshot } from "../../../host/agentHostApi.ts";
import { useTranslation } from "../../../i18n/index.ts";
import { resolveAgentErrorPresentation } from "../../../shared/agentEnv/agentErrorPresentation.ts";
import {
  AgentSetupDialog,
  AgentSetupStepIcon,
  type AgentSetupStepStatus
} from "../../../shared/agentEnv/AgentSetupDialog.tsx";
import { useAgentTargetSetupController } from "../../../shared/agentEnv/agentTargetSetupController.tsx";
import { useAgentVisibleErrorPresentation } from "../../../shared/visibleError/AgentVisibleErrorPresentationContext.tsx";
import styles from "../AgentGUINode.styles.ts";
const TerminalLoginGuide = observer(TerminalLoginGuideRender);
const SetupTrackRow = observer(SetupTrackRowRender);

export interface AgentTargetSetupGateProps {
  children?: ReactNode;
  carouselMountedExternally: boolean;
  dialogOwner?: boolean;
  gateVisible?: boolean;
}

export const AgentTargetSetupGate = observer(function AgentTargetSetupGate({
  children,
  carouselMountedExternally,
  dialogOwner = false,
  gateVisible = true
}: AgentTargetSetupGateProps): React.JSX.Element {
  const handleInstallCallback = usePersistCallback<typeof handleInstall>(
    (...args) => handleInstall(...args)
  );

  const handleAuthenticateCallback = usePersistCallback<
    typeof handleAuthenticate
  >((...args) => handleAuthenticate(...args));

  const handleTerminalLoginStartCallback = usePersistCallback<
    typeof handleTerminalLoginStart
  >((...args) => handleTerminalLoginStart(...args));

  const controller = useAgentTargetSetupController();
  const { scope } = useAgentVisibleErrorPresentation();
  const { terminalLogin } = useAgentHostApi();
  const { t } = useTranslation();
  const state = useExternalStoreSnapshot(controller);
  const {
    agentTarget,
    agentTargetId,
    authenticatePending,
    dialogOpen,
    enabled,
    installPending,
    selectedAuthMethodId,
    setup,
    terminalLoginAvailable,
    terminalLoginError,
    terminalLoginPhase
  } = state;
  const { snapshot, loading, failed } = setup;
  const sourceAuthMethods = snapshot?.authMethods;
  const authMethods = useMemo(
    () => sourceAuthMethods ?? [],
    [sourceAuthMethods]
  );
  const account = snapshot?.account ?? null;
  const effectiveAuthMethodId = authMethods.some(
    (method) => method.id === selectedAuthMethodId
  )
    ? (selectedAuthMethodId ?? "")
    : authMethods.some((method) => method.id === account?.authMethodId)
      ? (account?.authMethodId ?? "")
      : (authMethods[0]?.id ?? "");
  const effectiveAuthMethod = authMethods.find(
    (method) => method.id === effectiveAuthMethodId
  );
  const terminalLoginCommand =
    effectiveAuthMethod?.type === "terminal"
      ? (effectiveAuthMethod.terminalCommand?.trim() ?? "") || null
      : null;
  const terminalStartupAction =
    effectiveAuthMethod?.type === "terminal"
      ? (effectiveAuthMethod.terminalStartupAction ?? null)
      : null;

  // A shared caller can observe the Owner target's readiness, but must never
  // be routed into this device's setup/login/install workflow. Shared hosts
  // own their separate availability presentation and execution errors.
  if (!enabled || scope === "shared_caller") {
    return <>{children}</>;
  }

  const setupChecking = !snapshot && !failed;
  const setupBlocked = failed || !snapshot || snapshot.status !== "ready";
  const handleInstall = async () => {
    const plan = snapshot?.plan;
    if (plan) await controller.install(plan.planDigest);
  };

  const handleAuthenticate = async () => {
    if (!effectiveAuthMethodId) return;
    await controller.authenticate(effectiveAuthMethodId);
  };

  const terminalLoginLaunchAvailable =
    terminalLoginAvailable &&
    Boolean(terminalLoginCommand) &&
    (!terminalStartupAction ||
      Boolean(
        terminalLogin?.supportedStartupActionTypes?.includes(
          terminalStartupAction.type
        )
      ));
  const handleTerminalLoginStart = async () => {
    if (!terminalLoginCommand) return;
    await controller.startTerminalLogin({
      command: terminalLoginCommand,
      startupAction: terminalStartupAction
    });
  };

  const handleTerminalLoginCancel = () => controller.cancelTerminalLogin();
  const actionRunning = isSetupActionRunning(snapshot?.action?.status);
  const actionFailed = isSetupActionFailed(snapshot?.action?.status);
  const installRetryAvailable =
    snapshot?.status === "failed" &&
    snapshot.action?.kind === "install" &&
    actionFailed &&
    Boolean(snapshot.plan);
  const phase = actionRunning ? (snapshot?.action?.phase ?? null) : null;
  const statusLabel = phase
    ? targetSetupPhaseLabel(t, phase)
    : loading
      ? t("agentHost.agentGui.targetSetupChecking")
      : null;
  const detectionStatus: AgentSetupStepStatus = failed
    ? "error"
    : loading || !snapshot
      ? "running"
      : snapshot
        ? "ok"
        : "pending";
  const installStatus = resolveInstallStepStatus(snapshot);
  const loginStatus = resolveLoginStepStatus(snapshot);
  const providerLabel = agentTarget?.label ?? agentTargetId;
  const setupFailurePresentation =
    snapshot?.status === "failed"
      ? resolveAgentErrorPresentation(snapshot.reason)
      : null;
  const setupFailureMessage = setupFailurePresentation?.messageKey
    ? t(setupFailurePresentation.messageKey, { provider: providerLabel })
    : null;
  const accountDetail = account
    ? [account.displayName, account.organization].filter(Boolean).join(" · ")
    : undefined;
  const setupDescription = setupChecking
    ? t("agentHost.agentGui.targetSetupChecking")
    : snapshot?.status === "auth_required"
      ? t("agentHost.agentGui.targetSetupAuthRequired")
      : snapshot?.status === "ready"
        ? t("agentHost.agentGui.targetSetupReady")
        : (setupFailureMessage ??
          t("agentHost.agentGui.targetSetupDescription"));
  const authenticationAvailable =
    snapshot?.status === "auth_required" || snapshot?.status === "ready";
  const signInApplicable =
    authMethods.length > 0 ||
    account !== null ||
    snapshot?.status === "auth_required" ||
    snapshot?.status === "authenticating";
  // Prefer setup projection over the local authenticate promise. OAuth can mark
  // the account ready before the host login call settles.
  const loginActionPending =
    authenticatePending && snapshot?.status !== "ready";

  return (
    <>
      {gateVisible && setupBlocked ? (
        <div className={styles.emptyHero}>
          <div
            className={cn(styles.emptyHeroBody, styles.emptyProviderGate)}
            data-testid="agent-target-setup-gate"
            role="status"
          >
            {carouselMountedExternally ? (
              <div
                aria-hidden="true"
                className={styles.emptyHeroCarouselPlaceholder}
              />
            ) : null}
            <h2 className={styles.emptyHeroTitle}>
              {t("agentHost.agentGui.targetSetupTitle", {
                provider: providerLabel
              })}
            </h2>
            <p className={styles.emptyProviderGateDescription}>
              {setupDescription}
            </p>
            {!setupChecking ? (
              <ButtonBinding1 controller={controller} t={t} />
            ) : null}
          </div>
        </div>
      ) : (
        children
      )}

      {dialogOwner ? (
        <AgentSetupDialog
          open={dialogOpen}
          onOpenChange={controller.setDialogOpen}
          title={t("agentHost.agentGui.targetSetupTitle", {
            provider: providerLabel
          })}
          description={setupDescription}
          footer={
            <ButtonBinding2 loading={loading} controller={controller} t={t} />
          }
        >
          <div className="flex flex-col gap-4">
            <p className="m-0 text-[13px] text-[var(--text-secondary)]">
              {statusLabel ??
                (snapshot?.status === "ready"
                  ? t("agentHost.agentGui.targetSetupComplete", {
                      provider: providerLabel
                    })
                  : t("agentHost.agentGui.targetSetupRemaining", {
                      provider: providerLabel
                    }))}
            </p>

            <ol className="m-0 flex list-none flex-col divide-y divide-[var(--border-1)] p-0">
              <SetupTrackRow
                label={t("agentHost.agentGui.targetSetupStage.detect")}
                status={detectionStatus}
                detail={
                  snapshot?.runtimeVersion
                    ? `${snapshot.runtimeVersion} · ${snapshot.runtimeSource ?? ""}`
                    : undefined
                }
              />

              <SetupTrackRow
                label={t("agentHost.agentGui.targetSetupStage.install")}
                status={installStatus}
                warning={snapshot?.status === "not_installed"}
                detail={
                  snapshot?.plan
                    ? `${snapshot.plan.packageName}@${snapshot.plan.packageVersion}`
                    : undefined
                }
                action={
                  (snapshot?.status === "not_installed" ||
                    installRetryAvailable) &&
                  snapshot.plan ? (
                    <ButtonBinding3
                      installPending={installPending}
                      handleInstall={handleInstallCallback}
                      t={t}
                      installRetryAvailable={installRetryAvailable}
                    />
                  ) : undefined
                }
              >
                {snapshot?.plan ? (
                  <span className="mt-2 block break-all text-[12px] text-[var(--text-secondary)]">
                    {snapshot.plan.installRoot}
                  </span>
                ) : null}
              </SetupTrackRow>

              {signInApplicable ? (
                <SetupTrackRow
                  label={t(
                    snapshot?.status === "ready"
                      ? "agentHost.agentGui.targetSetupLoggedInAccount"
                      : "agentHost.agentGui.targetSetupStage.login"
                  )}
                  status={loginStatus}
                  warning={snapshot?.status === "auth_required"}
                  detail={
                    snapshot?.status === "ready" ? accountDetail : undefined
                  }
                  action={
                    authenticationAvailable &&
                    authMethods.length > 0 &&
                    !terminalLoginCommand ? (
                      <ButtonBinding4
                        effectiveAuthMethodId={effectiveAuthMethodId}
                        loginActionPending={loginActionPending}
                        handleAuthenticate={handleAuthenticateCallback}
                        t={t}
                        snapshot={snapshot}
                      />
                    ) : terminalLoginCommand &&
                      terminalLoginLaunchAvailable &&
                      terminalLoginPhase !== "waiting" ? (
                      <ButtonBinding5
                        handleTerminalLoginStart={
                          handleTerminalLoginStartCallback
                        }
                        t={t}
                      />
                    ) : undefined
                  }
                >
                  {terminalLoginCommand ? (
                    <TerminalLoginGuide
                      command={terminalLoginCommand}
                      error={
                        terminalLoginError === "timed_out"
                          ? t(
                              "agentHost.agentGui.targetSetupTerminalLoginTimedOut"
                            )
                          : terminalLoginError === "unavailable"
                            ? t(
                                "agentHost.agentGui.targetSetupTerminalLoginUnavailable"
                              )
                            : null
                      }
                      onCancelLogin={
                        terminalLoginPhase === "waiting"
                          ? handleTerminalLoginCancel
                          : undefined
                      }
                      waiting={terminalLoginPhase === "waiting"}
                    />
                  ) : null}
                  {snapshot?.status === "auth_required" &&
                  authMethods.length > 0 ? (
                    <label className="mt-2 flex flex-col gap-1 text-[12px] text-[var(--text-secondary)]">
                      {t("agentHost.agentGui.targetSetupAuthMethod")}
                      <Select
                        value={effectiveAuthMethodId}
                        onValueChange={controller.selectAuthMethod}
                      >
                        <SelectTrigger
                          aria-label={t(
                            "agentHost.agentGui.targetSetupAuthMethod"
                          )}
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContentBinding6 authMethods={authMethods} />
                      </Select>
                    </label>
                  ) : snapshot?.status === "auth_required" ? (
                    <p className="mt-2 mb-0 text-[12px] text-[var(--text-secondary)]">
                      {t("agentHost.agentGui.targetSetupNoAuthMethods")}
                    </p>
                  ) : null}
                </SetupTrackRow>
              ) : null}
            </ol>

            {failed || snapshot?.status === "failed" || actionFailed ? (
              <p className="m-0 text-[12px] text-[var(--state-danger)]">
                <span className="block">
                  {snapshot?.action?.kind === "authenticate"
                    ? t("agentHost.agentGui.targetSetupAuthFailed")
                    : t("agentHost.agentGui.targetSetupFailed")}
                </span>
                {setupFailureMessage ? (
                  <span className="mt-1 block break-words text-[var(--text-secondary)]">
                    {setupFailureMessage}
                  </span>
                ) : snapshot?.action?.errorMessage?.trim() ? (
                  <span className="mt-1 block break-words text-[var(--text-secondary)]">
                    {snapshot.action.errorMessage.trim()}
                  </span>
                ) : null}
              </p>
            ) : null}
          </div>
        </AgentSetupDialog>
      ) : null}
    </>
  );
});

function TerminalLoginGuideRender({
  command,
  error,
  onCancelLogin,
  waiting = false
}: {
  command: string;
  error?: string | null;
  onCancelLogin?: () => void;
  waiting?: boolean;
}): React.JSX.Element {
  const { t } = useTranslation();
  const { clipboard } = useAgentHostApi();
  const [copiedCommand, setCopiedCommand] = useState<string | null>(null);
  const copied = copiedCommand === command;
  const handleCopy = async () => {
    try {
      await clipboard.writeText(command);
      setCopiedCommand(command);
    } catch (error) {
      // Clipboard unavailable; the command text remains selectable.
      console.warn("agent-gui: clipboard copy failed", error);
    }
  };
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void handleCopy());
  return (
    <div className="mt-2 flex flex-col gap-2">
      <p className="m-0 text-[12px] text-[var(--text-secondary)]">
        {waiting
          ? t("agentHost.agentGui.targetSetupTerminalLoginWaiting")
          : t("agentHost.agentGui.targetSetupTerminalAuthHint")}
      </p>
      <div className="flex items-start gap-2">
        <code className="min-w-0 flex-1 select-all break-all rounded-md border border-[var(--line-2)] bg-[var(--background-fronted)] px-3 py-2 font-mono text-[11px] leading-5 text-[var(--text-primary)]">
          {command}
        </code>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label={
            copied
              ? t("agentHost.agentGui.targetSetupCommandCopied")
              : t("agentHost.agentGui.targetSetupCopyCommand")
          }
          title={
            copied
              ? t("agentHost.agentGui.targetSetupCommandCopied")
              : t("agentHost.agentGui.targetSetupCopyCommand")
          }
          onClick={onClickProp1}
        >
          {copied ? (
            <CheckIcon aria-hidden="true" />
          ) : (
            <CopyIcon aria-hidden="true" />
          )}
        </Button>
      </div>
      {waiting && onCancelLogin ? (
        <div className="flex items-center gap-2">
          <Button type="button" size="sm" onClick={onCancelLogin}>
            {t("agentHost.agentGui.targetSetupTerminalLoginCancel")}
          </Button>
        </div>
      ) : null}
      {error ? (
        <p className="m-0 text-[12px] text-[var(--state-danger)]">{error}</p>
      ) : null}
    </div>
  );
}

function isSetupActionRunning(status: string | undefined): boolean {
  return status === "queued" || status === "running";
}

function isSetupActionFailed(status: string | undefined): boolean {
  return status === "failed" || status === "interrupted";
}

function SetupTrackRowRender({
  action,
  children,
  detail,
  label,
  status,
  warning = false
}: {
  action?: ReactNode;
  children?: ReactNode;
  detail?: string;
  label: string;
  status: AgentSetupStepStatus;
  warning?: boolean;
}): React.JSX.Element {
  return (
    <li
      data-status={status}
      className={`flex items-start gap-2.5 py-3 ${status === "pending" ? "opacity-50" : ""}`}
    >
      <span className="mt-0.5 shrink-0">
        <AgentSetupStepIcon status={status} warning={warning} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span
            className={`shrink-0 text-[13px] font-medium ${status === "error" ? (warning ? "text-[var(--state-warning)]" : "text-[var(--state-danger)]") : "text-[var(--text-primary)]"}`}
          >
            {label}
          </span>
          {detail ? (
            <span className="min-w-0 truncate text-[12px] text-[var(--text-secondary)]">
              {detail}
            </span>
          ) : null}
        </span>
        {children}
      </span>
      {action}
    </li>
  );
}

function resolveInstallStepStatus(
  snapshot: AgentHostAgentTargetSetupSnapshot | null
): AgentSetupStepStatus {
  if (!snapshot) return "pending";
  if (snapshot.status === "installing") return "running";
  if (snapshot.status === "not_installed") return "error";
  if (
    snapshot.status === "auth_required" ||
    snapshot.status === "authenticating" ||
    snapshot.status === "ready" ||
    snapshot.runtimeSource
  ) {
    return "ok";
  }
  return snapshot.status === "failed" ? "error" : "pending";
}

function resolveLoginStepStatus(
  snapshot: AgentHostAgentTargetSetupSnapshot | null
): AgentSetupStepStatus {
  if (!snapshot) return "pending";
  if (snapshot.status === "authenticating") return "running";
  if (snapshot.status === "auth_required") return "error";
  if (snapshot.status === "ready") return "ok";
  if (
    snapshot.status === "failed" &&
    snapshot.action?.kind === "authenticate"
  ) {
    return "error";
  }
  return "pending";
}

function targetSetupPhaseLabel(
  t: ReturnType<typeof useTranslation>["t"],
  phase: NonNullable<AgentHostAgentTargetSetupSnapshot["action"]>["phase"]
): string {
  switch (phase) {
    case "preparing":
      return t("agentHost.agentGui.targetSetupPhase.preparing");
    case "installing":
      return t("agentHost.agentGui.targetSetupPhase.installing");
    case "verifying":
      return t("agentHost.agentGui.targetSetupPhase.verifying");
    case "probing":
      return t("agentHost.agentGui.targetSetupPhase.probing");
    case "activating":
      return t("agentHost.agentGui.targetSetupPhase.activating");
    case "authenticating":
      return t("agentHost.agentGui.targetSetupPhase.authenticating");
    case "complete":
      return t("agentHost.agentGui.targetSetupPhase.complete");
  }
}

const ButtonBinding1 = observer(function ButtonBinding1({
  controller,
  t,
  ...slotProps
}: {
  controller: import("../../../shared/agentEnv/agentTargetSetupController").AgentTargetSetupController;
  t: import("../../../i18n/runtime").TranslateFn;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    controller: import("../../../shared/agentEnv/agentTargetSetupController").AgentTargetSetupController;
    t: import("../../../i18n/runtime").TranslateFn;
  }
>) {
  const onClickProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => controller.setDialogOpen(true));
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        className={styles.emptyProviderGateAction}
        onClick={onClickProp2}
      >
        {t("agentHost.agentGui.targetSetupOpen")}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  loading,
  controller,
  t,
  ...slotProps
}: {
  loading: boolean;
  controller: import("../../../shared/agentEnv/agentTargetSetupController").AgentTargetSetupController;
  t: import("../../../i18n/runtime").TranslateFn;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    loading: boolean;
    controller: import("../../../shared/agentEnv/agentTargetSetupController").AgentTargetSetupController;
    t: import("../../../i18n/runtime").TranslateFn;
  }
>) {
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void controller.refresh());
  return (
    <Slot.Root {...slotProps}>
      <Button
        size="dialog"
        variant="secondary"
        type="button"
        disabled={loading}
        onClick={onClickProp3}
      >
        <RefreshIcon className="size-4" />
        {t("agentHost.agentGui.targetSetupRetry")}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  installPending,
  handleInstall,
  t,
  installRetryAvailable,
  ...slotProps
}: {
  installPending: boolean;
  handleInstall: () => Promise<void>;
  t: import("../../../i18n/runtime").TranslateFn;
  installRetryAvailable: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    installPending: boolean;
    handleInstall: () => Promise<void>;
    t: import("../../../i18n/runtime").TranslateFn;
    installRetryAvailable: boolean;
  }
>) {
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void handleInstall());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        size="sm"
        disabled={installPending}
        onClick={onClickProp4}
      >
        {installPending
          ? t("agentHost.agentGui.targetSetupStarting")
          : t(
              installRetryAvailable
                ? "agentHost.agentGui.targetSetupReinstall"
                : "agentHost.agentGui.targetSetupInstall"
            )}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  effectiveAuthMethodId,
  loginActionPending,
  handleAuthenticate,
  t,
  snapshot,
  ...slotProps
}: ButtonBinding4Props &
  Omit<React.ComponentProps<typeof Slot.Root>, keyof ButtonBinding4Props>) {
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void handleAuthenticate());
  return (
    <Slot.Root {...slotProps}>
      <Button
        type="button"
        size="sm"
        disabled={!effectiveAuthMethodId || loginActionPending}
        onClick={onClickProp5}
      >
        {loginActionPending
          ? t("agentHost.agentGui.targetSetupAuthStarting")
          : snapshot?.status === "ready"
            ? t("agentHost.agentGui.targetSetupReauthenticate")
            : t("agentHost.agentGui.targetSetupAuthenticate")}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding5 = observer(function ButtonBinding5({
  handleTerminalLoginStart,
  t,
  ...slotProps
}: {
  handleTerminalLoginStart: () => Promise<void>;
  t: import("../../../i18n/runtime").TranslateFn;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    handleTerminalLoginStart: () => Promise<void>;
    t: import("../../../i18n/runtime").TranslateFn;
  }
>) {
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => void handleTerminalLoginStart());
  return (
    <Slot.Root {...slotProps}>
      <Button type="button" size="sm" onClick={onClickProp6}>
        {t("agentHost.agentGui.targetSetupTerminalLoginStart")}
      </Button>
    </Slot.Root>
  );
});

const SelectContentBinding6 = observer(function SelectContentBinding6({
  authMethods
}: {
  authMethods: import("../../../host/agentHostApi").AgentHostAgentTargetAuthMethod[];
}) {
  const styleProp7 = useMemo<
    NonNullable<React.ComponentProps<typeof SelectContent>["style"]>
  >(() => ({ zIndex: "var(--z-dialog-popover)" }), []);
  return (
    <SelectContent style={styleProp7}>
      {authMethods.map((method) => (
        <SelectItem key={method.id} value={method.id}>
          {method.name}
        </SelectItem>
      ))}
    </SelectContent>
  );
});

type ButtonBinding4Props = {
  effectiveAuthMethodId: string;
  loginActionPending: boolean;
  handleAuthenticate: () => Promise<void>;
  t: import("../../../i18n/runtime").TranslateFn;
  snapshot: AgentHostAgentTargetSetupSnapshot;
};
