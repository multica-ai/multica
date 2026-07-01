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
