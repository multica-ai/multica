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

const noopDevLog = () => undefined;

export function installRendererRecoveryHandlers(
  window: RendererRecoveryWindow,
  {
    isDev,
    showReloadPrompt,
    getDiagnosticContext,
    persistBreadcrumb,
    clearBreadcrumb,
    log = noopDevLog,
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
      reportHang();
    }, unresponsivePromptDelayMs);
  });

  const reportHang = () => {
    const payload: ReloadPromptPayload = {
      kind: "unresponsive",
      context: mergeDiagnosticContext({}),
    };
    persistBreadcrumb?.(payload);
    unresponsiveBreadcrumbWritten = true;
    maybePromptReload(payload);
  };

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
  locale = "en",
) {
  return async (payload: ReloadPromptPayload): Promise<ReloadPromptResult> => {
    const copy = rendererRecoveryCopy(locale);
    const result = await showMessageBox({
      type: "warning",
      buttons: [copy.reload, copy.dismiss],
      defaultId: 0,
      cancelId: 1,
      title: copy.title,
      message: copy.messages[payload.kind],
      detail: rendererRecoveryDetail(payload, copy),
    });
    return result.response === 0 ? "reload" : "dismiss";
  };
}

type RendererRecoveryCopy = {
  reload: string;
  dismiss: string;
  title: string;
  messages: Record<ReloadPromptPayload["kind"], string>;
  guidance: string[];
  macGuidance: string;
  diagnosticDetails: string;
};

function rendererRecoveryCopy(locale: string): RendererRecoveryCopy {
  const language = locale.toLowerCase();
  if (language.startsWith("zh")) {
    return {
      reload: "重新加载",
      dismiss: "关闭",
      title: "Multica 需要重新加载",
      messages: {
        "render-process-gone": "桌面窗口意外停止运行。",
        "preload-error": "桌面窗口无法完成启动。",
        unresponsive: "桌面窗口已卡住数秒。",
      },
      guidance: [
        "点击“重新加载”刷新此窗口并继续使用 Multica。",
        "如果问题反复出现，请告诉我们提示出现前你正在进行什么操作，以及重新加载后窗口是否恢复。",
      ],
      macGuidance:
        "若在 macOS 上报告问题，请附上活动监视器中 Multica Helper (Renderer) 进程的取样信息，以帮助我们定位阻塞原因。",
      diagnosticDetails: "诊断详情：",
    };
  }
  if (language.startsWith("ja")) {
    return {
      reload: "再読み込み",
      dismiss: "閉じる",
      title: "Multica の再読み込みが必要です",
      messages: {
        "render-process-gone": "デスクトップウィンドウが予期せず停止しました。",
        "preload-error": "デスクトップウィンドウの起動を完了できませんでした。",
        unresponsive: "デスクトップウィンドウが数秒間応答していません。",
      },
      guidance: [
        "「再読み込み」をクリックしてウィンドウを更新し、Multica の使用を続けてください。",
        "繰り返し発生する場合は、このメッセージが表示される直前の操作と、再読み込みで復旧したかをお知らせください。",
      ],
      macGuidance:
        "macOS で報告する場合は、アクティビティモニタで Multica Helper (Renderer) プロセスをサンプルすると、原因の特定に役立ちます。",
      diagnosticDetails: "診断情報：",
    };
  }
  if (language.startsWith("ko")) {
    return {
      reload: "새로고침",
      dismiss: "닫기",
      title: "Multica를 새로고침해야 합니다",
      messages: {
        "render-process-gone": "데스크톱 창이 예기치 않게 중지되었습니다.",
        "preload-error": "데스크톱 창을 시작하지 못했습니다.",
        unresponsive: "데스크톱 창이 몇 초 동안 응답하지 않았습니다.",
      },
      guidance: [
        "‘새로고침’을 눌러 이 창을 갱신하고 Multica를 계속 사용하세요.",
        "문제가 계속되면 이 메시지가 나타나기 직전에 수행한 작업과 새로고침 후 창이 복구되었는지 알려 주세요.",
      ],
      macGuidance:
        "macOS에서 문제를 보고할 때 활동 모니터의 Multica Helper (Renderer) 프로세스 샘플을 첨부하면 원인 파악에 도움이 됩니다.",
      diagnosticDetails: "진단 세부 정보:",
    };
  }
  return {
    reload: "Reload",
    dismiss: "Dismiss",
    title: "Multica needs to reload",
    messages: {
      "render-process-gone": "The desktop window stopped unexpectedly.",
      "preload-error": "The desktop window could not finish starting.",
      unresponsive: "The desktop window has been stuck for a few seconds.",
    },
    guidance: [
      "Click Reload to refresh this window and keep using Multica.",
      "If this keeps happening, please tell us what you were doing right before this message appeared and whether Reload recovered the window.",
    ],
    macGuidance:
      "For macOS reports, an Activity Monitor sample of the Multica Helper (Renderer) process helps us find what blocked the app.",
    diagnosticDetails: "Diagnostic details:",
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

function rendererRecoveryDetail(
  payload: ReloadPromptPayload,
  copy: RendererRecoveryCopy,
) {
  const guidance = [...copy.guidance];

  if (payload.kind === "unresponsive") {
    guidance.push(
      copy.macGuidance,
    );
  }

  return [
    ...guidance,
    "",
    copy.diagnosticDetails,
    `kind: ${payload.kind}`,
    `context: ${JSON.stringify(payload.context)}`,
  ].join("\n");
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
