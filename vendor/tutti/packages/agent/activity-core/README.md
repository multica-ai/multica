# @tutti-os/agent-activity-core

Pure presentation contracts and helpers used by the owned Agent GUI. This package
has no React, transport, persistence, command executor or mutable session engine.
Desktop's native conversation models and services live in
`packages/conversation-client`; GUI-specific computed values live in
`vendor/tutti/packages/agent/gui/state`.

## Public contracts

The package exports session, turn, interaction, message, composer-option and
usage types, plus the presentation types needed by native service adapters.
Canonical session metadata, pending activations and queued prompt displays remain
separate contracts. A display record does not authorize execution or establish
that a command succeeded.

Use `normalizeAgentActivitySession` at a presentation boundary to fill optional
session fields. Unknown providers keep their identity. Capabilities are resolved
from canonical session and composer-option testimony; the GUI does not infer
provider execution capabilities from a name.

## Pure helpers

- `resolveAgentActivityCapability` and `resolveAgentActivityUsage` resolve
  capability and usage testimony.
- `mergeAgentActivityMessages` merges messages by their durable identity and
  version, preserving newer content when an older page arrives.
- `loadAllAgentSessionMessages` follows paged reads supplied by its caller.
- `providerForkBindingAllowsAttempt` checks the canonical fork boundary.
- Presentation comparison helpers preserve unchanged activation and pending
  submission values across native updates.

The package does not own subscriptions or a second copy of conversation data.
Views receive service/model instances and read them from `observer` consumers.
State changes, queue admission, interaction responses and cancellation belong to
the native services rather than a reducer protocol in this package.

## Verification

Run the repository's `pnpm typecheck` and `pnpm test`. State matrices belong beside
the native services; GUI wiring and rendering checks belong beside the GUI
components. Fake transports provide execution results in default tests.
