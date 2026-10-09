import { docsLocalePrefix } from "../../common/docs-locale";

export function daemonRuntimesDocsHref(language?: string): string {
  return `https://multica.ai/docs${docsLocalePrefix(language)}/daemon-runtimes`;
}

export function customRuntimeDocsHref(language?: string): string {
  const base = daemonRuntimesDocsHref(language);
  if (language?.startsWith("zh")) {
    return `${base}#${encodeURIComponent("自定义运行时配置")}`;
  }
  if (language?.startsWith("ja")) {
    return `${base}#${encodeURIComponent("カスタムランタイムプロファイル")}`;
  }
  if (language?.startsWith("ko")) {
    return `${base}#${encodeURIComponent("사용자-지정-런타임-프로필")}`;
  }
  if (language?.startsWith("fr")) {
    return `${base}#${encodeURIComponent("profils-de-runtime-personnalisés")}`;
  }
  return `${base}#custom-runtime-profiles`;
}
