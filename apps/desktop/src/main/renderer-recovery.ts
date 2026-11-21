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

import { type DialogLocale, resolveDialogLocale } from "./dialog-locale";

const RELOAD_PROMPT_COPY: Record<
  DialogLocale,
  {
    reload: string;
    dismiss: string;
    title: string;
    gone: string;
    preload: string;
    unresponsive: string;
    guidanceRefresh: string;
    guidanceReport: string;
    guidanceMacSample: string;
    diagnosticsHeading: string;
  }
> = {
  en: {
    reload: "Reload",
    dismiss: "Dismiss",
    title: "Multica needs to reload",
    gone: "The desktop window stopped unexpectedly.",
    preload: "The desktop window could not finish starting.",
    unresponsive: "The desktop window has been stuck for a few seconds.",
    guidanceRefresh: "Click Reload to refresh this window and keep using Multica.",
    guidanceReport:
      "If this keeps happening, please tell us what you were doing right before this message appeared and whether Reload recovered the window.",
    guidanceMacSample:
      "For macOS reports, an Activity Monitor sample of the Multica Helper (Renderer) process helps us find what blocked the app.",
    diagnosticsHeading: "Diagnostic details:",
  },
  "zh-Hans": {
    reload: "重新加载",
    dismiss: "关闭",
    title: "Multica 需要重新加载",
    gone: "桌面窗口意外停止。",
    preload: "桌面窗口未能完成启动。",
    unresponsive: "桌面窗口已无响应数秒。",
    guidanceRefresh: "点击「重新加载」刷新此窗口以继续使用 Multica。",
    guidanceReport:
      "如果此问题反复出现，请告诉我们出现前你正在进行的操作，以及重新加载是否恢复了窗口。",
    guidanceMacSample:
      "在 macOS 上反馈时，附上活动监视器中 Multica Helper (Renderer) 进程的采样有助于排查卡顿原因。",
    diagnosticsHeading: "诊断详情：",
  },
  "zh-Hant": {
    reload: "重新載入",
    dismiss: "關閉",
    title: "Multica 需要重新載入",
    gone: "桌面視窗意外停止。",
    preload: "桌面視窗未能完成啟動。",
    unresponsive: "桌面視窗已無回應數秒。",
    guidanceRefresh: "點擊「重新載入」重新整理此視窗以繼續使用 Multica。",
    guidanceReport:
      "若問題重複發生，請告訴我們發生前你正在執行的操作，以及重新載入是否恢復了視窗。",
    guidanceMacSample:
      "在 macOS 回報時，附上活動監視器中 Multica Helper (Renderer) 程序的取樣有助於排查原因。",
    diagnosticsHeading: "診斷詳情：",
  },
  ja: {
    reload: "再読み込み",
    dismiss: "閉じる",
    title: "Multica の再読み込みが必要です",
    gone: "デスクトップウィンドウが予期せず停止しました。",
    preload: "デスクトップウィンドウの起動を完了できませんでした。",
    unresponsive: "デスクトップウィンドウが数秒間応答していません。",
    guidanceRefresh: "「再読み込み」をクリックしてウィンドウを更新し、Multica を続けてご利用ください。",
    guidanceReport:
      "この問題が繰り返し発生する場合は、直前に行っていた操作と再読み込みで回復したかをお知らせください。",
    guidanceMacSample:
      "macOS の場合、アクティビティモニタの Multica Helper (Renderer) プロセスのサンプルがあると原因調査に役立ちます。",
    diagnosticsHeading: "診断の詳細:",
  },
  ko: {
    reload: "다시 불러오기",
    dismiss: "닫기",
    title: "Multica를 다시 불러와야 합니다",
    gone: "데스크톱 창이 예기치 않게 중지되었습니다.",
    preload: "데스크톱 창 시작을 완료하지 못했습니다.",
    unresponsive: "데스크톱 창이 몇 초 동안 응답하지 않습니다.",
    guidanceRefresh: "다시 불러오기를 클릭해 이 창을 새로고침하고 Multica를 계속 사용하세요.",
    guidanceReport:
      "문제가 계속 발생하면 직전에 하던 작업과 다시 불러오기로 복구되었는지 알려 주세요.",
    guidanceMacSample:
      "macOS에서는 활성 상태 보기의 Multica Helper (Renderer) 프로세스 샘플을 함께 보내 주시면 원인 파악에 도움이 됩니다.",
    diagnosticsHeading: "진단 세부 정보:",
  },
  fr: {
    reload: "Recharger",
    dismiss: "Fermer",
    title: "Multica doit se recharger",
    gone: "La fenêtre s'est arrêtée de manière inattendue.",
    preload: "La fenêtre n'a pas pu terminer son démarrage.",
    unresponsive: "La fenêtre ne répond plus depuis quelques secondes.",
    guidanceRefresh: "Cliquez sur Recharger pour actualiser cette fenêtre et continuer à utiliser Multica.",
    guidanceReport:
      "Si le problème persiste, indiquez-nous ce que vous faisiez juste avant et si le rechargement a résolu le problème.",
    guidanceMacSample:
      "Sur macOS, un échantillon du processus Multica Helper (Renderer) depuis le Moniteur d'activité nous aide à identifier le blocage.",
    diagnosticsHeading: "Détails de diagnostic :",
  },
  es: {
    reload: "Recargar",
    dismiss: "Cerrar",
    title: "Multica necesita recargarse",
    gone: "La ventana se detuvo inesperadamente.",
    preload: "La ventana no pudo terminar de iniciarse.",
    unresponsive: "La ventana lleva unos segundos sin responder.",
    guidanceRefresh: "Haz clic en Recargar para actualizar esta ventana y seguir usando Multica.",
    guidanceReport:
      "Si esto sigue ocurriendo, indícanos qué estabas haciendo justo antes y si recargar recuperó la ventana.",
    guidanceMacSample:
      "En macOS, una muestra del proceso Multica Helper (Renderer) desde el Monitor de Actividad nos ayuda a encontrar la causa.",
    diagnosticsHeading: "Detalles de diagnóstico:",
  },
  ru: {
    reload: "Перезагрузить",
    dismiss: "Закрыть",
    title: "Multica требуется перезагрузка",
    gone: "Окно приложения неожиданно остановилось.",
    preload: "Не удалось завершить запуск окна приложения.",
    unresponsive: "Окно приложения не отвечает уже несколько секунд.",
    guidanceRefresh: "Нажмите «Перезагрузить», чтобы обновить окно и продолжить работу в Multica.",
    guidanceReport:
      "Если проблема повторяется, сообщите, что вы делали перед её появлением и помогла ли перезагрузка.",
    guidanceMacSample:
      "В macOS снимок процесса Multica Helper (Renderer) из «Мониторинга системы» поможет нам найти причину зависания.",
    diagnosticsHeading: "Диагностические сведения:",
  },
};

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
  getPreferredLanguage: () => string = () => "en",
) {
  return async (payload: ReloadPromptPayload): Promise<ReloadPromptResult> => {
    const copy = RELOAD_PROMPT_COPY[resolveDialogLocale(getPreferredLanguage())];
    const result = await showMessageBox({
      type: "warning",
      buttons: [copy.reload, copy.dismiss],
      defaultId: 0,
      cancelId: 1,
      title: copy.title,
      message: rendererRecoveryMessage(payload.kind, copy),
      detail: rendererRecoveryDetail(payload, copy),
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

function rendererRecoveryMessage(
  kind: ReloadPromptPayload["kind"],
  copy: (typeof RELOAD_PROMPT_COPY)[DialogLocale],
) {
  switch (kind) {
    case "render-process-gone":
      return copy.gone;
    case "preload-error":
      return copy.preload;
    case "unresponsive":
      return copy.unresponsive;
  }
}

function rendererRecoveryDetail(
  payload: ReloadPromptPayload,
  copy: (typeof RELOAD_PROMPT_COPY)[DialogLocale],
) {
  const guidance = [copy.guidanceRefresh, copy.guidanceReport];

  if (payload.kind === "unresponsive") {
    guidance.push(copy.guidanceMacSample);
  }

  return [
    ...guidance,
    "",
    copy.diagnosticsHeading,
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
