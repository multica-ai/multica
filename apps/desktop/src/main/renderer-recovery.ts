export type RendererRecoveryWindow = {
  isDestroyed: () => boolean;
  on: (event: "unresponsive" | "responsive", handler: () => void) => unknown;
  webContents: {
    on: (event: string, handler: (...args: any[]) => void) => unknown;
    reload: () => void;
  };
};

type ReloadPromptPayload = {
  kind: "render-process-gone" | "preload-error" | "unresponsive";
  context: Record<string, unknown>;
};

type ReloadPromptResult = "reload" | "dismiss";

type RendererRecoveryOptions = {
  isDev: boolean;
  showReloadPrompt: (payload: ReloadPromptPayload) => Promise<ReloadPromptResult>;
  getDiagnosticContext?: () => Record<string, unknown>;
  /**
   * Persist a freeze/crash breadcrumb to disk. The renderer can't report a
   * true hang or process death itself (blocked / gone), so the main process
   * writes it here and the next renderer boot flushes it to telemetry. Omit
   * in dev to keep field telemetry clean.
   */
  persistBreadcrumb?: (payload: ReloadPromptPayload) => void;
  /**
   * Delete a previously-persisted unresponsive breadcrumb. Called when the
   * renderer recovers (`responsive` after `unresponsive`): the window came
   * back, so the in-thread watchdog reports the freeze and the breadcrumb
   * would only double-count it. Crash breadcrumbs are never cleared — a dead
   * process never recovers.
   */
  clearBreadcrumb?: () => void;
  log?: (tag: string, ...args: unknown[]) => void;
  unresponsivePromptDelayMs?: number;
};

export function installRendererRecoveryHandlers(
  window: RendererRecoveryWindow,
  {
    isDev,
    showReloadPrompt,
    getDiagnosticContext,
    persistBreadcrumb,
    clearBreadcrumb,
    log = defaultDevLog,
    unresponsivePromptDelayMs = 1500,
  }: RendererRecoveryOptions,
) {
  let unresponsivePromptTimer: ReturnType<typeof setTimeout> | null = null;
  // True once a breadcrumb has been written for the current hang. A later
  // `responsive` clears it; only a hang that never returns survives to report.
  let unresponsiveBreadcrumbWritten = false;
  const mergeDiagnosticContext = (context: Record<string, unknown>) => ({
    ...readDiagnosticContext(getDiagnosticContext),
    ...context,
  });
  const maybePromptReload = (payload: ReloadPromptPayload) => {
    if (isDev) return;
    void showReloadPrompt(payload).then((result) => {
      if (result === "reload" && !window.isDestroyed()) {
        window.webContents.reload();
      }
    });
  };

  window.webContents.on("render-process-gone", (_event, details) => {
    if (isDev) log("process-gone", JSON.stringify(details));
    if (!isRecoverableRendererExit(details)) return;
    const payload: ReloadPromptPayload = {
      kind: "render-process-gone",
      context: mergeDiagnosticContext({ details }),
    };
    persistBreadcrumb?.(payload);
    maybePromptReload(payload);
  });

  // preload-error intentionally does NOT persist a breadcrumb: it's a startup
  // failure of the preload script itself, and the breadcrumb-flush path depends
  // on that same preload exposing `getLastFreeze` — if preload is broken, the
  // next boot couldn't read it back anyway. We only prompt for reload here.
  window.webContents.on("preload-error", (_event, preloadPath, error) => {
    if (isDev) log("preload-error", `path=${preloadPath} err=${formatError(error)}`);
    maybePromptReload({
      kind: "preload-error",
      context: mergeDiagnosticContext({ preloadPath, error: formatError(error) }),
    });
  });

  window.on("unresponsive", () => {
    if (isDev || unresponsivePromptTimer) return;
    unresponsivePromptTimer = setTimeout(() => {
      unresponsivePromptTimer = null;
      const payload: ReloadPromptPayload = {
        kind: "unresponsive",
        context: mergeDiagnosticContext({}),
      };
      persistBreadcrumb?.(payload);
      unresponsiveBreadcrumbWritten = true;
      maybePromptReload(payload);
    }, unresponsivePromptDelayMs);
  });

  window.on("responsive", () => {
    if (unresponsivePromptTimer) {
      clearTimeout(unresponsivePromptTimer);
      unresponsivePromptTimer = null;
    }
    // The window came back: drop any breadcrumb written during this hang so it
    // isn't re-reported (and mislabeled `recovered: false`) on next boot.
    if (unresponsiveBreadcrumbWritten) {
      clearBreadcrumb?.();
      unresponsiveBreadcrumbWritten = false;
    }
  });
}

export function createElectronReloadPrompt(
  showMessageBox: (options: {
    type: "warning";
    buttons: string[];
    defaultId: number;
    cancelId: number;
    title: string;
    message: string;
    detail: string;
  }) => Promise<{ response: number }>,
) {
  return async (payload: ReloadPromptPayload): Promise<ReloadPromptResult> => {
    const result = await showMessageBox({
      type: "warning",
      buttons: ["Reload", "Dismiss"],
      defaultId: 0,
      cancelId: 1,
      title: "Multica needs to reload",
      message: rendererRecoveryMessage(payload.kind),
      detail: rendererRecoveryDetail(payload),
    });
    return result.response === 0 ? "reload" : "dismiss";
  };
}

function isRecoverableRendererExit(details: unknown) {
  if (!details || typeof details !== "object") return false;
  const reason = (details as { reason?: unknown }).reason;
  return (
    reason === "crashed" ||
    reason === "oom" ||
    reason === "abnormal-exit" ||
    reason === "launch-failed" ||
    reason === "integrity-failure"
  );
}

function rendererRecoveryMessage(kind: ReloadPromptPayload["kind"]) {
  switch (kind) {
    case "render-process-gone":
      return "The desktop window stopped unexpectedly.";
    case "preload-error":
      return "The desktop window could not finish starting.";
    case "unresponsive":
      return "The desktop window has been stuck for a few seconds.";
  }
}

function rendererRecoveryDetail(payload: ReloadPromptPayload) {
  const guidance = [
    "Click Reload to refresh this window and keep using Multica.",
    "If this keeps happening, please tell us what you were doing right before this message appeared and whether Reload recovered the window.",
  ];

  if (payload.kind === "unresponsive") {
    guidance.push(
      "For macOS reports, an Activity Monitor sample of the Multica Helper (Renderer) process helps us find what blocked the app.",
    );
  }

  return [
    ...guidance,
    "",
    "Diagnostic details:",
    `kind: ${payload.kind}`,
    `context: ${JSON.stringify(payload.context)}`,
  ].join("\n");
}

function defaultDevLog(tag: string, ...args: unknown[]) {
  process.stderr.write(`[renderer ${tag}] ${args.map(String).join(" ")}\n`);
}

function readDiagnosticContext(
  getDiagnosticContext: (() => Record<string, unknown>) | undefined,
) {
  if (!getDiagnosticContext) return {};
  try {
    return getDiagnosticContext();
  } catch {
    return {};
  }
}

function formatError(error: unknown) {
  return error instanceof Error ? (error.stack ?? error.message) : String(error);
}
