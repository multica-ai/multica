/** Resolve bookmarks for settings pages that now live inside another page. */
export function resolveSettingsLocation(params: URLSearchParams) {
  const tab = params.get("tab") ?? "profile";
  if (tab === "issue" || tab === "chat") {
    return { tab: "preferences", section: tab, integration: null };
  }
  if (tab === "github" || tab === "lark") {
    return { tab: "integrations", section: null, integration: tab };
  }
  return {
    tab: tab === "labs" ? "workspace" : tab,
    section: params.get("section"),
    integration: params.get("integration"),
  };
}

/** Clear page-specific state while preserving unrelated callback parameters. */
export function settingsHref(
  pathname: string,
  searchParams: URLSearchParams,
  tab: string,
  detail: { section?: string; integration?: string } = {},
) {
  const params = new URLSearchParams(searchParams);
  params.set("tab", tab);
  params.delete("section");
  params.delete("integration");
  if (detail.section) params.set("section", detail.section);
  if (detail.integration) params.set("integration", detail.integration);
  return `${pathname}?${params.toString()}`;
}
