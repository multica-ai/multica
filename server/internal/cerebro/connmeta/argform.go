// Package connmeta holds tiny, dependency-free constants about connection tools
// that must be identical across otherwise-separate surfaces. It imports nothing,
// so both the cloud gateway (server/internal/cerebro/runtime) and the local claim
// brief (server/internal/daemon/execenv) can depend on it without an import cycle.
package connmeta

// APIConnectionArgHint is the argument-shape guidance for api-connection tools.
// It is the SINGLE source rendered in an agent's first prompt on BOTH the cloud
// gateway system prompt and the local claim brief, so the two can never drift on
// the rule that query parameters go inside a `query` object (FIR-2441). Without
// it an agent discovers the `query` object only by calling once and reading the
// error (a 502 for the endpoints that then drop the top-level params).
const APIConnectionArgHint = "Some of your tools are **API connection tools** (server-side HTTP endpoints). They take a fixed argument shape: path parameters at the top level, query parameters inside a `query` object, and the request body inside `body`. Passing query parameters at the top level instead of inside `query` drops them and the call fails."

// ConnectionGuidance is the full descriptive first-prompt guidance for connection
// tools. It embeds APIConnectionArgHint and is rendered VERBATIM on both the cloud
// gateway system prompt and the local claim brief, so the two surfaces give an
// agent the same understanding of what a connection is and how to call it
// (FIR-2441 fix-list #5). Before this, only the local brief carried the full
// guidance while the live cloud tool loop shipped a bare tool-name list — so a
// cloud agent had to discover the `query` argument shape by calling and failing.
// The text is surface-neutral (no "listed below" / "(ask)" phrasing that only
// fits the local text listing) so it reads correctly wherever it is appended.
const ConnectionGuidance = "A **connection** is an external API or MCP server a workspace admin wired into this workspace (for example a customer-service backend or a data registry). You call its tools like any other tool. **MCP connection tools** are self-describing — read their schema for exact arguments. " + APIConnectionArgHint + " Tools that require approval pause for a human when you call them, and a tool you were not granted is simply not in your tool list."
