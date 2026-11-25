export const COMPOSIO_MCP_APPS_FLAG = "composio_mcp_apps";
export const PLUGINS_V1_FLAG = "plugins_v1";
export const DOCS_V1_FLAG = "docs_v1";
// Lets a Feishu/Lark bot serve group senders outside the workspace (#11437).
export const LARK_OPEN_GROUP_ACCESS_FLAG = "lark_open_group_access";
export const BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG =
  "billing_workspace_subscriptions";
// Guests (MUL-9420): free, seatless members limited to shared projects. Not a
// flag of its own: the server reports whether this deployment offers adding
// guests (always on self-host; on Cloud once billing launches), and the plan
// then decides through the "guests" paid feature.
export const WORKSPACE_GUESTS_FLAG = "workspace_guests";
// The 365-day range on the workspace dashboard (MUL-9490). Servers that
// support it publish it on by default; a missing key reads as off, so the
// range is never offered to a server that predates it.
export const DASHBOARD_HISTORY_365D_FLAG = "dashboard_history_365d";
