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
  log?: (tag: string, ...args: unknown[]) => void;
  unresponsivePromptDelayMs?: number;
};

export function installRendererRecoveryHandlers(
  window: RendererRecoveryWindow,
  {
    isDev,
    showReloadPrompt,
    log = defaultDevLog,
    unresponsivePromptDelayMs = 1500,
  }: RendererRecoveryOptions,
) {
  let unresponsivePromptTimer: ReturnType<typeof setTimeout> | null = null;
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
    maybePromptReload({ kind: "render-process-gone", context: { details } });
  });

  window.webContents.on("preload-error", (_event, preloadPath, error) => {
    if (isDev) log("preload-error", `path=${preloadPath} err=${formatError(error)}`);
    maybePromptReload({
      kind: "preload-error",
      context: { preloadPath, error: formatError(error) },
    });
  });

  window.on("unresponsive", () => {
    if (isDev || unresponsivePromptTimer) return;
    unresponsivePromptTimer = setTimeout(() => {
      unresponsivePromptTimer = null;
      maybePromptReload({ kind: "unresponsive", context: {} });
    }, unresponsivePromptDelayMs);
  });

  window.on("responsive", () => {
    if (!unresponsivePromptTimer) return;
    clearTimeout(unresponsivePromptTimer);
    unresponsivePromptTimer = null;
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
      return "The desktop renderer process stopped responding or crashed.";
    case "preload-error":
      return "The desktop preload script failed before the app could start.";
    case "unresponsive":
      return "The desktop window is not responding.";
  }
}

function rendererRecoveryDetail(payload: ReloadPromptPayload) {
  return [
    "Reloading is the safest recovery path for this window.",
    "",
    `kind: ${payload.kind}`,
    `context: ${JSON.stringify(payload.context)}`,
  ].join("\n");
}

function defaultDevLog(tag: string, ...args: unknown[]) {
  process.stderr.write(`[renderer ${tag}] ${args.map(String).join(" ")}\n`);
}

function formatError(error: unknown) {
  return error instanceof Error ? (error.stack ?? error.message) : String(error);
}