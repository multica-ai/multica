"use client";

import { useQuery } from "@tanstack/react-query";
import { ApiError, errorCode } from "@multica/core/api";
import { composioToolkitsOptions } from "@multica/core/composio";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";

/**
 * Whether Connected apps exist on this server. A pending or failed catalog
 * read still counts as available — only an explicit "Composio is not
 * configured on this server" hides the surfaces, so a transient error never
 * removes them.
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
