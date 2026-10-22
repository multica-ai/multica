import { useEffect, type ReactNode } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enUi from "@multica/views/locales/en/ui.json";
import zhUi from "@multica/views/locales/zh-Hans/ui.json";
import en from "./locales/en.json";
import zh from "./locales/zh.json";
import { productLocale, type LabLocale } from "./locale";

declare global {
  interface I18nResources {
    uiLab: typeof en;
  }
}

const resources = {
  en: { uiLab: en, ui: enUi },
  "zh-Hans": { uiLab: zh, ui: zhUi },
};

export function LabI18nProvider({
  locale,
  children,
}: {
  locale: LabLocale;
  children: ReactNode;
}) {
  useEffect(() => {
    document.documentElement.lang = productLocale(locale);
  }, [locale]);
  return (
    <I18nProvider locale={productLocale(locale)} resources={resources}>
      {children}
    </I18nProvider>
  );
}
