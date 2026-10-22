/** Messaging integrations that live under the Channels page. */
export const CHANNEL_INTEGRATIONS = [
  "lark",
  "slack",
  "dingtalk",
  "wecom",
  "telegram",
] as const;

export type ChannelIntegration = (typeof CHANNEL_INTEGRATIONS)[number];

export function isChannelIntegration(
  value: string | null,
): value is ChannelIntegration {
  return (CHANNEL_INTEGRATIONS as readonly string[]).includes(value ?? "");
}

export interface SettingsLocation {
  tab: string;
  /** In-page anchor to scroll to (see `SettingsSection`/`SettingsRow`). */
  section: string | null;
  /** Detail page within Channels. */
  integration: string | null;
}

/**
 * Resolve a settings URL, including links that predate the current layout.
 * Server redirects still point at some of them — the GitHub App install flow
 * returns to `?tab=github` / `?tab=repositories` and the Composio OAuth flow to
 * `?tab=integrations&connected=…` — so these mappings are part of the URL
 * contract, not bookmarks we can drop.
 */
export function resolveSettingsLocation(
  params: URLSearchParams,
): SettingsLocation {
  const tab = params.get("tab") ?? "profile";
  const section = params.get("section");
  const integration = params.get("integration");
  switch (tab) {
    case "issue":
    case "chat":
      return { tab: "preferences", section: tab, integration: null };
    case "labs":
      return { tab: "workspace", section: null, integration: null };
    case "github":
      return { tab: "code", section, integration: null };
    case "repositories":
      return { tab: "code", section: section ?? "repositories", integration: null };
    case "integrations": {
      const composioCallback =
        params.has("connected") ||
        params.get("error") === "composio_connect_failed";
      if (composioCallback || integration === "composio") {
        return { tab: "apps", section: null, integration: null };
      }
      if (integration === "github") {
        return { tab: "code", section: null, integration: null };
      }
      if (integration === "vcs") {
        return { tab: "code", section: "code-hosting", integration: null };
      }
      return {
        tab: "channels",
        section: null,
        integration: isChannelIntegration(integration) ? integration : null,
      };
    }
    default:
      if (isChannelIntegration(tab)) {
        return { tab: "channels", section: null, integration: tab };
      }
      return { tab, section, integration };
  }
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
