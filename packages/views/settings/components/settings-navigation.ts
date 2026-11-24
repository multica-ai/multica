/** Messaging integrations that live under the Channels page. */
export const CHANNEL_INTEGRATIONS = [
  "lark",
  "slack",
  "dingtalk",
  "wecom",
  "telegram",
  "weixin",
] as const;

export type ChannelIntegration = (typeof CHANNEL_INTEGRATIONS)[number];

export function isChannelIntegration(
  value: string | null,
): value is ChannelIntegration {
  return (CHANNEL_INTEGRATIONS as readonly string[]).includes(value ?? "");
}

/**
 * Preferences anchors that moved to the Appearance page, and where they land
 * there. The light/dark picker was `theme` under Preferences; on the
 * Appearance page that row is `mode` and `theme` is the theme gallery.
 */
const APPEARANCE_SECTIONS: Record<string, string | null> = {
  appearance: null,
  theme: "mode",
  style: "theme",
};

export interface SettingsLocation {
  tab: string;
  /** In-page anchor to scroll to (see `SettingsSection`/`SettingsRow`). */
  section: string | null;
  /** Reserved for settings locations outside messaging. */
  integration: string | null;
}

function isComposioCallback(params: URLSearchParams) {
  return params.has("connected") || params.get("error") === "composio_connect_failed";
}

// A plan refusal (`reason=feature_locked`) can only be for a team account.
function isPersonalComposioCallback(params: URLSearchParams) {
  return isComposioCallback(params) && params.get("scope") !== "team" && params.get("reason") !== "feature_locked";
}

/**
 * Resolve a settings URL, including links that predate the current layout.
 * Server redirects still point at some of them — the GitHub App install flow
 * returns to `?tab=github` / `?tab=repositories` and the Composio OAuth flow to
 * `?tab=apps&connected=…[&scope=team]` (once `?tab=integrations&connected=…`)
 * — so these mappings are part of the URL contract, not bookmarks we can drop.
 *
 * The workspace's apps and accounts left settings for the Apps page: `apps`
 * here means "redirect to it", and section `accounts` its Accounts tab, where
 * a personal connection returns. `accounts` and `connected-accounts` were
 * settings tabs before.
 */
export function resolveSettingsLocation(
  params: URLSearchParams,
): SettingsLocation {
  const tab = params.get("tab") ?? "profile";
  const section = params.get("section");
  const integration = params.get("integration");
  switch (tab) {
    case "issue-statuses":
      return { tab: "workflows", section: null, integration: null };
    case "issue":
    case "chat":
      return { tab: "preferences", section: tab, integration: null };
    case "preferences":
      if (section && Object.hasOwn(APPEARANCE_SECTIONS, section)) {
        return { tab: "appearance", section: APPEARANCE_SECTIONS[section] ?? null, integration: null };
      }
      return { tab, section, integration };
    case "labs":
      return { tab: "workspace", section: null, integration: null };
    case "github":
      return { tab: "code", section, integration: null };
    case "repositories":
      return { tab: "code", section: section ?? "repositories", integration: null };
    case "channels":
      return { tab: "channels", section, integration: null };
    case "accounts":
    case "connected-accounts":
      return { tab: "apps", section: "accounts", integration: null };
    case "apps":
      if (isPersonalComposioCallback(params)) {
        return { tab: "apps", section: "accounts", integration: null };
      }
      return { tab: "apps", section: null, integration: null };
    case "integrations": {
      if (isPersonalComposioCallback(params)) {
        return { tab: "apps", section: "accounts", integration: null };
      }
      if (isComposioCallback(params) || integration === "composio") {
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
        integration: null,
      };
    }
    default:
      if (isChannelIntegration(tab)) {
        return { tab: "channels", section: null, integration: null };
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
  params.delete("scope");
  if (detail.section) params.set("section", detail.section);
  if (detail.integration) params.set("integration", detail.integration);
  return `${pathname}?${params.toString()}`;
}

/** The Apps page for a retired settings link, keeping the connect result. */
export function movedAppsHref(appsPath: string, searchParams: URLSearchParams, section: string | null = null) {
  const params = new URLSearchParams();
  if (section) params.set("tab", section);
  for (const key of ["connected", "error", "reason"]) {
    const value = searchParams.get(key);
    if (value) params.set(key, value);
  }
  const search = params.toString();
  return search ? `${appsPath}?${search}` : appsPath;
}
