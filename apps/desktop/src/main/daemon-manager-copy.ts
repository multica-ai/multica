import { resolveDialogLocale, type DialogLocale } from "./dialog-locale";

export interface DaemonErrorCopy {
  updateInProgress: string;
  appQuitting: string;
  cliNotInstalled: string;
  waitingServiceAddress: string;
  recoverySuperseded: string;
  logFileNotFound: string;
  cliNotAvailable: string;
  couldNotStopDaemon: string;
  failedToRestartDaemon: string;
}

const DAEMON_ERROR_COPY: Record<DialogLocale, DaemonErrorCopy> = {
  en: {
    updateInProgress: "An update is being installed. Try again shortly.",
    appQuitting: "Application is quitting.",
    cliNotInstalled: "Multica CLI is not installed.",
    waitingServiceAddress: "Waiting for the service address.",
    recoverySuperseded: "Daemon recovery was superseded.",
    logFileNotFound: "Log file not found yet.",
    cliNotAvailable: "Multica CLI is not available.",
    couldNotStopDaemon: "Could not stop the daemon.",
    failedToRestartDaemon: "Failed to restart daemon.",
  },
  "zh-Hans": {
    updateInProgress: "正在安装更新，请稍后再试。",
    appQuitting: "应用正在退出。",
    cliNotInstalled: "尚未安装 Multica CLI。",
    waitingServiceAddress: "正在等待服务地址。",
    recoverySuperseded: "守护进程恢复已被新操作取代。",
    logFileNotFound: "尚未找到日志文件。",
    cliNotAvailable: "Multica CLI 当前不可用。",
    couldNotStopDaemon: "无法停止守护进程。",
    failedToRestartDaemon: "无法重启守护进程。",
  },
  "zh-Hant": {
    updateInProgress: "正在安裝更新，請稍後再試。",
    appQuitting: "應用程式正在結束。",
    cliNotInstalled: "尚未安裝 Multica CLI。",
    waitingServiceAddress: "正在等待服務位址。",
    recoverySuperseded: "守護程序恢復已被新操作取代。",
    logFileNotFound: "尚未找到記錄檔。",
    cliNotAvailable: "Multica CLI 目前無法使用。",
    couldNotStopDaemon: "無法停止守護程序。",
    failedToRestartDaemon: "無法重新啟動守護程序。",
  },
  ja: {
    updateInProgress: "アップデートをインストール中です。しばらくしてから再試行してください。",
    appQuitting: "アプリケーションを終了しています。",
    cliNotInstalled: "Multica CLI がインストールされていません。",
    waitingServiceAddress: "サービスアドレスを待機しています。",
    recoverySuperseded: "デーモンの復旧処理は新しい操作に置き換えられました。",
    logFileNotFound: "ログファイルがまだ見つかりません。",
    cliNotAvailable: "Multica CLI を利用できません。",
    couldNotStopDaemon: "デーモンを停止できませんでした。",
    failedToRestartDaemon: "デーモンを再起動できませんでした。",
  },
  ko: {
    updateInProgress: "업데이트를 설치하는 중입니다. 잠시 후 다시 시도하세요.",
    appQuitting: "앱을 종료하는 중입니다.",
    cliNotInstalled: "Multica CLI가 설치되어 있지 않습니다.",
    waitingServiceAddress: "서비스 주소를 기다리는 중입니다.",
    recoverySuperseded: "데몬 복구가 새 작업으로 대체되었습니다.",
    logFileNotFound: "로그 파일을 아직 찾을 수 없습니다.",
    cliNotAvailable: "Multica CLI를 사용할 수 없습니다.",
    couldNotStopDaemon: "데몬을 중지하지 못했습니다.",
    failedToRestartDaemon: "데몬을 다시 시작하지 못했습니다.",
  },
  fr: {
    updateInProgress: "Une mise à jour est en cours d'installation. Réessayez dans un instant.",
    appQuitting: "L'application est en cours de fermeture.",
    cliNotInstalled: "Multica CLI n'est pas installé.",
    waitingServiceAddress: "En attente de l'adresse du service.",
    recoverySuperseded: "La reprise du daemon a été remplacée.",
    logFileNotFound: "Fichier journal introuvable pour le moment.",
    cliNotAvailable: "Multica CLI n'est pas disponible.",
    couldNotStopDaemon: "Impossible d'arrêter le daemon.",
    failedToRestartDaemon: "Impossible de redémarrer le daemon.",
  },
  es: {
    updateInProgress: "Se está instalando una actualización. Inténtalo de nuevo en unos momentos.",
    appQuitting: "La aplicación se está cerrando.",
    cliNotInstalled: "Multica CLI no está instalado.",
    waitingServiceAddress: "Esperando la dirección del servicio.",
    recoverySuperseded: "La recuperación del daemon fue reemplazada.",
    logFileNotFound: "Aún no se encuentra el archivo de registro.",
    cliNotAvailable: "Multica CLI no está disponible.",
    couldNotStopDaemon: "No se pudo detener el daemon.",
    failedToRestartDaemon: "No se pudo reiniciar el daemon.",
  },
  ru: {
    updateInProgress: "Устанавливается обновление. Повторите попытку чуть позже.",
    appQuitting: "Приложение закрывается.",
    cliNotInstalled: "Multica CLI не установлен.",
    waitingServiceAddress: "Ожидание адреса сервиса.",
    recoverySuperseded: "Восстановление демона заменено новой операцией.",
    logFileNotFound: "Файл журнала пока не найден.",
    cliNotAvailable: "Multica CLI недоступен.",
    couldNotStopDaemon: "Не удалось остановить демон.",
    failedToRestartDaemon: "Не удалось перезапустить демон.",
  },
};

export function getDaemonErrorCopy(preferredLanguage: string): DaemonErrorCopy {
  return DAEMON_ERROR_COPY[resolveDialogLocale(preferredLanguage)];
}
