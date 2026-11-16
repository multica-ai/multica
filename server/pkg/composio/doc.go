// Package composio provides a standalone client for the Composio REST API.
// It covers connection management, toolkit/auth-config discovery, MCP sessions,
// tool execution, and webhook verification without Multica-specific dependencies.
//
// Non-2xx responses return an APIError with upstream status, slug, and message;
// transport errors remain inspectable with errors.Is/As. VerifyWebhook checks
// HMAC-SHA256 signatures with a configurable replay tolerance.
package composio
