function docsLocaleSegment(language?: string): string {
  if (language?.startsWith("zh")) return "/zh";
  if (language?.startsWith("ja")) return "/ja";
  if (language?.startsWith("ko")) return "/ko";
  return "";
}

export function daemonRuntimesDocsHref(language?: string): string {
  return `https://multica.ai/docs${docsLocaleSegment(language)}/daemon-runtimes`;
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
  return `${base}#custom-runtime-profiles`;
}
