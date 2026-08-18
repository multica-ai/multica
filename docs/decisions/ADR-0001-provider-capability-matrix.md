# ADR-0001: Centralize provider capability and compatibility declarations

- **Date:** 2026-08-18
- **Status:** proposed

## Context and Problem

Provider behavior is represented by adapter factories, daemon conditionals, runtime context paths, MCP discovery, frontend capability tabs, and documentation. A provider change can therefore require synchronized edits across several layers. OpenClaw additionally has a native workspace instruction path and a legacy inline path, so an unconditional fallback duplicates the runtime brief for supported releases.

## Decision

`server/pkg/agent/provider_capabilities.go` is the executable source of truth for provider capability metadata. It declares supported provider families, MCP support and source locations, sandbox/approval behavior, runtime config files, workspace and user skill paths, instruction delivery, and compatibility requirements.

- `SupportedTypes` is derived from the matrix.
- Daemon runtime config and skill path resolution consume matrix rows.
- MCP discovery uses the matrix support predicate and retains provider-specific parsing only where the CLI format differs.
- The frontend MCP provider set is a generated mirror guarded by a Go canary.
- `docs/provider-capabilities.md` is generated from the matrix and tested byte-for-byte.
- OpenClaw versions below `2026.5.5` retain inline instruction delivery. Supported versions use the workspace-pinned `AGENTS.md` and `skills/` path only.

## Consequences

Adding a provider capability requires one matrix row plus the adapter implementation and any provider-specific executable behavior. Drift in the supported-type list, frontend MCP mirror, generated documentation, skill paths, or OpenClaw instruction boundary fails focused tests instead of relying on manual review.

Provider-specific parsing remains in the daemon when the external CLI uses a unique config format. The matrix records that source and the tests verify the shared support contract; it does not pretend that all providers share one wire format.

## Verification

- `TestProviderCapabilityMatrixMatchesFactory`
- `TestProviderCapabilityMatrixMatchesFrontendMCPMirror`
- `TestProviderCapabilityMatrixDocumentationIsGenerated`
- `TestOpenClawInstructionDeliveryIsVersionGated`
- `TestVersionAtLeastParsesCLIOutput`
