import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  type ReactNode
} from "react";
import { toast } from "@tutti-os/ui-system";
import { useOptionalAgentHostApi } from "../../agentActivityHost.tsx";
import type {
  AgentHostAgentTargetSetupState,
  AgentHostAgentTargetSetupWatch,
  AgentHostTerminalLoginApi,
  AgentHostTerminalLoginHandle,
  AgentHostTerminalStartupAction,
  AgentHostToastApi
} from "../../host/agentHostApi.ts";
import { useTranslation } from "../../i18n/index.ts";
import type { AgentGUIAgentTarget } from "../../types.ts";
import {
  createAgentTargetSetupFailureNotificationController,
  type AgentTargetSetupFailureNotification
} from "./agentTargetSetupNotificationController.ts";
import { resolveAgentErrorPresentation } from "./agentErrorPresentation.ts";
import { observer } from "mobx-react-lite";
export const AgentTargetSetupControllerProvider = observer(
  AgentTargetSetupControllerProviderRender
);

const DISABLED_SETUP_STATE: AgentHostAgentTargetSetupState = {
  snapshot: null,
  loading: false,
  failed: false
};

export interface AgentTargetSetupControllerState {
  agentTarget: AgentGUIAgentTarget | null;
  agentTargetId: string;
  authenticatePending: boolean;
  dialogOpen: boolean;
  enabled: boolean;
  installPending: boolean;
  selectedAuthMethodId: string | null;
  setup: AgentHostAgentTargetSetupState;
  terminalLoginAvailable: boolean;
  terminalLoginError: "timed_out" | "unavailable" | null;
  terminalLoginPhase: "error" | "idle" | "waiting";
}

export interface AgentTargetSetupController {
  authenticate(methodId: string): Promise<void>;
  cancelTerminalLogin(): void;
  getSnapshot(): AgentTargetSetupControllerState;
  install(planDigest: string): Promise<void>;
  refresh(): Promise<void>;
  selectAuthMethod(methodId: string): void;
  setDialogOpen(open: boolean): void;
  startTerminalLogin(input: {
    command: string;
    startupAction?: AgentHostTerminalStartupAction | null;
  }): Promise<void>;
  subscribe(listener: () => void): () => void;
}

const AgentTargetSetupControllerContext =
  createContext<AgentTargetSetupController | null>(null);

function AgentTargetSetupControllerProviderRender({
  children,
  controller
}: {
  children: ReactNode;
  controller: AgentTargetSetupController;
}): React.JSX.Element {
  return (
    <AgentTargetSetupControllerContext.Provider value={controller}>
      {children}
    </AgentTargetSetupControllerContext.Provider>
  );
}

export function useAgentTargetSetupController(): AgentTargetSetupController {
  const controller = useContext(AgentTargetSetupControllerContext);
  if (!controller) {
    throw new Error("AgentTargetSetupControllerProvider is missing.");
  }
  return controller;
}

export function useCreateAgentTargetSetupController(
  agentTarget: AgentGUIAgentTarget | null
): AgentTargetSetupController {
  const hostApi = useOptionalAgentHostApi();
  const { t } = useTranslation();
  const agentTargetId =
    agentTarget?.agentTargetId?.trim() || agentTarget?.targetId.trim() || "";
  const enabled =
    agentTarget?.ref.setupKind === "target_runtime" &&
    Boolean(hostApi?.agentTargetSetup && agentTargetId);
  const stableAgentTarget = useMemo(
    () => agentTarget,
    [
      agentTargetId,
      agentTarget?.label,
      agentTarget?.provider,
      agentTarget?.ref.setupKind
    ]
  );
  const watch = useMemo(
    () =>
      enabled
        ? (hostApi?.agentTargetSetup?.watch({ agentTargetId }) ?? null)
        : null,
    [agentTargetId, enabled, hostApi?.agentTargetSetup]
  );
  const showNotification = useCallback(
    (notification: AgentTargetSetupFailureNotification) =>
      showTargetSetupFailureNotification({
        hostToast: hostApi?.toast,
        notification,
        providerLabel: agentTarget?.label ?? agentTargetId,
        t
      }),
    [agentTarget?.label, agentTargetId, hostApi?.toast, t]
  );
  const logCommandError = useCallback(
    (command: "authenticate" | "install", error: unknown) => {
      hostApi?.debug?.logRuntimeDiagnostics({
        agentTargetId,
        error: error instanceof Error ? error.message : String(error),
        event:
          command === "install"
            ? "agent-target-runtime-install-failed"
            : "agent-target-runtime-authentication-failed"
      });
    },
    [agentTargetId, hostApi?.debug]
  );
  const controllerRef = useRef<{
    controller: AgentTargetSetupController;
    targetKey: string;
  } | null>(null);
  const targetKey = enabled ? agentTargetId : "";
  const controllerEntry = useMemo(() => {
    const previousState =
      controllerRef.current?.targetKey === targetKey
        ? controllerRef.current.controller.getSnapshot()
        : null;
    return {
      controller: createAgentTargetSetupController({
        agentTarget: stableAgentTarget,
        agentTargetId,
        enabled,
        initialDialogOpen: previousState?.dialogOpen ?? false,
        initialSelectedAuthMethodId:
          previousState?.selectedAuthMethodId ?? null,
        logCommandError,
        onNotification: showNotification,
        terminalLogin: hostApi?.terminalLogin,
        watch
      }),
      targetKey
    };
  }, [
    agentTargetId,
    enabled,
    logCommandError,
    showNotification,
    stableAgentTarget,
    targetKey,
    hostApi?.terminalLogin,
    watch
  ]);
  controllerRef.current = controllerEntry;
  return controllerEntry.controller;
}

