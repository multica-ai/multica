import { observer } from "mobx-react-lite";
import React, {
  createContext,
  useCallback,
  useContext,
  useMemo,
  type ReactNode
} from "react";
import {
  createLocaleObjectI18nModuleManifest,
  type I18nRuntime
} from "@tutti-os/ui-i18n-runtime";
import * as agentGuiI18nRuntime from "./runtime.ts";
import type {
  AgentGuiI18nLocale,
  TranslateFn,
  TranslateOptions
} from "./runtime.ts";

export const agentGuiI18nResources = agentGuiI18nRuntime.agentGuiI18nResources;
export const getActiveUiLanguage = agentGuiI18nRuntime.getActiveUiLanguage;
export const translate = agentGuiI18nRuntime.translate;
export const translateInUiLanguage = agentGuiI18nRuntime.translateInUiLanguage;
export type { AgentGuiI18nLocale, TranslateFn, TranslateOptions };

export const agentGuiI18nModule = createLocaleObjectI18nModuleManifest({
  fileByLocale: {
    en: "packages/agent/gui/app/renderer/i18n/locales/en.ts",
    "zh-CN": "packages/agent/gui/app/renderer/i18n/locales/zh-CN.ts"
  },
  name: "agent-gui",
  sourceRoot: "packages/agent/gui"
});

const AgentGuiI18nContext = createContext<{
  locale: AgentGuiI18nLocale;
  runtime: I18nRuntime<string>;
} | null>(null);

export const AgentGuiI18nProvider = observer(function AgentGuiI18nProvider({
  children,
  locale = "en",
  runtime
}: {
  children: ReactNode;
  locale?: AgentGuiI18nLocale;
  runtime?: I18nRuntime<string> | null;
}): React.ReactElement {
  const value = useMemo(
    () =>
      agentGuiI18nRuntime.resolveAgentGuiI18nRuntime({
        locale,
        runtime
      }),
    [locale, runtime]
  );

  agentGuiI18nRuntime.setCurrentAgentGuiI18nRuntime(value);

  return React.createElement(AgentGuiI18nContext.Provider, { value }, children);
});

export function useTranslation(): {
  i18n: I18nRuntime<string>;
  locale: AgentGuiI18nLocale;
  t: TranslateFn;
} {
  const context =
    useContext(AgentGuiI18nContext) ??
    agentGuiI18nRuntime.getCurrentAgentGuiI18nRuntime();
  const t = useCallback(
    (key: string, options?: TranslateOptions) =>
      context.runtime.t(key, options),
    [context.runtime]
  );

  return {
    i18n: context.runtime,
    locale: context.locale,
    t
  };
}
