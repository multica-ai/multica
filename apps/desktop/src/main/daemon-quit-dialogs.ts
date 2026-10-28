import type { MessageBoxOptions } from "electron";

interface QuitLabels {
  title: string;
  activeRuns: (count: number) => string;
  unknownRuns: string;
  interruptRuns: string;
  interruptUnknownRuns: string;
  cancel: string;
  stopAndQuit: string;
  failureTitle: string;
  daemonStopped: string;
  shutdownUnknown: string;
  cancelAfterStop: string;
  cancelUnconfirmed: string;
  cancelQuit: string;
  ignoreAndQuit: string;
}

const labels: Record<string, QuitLabels> = {
  en: {
    title: "Quit Multica",
    activeRuns: (count) => `${count} ${count === 1 ? "run is" : "runs are"} active on this device.`,
    unknownRuns: "Could not confirm whether any runs are active on this device.",
    interruptRuns: "Stopping the daemon now will interrupt active runs.",
    interruptUnknownRuns: "Stopping the daemon may interrupt runs on this device.",
    cancel: "Cancel",
    stopAndQuit: "Stop now and quit",
    failureTitle: "Could not confirm shutdown",
    daemonStopped: "The daemon has stopped, but its runs could not be confirmed to have exited.",
    shutdownUnknown: "Could not confirm whether the daemon and its runs have stopped.",
    cancelAfterStop: "If you cancel, Multica stays open and the daemon remains stopped. Background runs may continue.",
    cancelUnconfirmed: "If you cancel, Multica stays open with automatic daemon restart disabled. Background runs may continue.",
    cancelQuit: "Cancel quit",
    ignoreAndQuit: "Ignore and quit",
  },
  "zh-Hans": {
    title: "退出 Multica",
    activeRuns: (count) => `此设备上有 ${count} 次运行尚未结束。`,
    unknownRuns: "无法确认此设备上是否有运行正在进行。",
    interruptRuns: "立即停止守护进程会中断这些运行。",
    interruptUnknownRuns: "停止守护进程可能中断此设备上的运行。",
    cancel: "取消",
    stopAndQuit: "立即停止并退出",
    failureTitle: "无法确认停止完成",
    daemonStopped: "守护进程已停止，但无法确认其运行进程是否已全部退出。",
    shutdownUnknown: "无法确认守护进程及其运行是否已全部停止。",
    cancelAfterStop: "取消后 Multica 会保持打开，守护进程保持停止。后台运行可能继续执行。",
    cancelUnconfirmed: "取消后 Multica 会保持打开，守护进程不会自动重启。后台运行可能继续执行。",
    cancelQuit: "取消退出",
    ignoreAndQuit: "忽略并退出",
  },
  "zh-Hant": {
    title: "結束 Multica",
    activeRuns: (count) => `此裝置上有 ${count} 次執行尚未結束。`,
    unknownRuns: "無法確認此裝置上是否有執行正在進行。",
    interruptRuns: "立即停止守護進程會中斷這些執行。",
    interruptUnknownRuns: "停止守護進程可能中斷此裝置上的執行。",
    cancel: "取消",
    stopAndQuit: "立即停止並結束",
    failureTitle: "無法確認停止完成",
    daemonStopped: "守護進程已停止，但無法確認其執行程序是否已全部結束。",
    shutdownUnknown: "無法確認守護進程及其執行是否已全部停止。",
    cancelAfterStop: "取消後 Multica 會保持開啟，守護進程保持停止。背景執行可能繼續進行。",
    cancelUnconfirmed: "取消後 Multica 會保持開啟，守護進程不會自動重新啟動。背景執行可能繼續進行。",
    cancelQuit: "取消結束",
    ignoreAndQuit: "忽略並結束",
  },
  ja: {
    title: "Multica を終了",
    activeRuns: (count) => `このデバイスで ${count} 件の実行が進行中です。`,
    unknownRuns: "このデバイスで実行が進行中か確認できませんでした。",
    interruptRuns: "今すぐデーモンを停止すると、これらの実行は中断されます。",
    interruptUnknownRuns: "デーモンを停止すると、このデバイスでの実行が中断される可能性があります。",
    cancel: "キャンセル",
    stopAndQuit: "今すぐ停止して終了",
    failureTitle: "停止を確認できませんでした",
    daemonStopped: "デーモンは停止しましたが、実行プロセスがすべて終了したか確認できませんでした。",
    shutdownUnknown: "デーモンと実行プロセスがすべて停止したか確認できませんでした。",
    cancelAfterStop: "キャンセルすると Multica は開いたままとなり、デーモンは停止したままです。バックグラウンドの実行が続く可能性があります。",
    cancelUnconfirmed: "キャンセルすると Multica は開いたままとなり、デーモンの自動再起動は無効のままです。バックグラウンドの実行が続く可能性があります。",
    cancelQuit: "終了をキャンセル",
    ignoreAndQuit: "無視して終了",
  },
  ko: {
    title: "Multica 종료",
    activeRuns: (count) => `이 기기에서 ${count}개의 실행이 진행 중입니다.`,
    unknownRuns: "이 기기에서 진행 중인 실행이 있는지 확인할 수 없습니다.",
    interruptRuns: "지금 데몬을 중지하면 이 실행들이 중단됩니다.",
    interruptUnknownRuns: "데몬을 중지하면 이 기기의 실행이 중단될 수 있습니다.",
    cancel: "취소",
    stopAndQuit: "지금 중지하고 종료",
    failureTitle: "중지 완료를 확인할 수 없음",
    daemonStopped: "데몬은 중지되었지만 실행 프로세스가 모두 종료되었는지 확인할 수 없습니다.",
    shutdownUnknown: "데몬과 실행 프로세스가 모두 중지되었는지 확인할 수 없습니다.",
    cancelAfterStop: "취소하면 Multica는 열린 상태로 유지되고 데몬은 중지된 상태로 유지됩니다. 백그라운드 실행은 계속될 수 있습니다.",
    cancelUnconfirmed: "취소하면 Multica는 열린 상태로 유지되고 데몬은 자동으로 다시 시작되지 않습니다. 백그라운드 실행은 계속될 수 있습니다.",
    cancelQuit: "종료 취소",
    ignoreAndQuit: "무시하고 종료",
  },
};