function createAgentTargetSetupController(
  input: createAgentTargetSetupControllerProps
): AgentTargetSetupController {
  const listeners = new Set<() => void>();
  const initialSetup = input.watch?.getSnapshot() ?? DISABLED_SETUP_STATE;
  const notifications =
    createAgentTargetSetupFailureNotificationController(initialSetup);
  let unsubscribe: (() => void) | null = null;
  let disposeGeneration = 0;
  let terminalLoginGeneration = 0;
  let terminalLoginHandle: AgentHostTerminalLoginHandle | null = null;
  let state: AgentTargetSetupControllerState = {
    agentTarget: input.agentTarget,
    agentTargetId: input.agentTargetId,
    authenticatePending: false,
    dialogOpen: input.initialDialogOpen,
    enabled: input.enabled,
    installPending: false,
    selectedAuthMethodId: input.initialSelectedAuthMethodId,
    setup: initialSetup,
    terminalLoginAvailable: Boolean(input.terminalLogin),
    terminalLoginError: null,
    terminalLoginPhase: "idle"
  };
  const update = (patch: Partial<AgentTargetSetupControllerState>) => {
    state = { ...state, ...patch };
    for (const listener of listeners) listener();
  };
  const runCommand = async (
    command: "authenticate" | "install",
    operation: () => Promise<void>
  ) => {
    const pendingKey =
      command === "install" ? "installPending" : "authenticatePending";
    update({ [pendingKey]: true });
    try {
      await operation();
    } catch (error) {
      input.logCommandError(command, error);
    } finally {
      update({ [pendingKey]: false });
    }
  };
  const closeTerminalLoginHandle = () => {
    const handle = terminalLoginHandle;
    terminalLoginHandle = null;
    try {
      handle?.close();
    } catch (error) {
      console.warn("agent-gui: terminal login close failed", error);
    }
  };
  const settleTerminalLogin = (
    generation: number,
    result: "ready" | "timed_out" | "unavailable"
  ) => {
    if (generation !== terminalLoginGeneration) return;
    terminalLoginGeneration += 1;
    closeTerminalLoginHandle();
    update({
      terminalLoginError:
        result === "ready"
          ? null
          : result === "timed_out"
            ? "timed_out"
            : "unavailable",
      terminalLoginPhase: result === "ready" ? "idle" : "error"
    });
  };
  const cancelTerminalLogin = (publish: boolean) => {
    terminalLoginGeneration += 1;
    closeTerminalLoginHandle();
    if (publish) {
      update({
        terminalLoginError: null,
        terminalLoginPhase: "idle"
      });
    }
  };
  const startTerminalLogin = async (request: {
    command: string;
    startupAction?: AgentHostTerminalStartupAction | null;
  }) => {
    const normalizedCommand = request.command.trim();
    if (!input.terminalLogin || !normalizedCommand) return;
    if (
      request.startupAction &&
      !input.terminalLogin.supportedStartupActionTypes?.includes(
        request.startupAction.type
      )
    ) {
      update({
        terminalLoginError: "unavailable",
        terminalLoginPhase: "error"
      });
      return;
    }
    cancelTerminalLogin(false);
    const generation = terminalLoginGeneration;
    update({
      terminalLoginError: null,
      terminalLoginPhase: "waiting"
    });
    let handle: AgentHostTerminalLoginHandle | void;
    try {
      handle = await input.terminalLogin.run({
        agentTargetId: input.agentTargetId,
        command: normalizedCommand,
        ...(request.startupAction
          ? { startupAction: request.startupAction }
          : {})
      });
    } catch {
      settleTerminalLogin(generation, "unavailable");
      return;
    }
    if (!handle) {
      settleTerminalLogin(generation, "unavailable");
      return;
    }
    if (generation !== terminalLoginGeneration) {
      try {
        handle.close();
      } catch (error) {
        console.warn("agent-gui: terminal login close failed", error);
      }
      return;
    }
    terminalLoginHandle = handle;
    update({ dialogOpen: false });
    void handle.completion.then(
      (result) => settleTerminalLogin(generation, result),
      () => settleTerminalLogin(generation, "unavailable")
    );
  };
  return {
    authenticate: (methodId) =>
      runCommand(
        "authenticate",
        () =>
          input.watch?.authenticate({
            methodId,
            clientActionId: createClientActionId()
          }) ?? Promise.resolve()
      ),
    cancelTerminalLogin: () => cancelTerminalLogin(true),
    getSnapshot: () => state,
    install: (planDigest) =>
      runCommand(
        "install",
        () =>
          input.watch?.install({
            planDigest,
            clientActionId: createClientActionId()
          }) ?? Promise.resolve()
      ),
    refresh: () => input.watch?.refresh() ?? Promise.resolve(),
    selectAuthMethod: (selectedAuthMethodId) =>
      update({ selectedAuthMethodId }),
    setDialogOpen: (dialogOpen) => update({ dialogOpen }),
    startTerminalLogin,
    subscribe(listener) {
      listeners.add(listener);
      disposeGeneration += 1;
      if (!unsubscribe && input.watch) {
        unsubscribe = input.watch.subscribe((setup) => {
          const notification = notifications.observe(setup);
          if (notification) input.onNotification(notification);
          const ready = setup.snapshot?.status === "ready";
          // Setup projection is the source of truth for signed-in. Clear local
          // in-flight flags as soon as ready arrives so CTAs cannot keep saying
          // "opening login" after the checklist already shows a signed-in account.
          if (ready && state.terminalLoginPhase === "waiting") {
            terminalLoginGeneration += 1;
            closeTerminalLoginHandle();
            update({
              setup,
              authenticatePending: false,
              terminalLoginError: null,
              terminalLoginPhase: "idle"
            });
            return;
          }
          if (ready && state.authenticatePending) {
            update({ setup, authenticatePending: false });
            return;
          }
          update({ setup });
        });
      }
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          const scheduledGeneration = ++disposeGeneration;
          queueMicrotask(() => {
            if (
              listeners.size === 0 &&
              disposeGeneration === scheduledGeneration
            ) {
              unsubscribe?.();
              unsubscribe = null;
            }
          });
        }
      };
    }
  };
}

