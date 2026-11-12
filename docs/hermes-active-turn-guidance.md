# Hermes active-turn guidance (experimental)

This adapter requires a companion Hermes capability. **Unmodified upstream Hermes
is not supported for active-turn guidance by this contract.** Ordinary Hermes
prompts continue to work without it; unsupported peers never expose Add to current
run. Guidance is never enabled from an executable name or version string.

The companion proposal is
[NousResearch/hermes-agent#154368](https://github.com/NousResearch/hermes-agent/pull/154368),
validated at `ad5cfe51dab0ec90db995adbf060fa83675be461` on Hermes main
`097e69c7316102dbb47cda49cbfe9a00750cbe6a`.
It changes Hermes' ACP gateway, execution authority and iteration inbox and is
not yet an upstream release. Its contract must be agreed and merged upstream
before this adapter can be enabled for ordinary Hermes installations. Native steering of queued
Hermes admissions is not interchangeable with this active-only delivery contract.

## Negotiation and primary-turn identity

Multica requests `_meta["ai.nous/active-turn-guidance"].version = 1` during ACP
initialization. The peer must advertise version 1 and all of `statusLookup`,
`literalText` and `terminalReceipts`. Prompt metadata is added only after complete
negotiation and only when guidance was requested by the daemon.

The companion owner must advertise `active-turn-guidance-v1` and confirm the
running prompt's identity. `_ai.nous/guidanceTurn` binds the primary prompt ID to
an instance/authority epoch, session and immutable turn ID. Only that correlated
notification makes the adapter ready. ACP aliases retain their client-visible
session ID; the companion maps them to the canonical owner session internally.

Hermes chat tasks initially have no supplement capability row. Once ready, the
daemon activates the existing authenticated `/supplements/claim` endpoint with
`active_turn_guidance: "hermes-active-turn-guidance-v1"`. Activation locks the
running task, excludes channel bindings and does not claim input. Transient
activation failures may retry the idempotent activation after checking readiness;
unsupported contracts and definitive refusals stop activation.

## Delivery and recovery

The existing durable Chat receipt ID becomes `messageId`. `_ai.nous/steer` includes
`runtimeInstanceId`, `sessionId`, `expectedTurnId`, `messageId` and literal `text`.
The companion admits only the expected active turn and incorporates instructions
in order at a legal assistant/tool boundary, without dispatching slash commands,
rewriting cached history or creating a successor prompt.

Only a correlated `accepted_active` receipt with `state:incorporated` proves
incorporation. `pending_incorporation` remains in flight. Known rejection
dispositions must also carry `state:rejected`; missing, future, inconsistent or
uncorrelated values remain uncertain. An incorporated instruction is not a
promise that the model will follow it.

After a lost response, Multica calls only `_ai.nous/guidanceStatus`, never another
steer. Terminal receipts can settle delivery after the primary response. Extension
replies cannot complete the primary prompt or contaminate its result/usage.
Primary cancellation closes readiness and propagates normally.

## Companion boundaries

The companion maps ACP operations to authenticated owner methods:

| ACP extension | Owner method |
| --- | --- |
| `_ai.nous/steer` | `session.guidance.admit` |
| `_ai.nous/guidanceStatus` | `session.guidance.status` |
| `_ai.nous/guidanceTurn` / terminal metadata | `session.guidance.turn` |

The owner closes pending input as `unconsumed_at_turn_end` or
`cancelled_before_incorporation`; it never queues that input for a successor.
Its ledger is in memory, limited to 4096 accepted IDs per live session without
eviction. Restart, retirement or loss of proof leaves delivery uncertain, without
replay. Managed-worker and non-ACP execution are outside this companion's scope.
Optional guidance lookups/announcements must not replace a committed primary
result or stop its event pump.

## Verification without agent accounts

Default adapter tests use deterministic test-created processes and simulated
receipts. The real gateway transport and HTTP/PostgreSQL/daemon tests require an
explicit patched-source fixture, not an installed agent. Set
`MULTICA_HERMES_GUIDANCE_GATEWAY_FIXTURE` to the companion's
`tests/acp_adapter/guidance_gateway_fixture.py` and
`MULTICA_HERMES_GATEWAY_TEST_PYTHON` to its isolated test interpreter, then run
`TestHermesGuidanceGatewayACPTransport` and
`TestHermesGatewayChatSupplementEndToEnd` with `-race` and the managed checkout's
test database. They use the real gateway owner/AIAgent with a loopback model;
missing opt-in variables skip those integrations rather than imply a pass.
