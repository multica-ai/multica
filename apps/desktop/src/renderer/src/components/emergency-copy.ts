import type { SupportedLocale } from "@multica/core/i18n";

const LOCALE_STORAGE_KEY = "multica-locale";

function matchSupportedLocale(raw: string): SupportedLocale {
  const parts = raw
    .trim()
    .toLowerCase()
    .replace(/_/g, "-")
    .split("-")
    .filter(Boolean);
  const lang = parts[0];
  if (lang === "zh") {
    const isTraditional = parts
      .slice(1)
      .some((sub) => sub === "hant" || sub === "tw" || sub === "hk" || sub === "mo");
    return isTraditional ? "zh-Hant" : "zh-Hans";
  }
  if (lang === "ja") return "ja";
  if (lang === "ko") return "ko";
  if (lang === "fr") return "fr";
  if (lang === "es") return "es";
  if (lang === "ru") return "ru";
  return "en";
}

function readStoredLocale(): string | null {
  try {
    return typeof window !== "undefined"
      ? window.localStorage.getItem(LOCALE_STORAGE_KEY)
      : null;
  } catch {
    return null;
  }
}

export function resolveDesktopEmergencyLocale(
  preferredLocale?: SupportedLocale,
): SupportedLocale {
  if (preferredLocale) return preferredLocale;
  const stored = readStoredLocale();
  if (stored) return matchSupportedLocale(stored);
  const systemLocale =
    typeof window !== "undefined" ? window.desktopAPI?.systemLocale : "";
  if (systemLocale) return matchSupportedLocale(systemLocale);
  const docLang =
    typeof document !== "undefined" ? document.documentElement.lang : "";
  if (docLang && docLang.toLowerCase() !== "en") {
    return matchSupportedLocale(docLang);
  }
  const navLang =
    typeof navigator !== "undefined" ? navigator.language : "";
  return matchSupportedLocale(navLang || docLang || "en");
}

export const DESKTOP_EMERGENCY_COPY: Record<
  SupportedLocale,
  {
    crashTitle: string;
    crashDescription: string;
    crashFallbackError: string;
    reload: string;
    configTitle: string;
    configDescriptionBefore: string;
    configDescriptionAfter: string;
  }
> = {
  en: {
    crashTitle: "Something went wrong",
    crashDescription:
      "Multica hit an unexpected error and could not keep rendering. Reloading usually recovers — your work is stored on the server.",
    crashFallbackError: "An unexpected error occurred.",
    reload: "Reload",
    configTitle: "Desktop configuration error",
    configDescriptionBefore: "Multica could not load ",
    configDescriptionAfter: ". Fix or remove the file and restart the app.",
  },
  "zh-Hans": {
    crashTitle: "出现了问题",
    crashDescription:
      "Multica 遇到意外错误，无法继续渲染。重新加载通常可以恢复，你的数据已保存在服务端。",
    crashFallbackError: "发生了意外错误。",
    reload: "重新加载",
    configTitle: "桌面端配置错误",
    configDescriptionBefore: "Multica 无法加载 ",
    configDescriptionAfter: "。请修复或删除该文件后重启应用。",
  },
  "zh-Hant": {
    crashTitle: "發生了問題",
    crashDescription:
      "Multica 遇到意外錯誤，無法繼續渲染。重新載入通常可恢復，你的資料已儲存在伺服器上。",
    crashFallbackError: "發生了意外錯誤。",
    reload: "重新載入",
    configTitle: "桌面端設定錯誤",
    configDescriptionBefore: "Multica 無法載入 ",
    configDescriptionAfter: "。請修復或移除該檔案後重新啟動應用程式。",
  },
  ja: {
    crashTitle: "問題が発生しました",
    crashDescription:
      "Multica で予期しないエラーが発生し、描画を続けられませんでした。再読み込みで通常は回復します。作業内容はサーバーに保存されています。",
    crashFallbackError: "予期しないエラーが発生しました。",
    reload: "再読み込み",
    configTitle: "デスクトップ設定エラー",
    configDescriptionBefore: "Multica は ",
    configDescriptionAfter:
      " を読み込めませんでした。ファイルを修正または削除してアプリを再起動してください。",
  },
  ko: {
    crashTitle: "문제가 발생했습니다",
    crashDescription:
      "Multica에서 예기치 않은 오류가 발생해 화면을 계속 표시할 수 없습니다. 다시 불러오면 대부분 복구되며 작업 내용은 서버에 저장되어 있습니다.",
    crashFallbackError: "예기치 않은 오류가 발생했습니다.",
    reload: "다시 불러오기",
    configTitle: "데스크톱 설정 오류",
    configDescriptionBefore: "Multica에서 ",
    configDescriptionAfter:
      " 파일을 불러올 수 없습니다. 파일을 수정하거나 삭제한 뒤 앱을 다시 시작하세요.",
  },
  fr: {
    crashTitle: "Une erreur s'est produite",
    crashDescription:
      "Multica a rencontré une erreur inattendue et ne peut plus s'afficher. Recharger résout généralement le problème — votre travail est enregistré sur le serveur.",
    crashFallbackError: "Une erreur inattendue s'est produite.",
    reload: "Recharger",
    configTitle: "Erreur de configuration",
    configDescriptionBefore: "Multica n'a pas pu charger ",
    configDescriptionAfter:
      ". Corrigez ou supprimez le fichier, puis redémarrez l'application.",
  },
  es: {
    crashTitle: "Algo ha ido mal",
    crashDescription:
      "Multica encontró un error inesperado y no pudo seguir renderizando. Recargar suele solucionarlo; tu trabajo está guardado en el servidor.",
    crashFallbackError: "Se produjo un error inesperado.",
    reload: "Recargar",
    configTitle: "Error de configuración",
    configDescriptionBefore: "Multica no pudo cargar ",
    configDescriptionAfter:
      ". Corrige o elimina el archivo y reinicia la aplicación.",
  },
  ru: {
    crashTitle: "Что-то пошло не так",
    crashDescription:
      "В Multica произошла непредвиденная ошибка. Перезагрузка обычно помогает — ваши данные сохранены на сервере.",
    crashFallbackError: "Произошла непредвиденная ошибка.",
    reload: "Перезагрузить",
    configTitle: "Ошибка конфигурации",
    configDescriptionBefore: "Multica не удалось загрузить ",
    configDescriptionAfter:
      ". Исправьте или удалите этот файл и перезапустите приложение.",
  },
};
