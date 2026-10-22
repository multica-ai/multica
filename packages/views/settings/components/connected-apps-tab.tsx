"use client";

import { useQuery } from "@tanstack/react-query";
import { ApiError, errorCode } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import { useT } from "../../i18n";
import { ComposioTab } from "./composio-tab";
import { SettingsTab } from "./settings-layout";

/**
 * Whether the Connected apps page exists. A pending or failed catalog read
 * still counts as available — only an explicit "Composio is not configured on
 * this server" hides the page, so a transient error never removes it.
 */
export function useComposioAvailable(): boolean {
  const enabled = useFeatureEnabled(COMPOSIO_MCP_APPS_FLAG, false);
  const toolkits = useQuery({ ...composioToolkitsOptions(), enabled });
  return (
    enabled &&
    errorCode(toolkits.error) !== "composio_not_configured" &&
    !(toolkits.error instanceof ApiError && toolkits.error.status === 503)
  );
}

/**
 * App connections belong to the signed-in account, not the workspace: the
 * user authorizes their own Gmail or Notion and enables it on agents they own.
 */
export function ConnectedAppsTab() {
  const { t } = useT("settings");
  return (
    <SettingsTab
      title={t(($) => $.page.tabs.apps)}
      description={t(($) => $.composio.page_description)}
      scope="account"
    >
      <ComposioTab />
    </SettingsTab>
  );
}
