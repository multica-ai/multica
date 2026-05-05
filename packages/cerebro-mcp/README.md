# @multica/cerebro-mcp

Cerebro fork MCP (Model Context Protocol) integration: tool registries,
client wrappers, and UI for configuring cerebro-specific MCP servers.

## Owns

- Cerebro-specific MCP tool registrations and metadata.
- Client wrappers that talk to cerebro-only MCP backends.
- UI for browsing and configuring those tools.

## May land here

- New MCP tools cerebro exposes that upstream does not.
- Configuration UI for cerebro-specific MCP servers.

## May NOT land here

- The generic MCP transport/protocol layer — that lives upstream (or in
  a future shared package). Keep cerebro-only on top of upstream.

## Imports

- May import from: `@multica/core`, `@multica/ui`, `@multica/views`,
  `@multica/cerebro-feature-flags`, `@multica/cerebro-types`.
- May NOT import from: `apps/*`, `next/*`, `react-router-dom`.
