// Docs-site path prefix for a UI language. English docs are prefix-less;
// every other docs locale lives under `/docs/<lang>/`.
export function docsLocalePrefix(language?: string): string {
  // Check Traditional before the generic `zh` match, which would otherwise
  // send zh-Hant readers to the Simplified docs.
  if (isTraditionalChinese(language)) return "/zh-Hant";
  if (language?.startsWith("zh")) return "/zh";
  if (language?.startsWith("ja")) return "/ja";
  if (language?.startsWith("ko")) return "/ko";
  if (language?.startsWith("fr")) return "/fr";
  return "";
}

export function isTraditionalChinese(language?: string): boolean {
  return language?.toLowerCase().startsWith("zh-hant") ?? false;
}