function showTargetSetupFailureNotification(input: {
  hostToast: AgentHostToastApi | undefined;
  notification: AgentTargetSetupFailureNotification;
  providerLabel: string;
  t: ReturnType<typeof useTranslation>["t"];
}): void {
  const title =
    input.notification.actionKind === "authenticate"
      ? input.t("agentHost.agentGui.targetSetupAuthFailed")
      : input.t("agentHost.agentGui.targetSetupFailed");
  const presentation = resolveAgentErrorPresentation(
    input.notification.errorCode
  );
  const description = presentation?.messageKey
    ? input.t(presentation.messageKey, { provider: input.providerLabel })
    : input.notification.errorMessage;
  if (input.hostToast?.error) {
    input.hostToast.error(title, description);
    return;
  }
  toast.error(title, {
    description,
    id: `agent-target-setup-${input.notification.actionId}`
  });
}

function createClientActionId(): string {
  return (
    globalThis.crypto?.randomUUID?.() ??
    `agent-setup-${Date.now()}-${Math.random().toString(36).slice(2)}`
  );
}

type createAgentTargetSetupControllerProps = {
  agentTarget: AgentGUIAgentTarget | null;
  agentTargetId: string;
  enabled: boolean;
  initialDialogOpen: boolean;
  initialSelectedAuthMethodId: string | null;
  logCommandError: (
    command: "authenticate" | "install",
    error: unknown
  ) => void;
  onNotification: (notification: AgentTargetSetupFailureNotification) => void;
  terminalLogin: AgentHostTerminalLoginApi | undefined;
  watch: AgentHostAgentTargetSetupWatch | null;
};
