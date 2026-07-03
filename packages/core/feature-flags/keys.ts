export const COMPOSIO_MCP_APPS_FLAG = "composio_mcp_apps";

/**
 * AGENT_ACCESS_PICKER_FLAG gates the MUL-3963-aligned access UI in the
 * agent create/duplicate flow. When ON, the visibility section switches from
 * the legacy Workspace/Personal toggle to the new Private / Public-to model
 * (`permission_mode` + `invocation_targets`) that matches AccessPicker on
 * the agent detail page. Defaults to OFF so production stays on the legacy
 * toggle until the rollout is greenlit.
 */
export const AGENT_ACCESS_PICKER_FLAG = "agent_access_picker";
