export const COMPOSIO_MCP_APPS_FLAG = "composio_mcp_apps";

/**
 * Gates the group-based inbox on web and desktop.
 *
 * Independent of the server's write gate: the backend can be writing groups
 * long before any client reads them, and this switch can go back off at any
 * time because inbox_item stays the complete v1 truth throughout.
 */
export const INBOX_V2_FLAG = "inbox_v2";