function quitLabels(preferredLanguage: string): QuitLabels {
  const preferred = preferredLanguage.toLowerCase();
  if (preferred.startsWith("zh")) {
    return /^zh-(hant|tw|hk|mo)\b/.test(preferred) ? labels["zh-Hant"] : labels["zh-Hans"];
  }
  if (preferred.startsWith("ja")) return labels.ja;
  if (preferred.startsWith("ko")) return labels.ko;
  return labels.en;
}

export function quitConfirmationOptions(
  preferredLanguage: string,
  activeRuns: number | null,
): MessageBoxOptions {
  const copy = quitLabels(preferredLanguage);
  return {
    type: "warning",
    title: copy.title,
    message: activeRuns === null ? copy.unknownRuns : copy.activeRuns(activeRuns),
    detail: activeRuns === null ? copy.interruptUnknownRuns : copy.interruptRuns,
    buttons: [copy.cancel, copy.stopAndQuit],
    defaultId: 0,
    cancelId: 0,
    noLink: true,
  };
}

export function quitFailureOptions(
  preferredLanguage: string,
  daemonStopped: boolean,
): MessageBoxOptions {
  const copy = quitLabels(preferredLanguage);
  return {
    type: "warning",
    title: copy.failureTitle,
    message: daemonStopped ? copy.daemonStopped : copy.shutdownUnknown,
    detail: daemonStopped ? copy.cancelAfterStop : copy.cancelUnconfirmed,
    buttons: [copy.cancelQuit, copy.ignoreAndQuit],
    defaultId: 0,
    cancelId: 0,
    noLink: true,
  };
}
