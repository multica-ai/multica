# Hermes native Chat guidance

Hermes uses the shared direct Chat Send / Queue message flow and Supplement
claim/ack loop. The adapter uses standard ACP and Hermes' native `/steer` command;
it requires no Hermes source modification or private ACP extension.

Send attempts guidance after the current ACP session advertises `steer` in an
`available_commands_update` and the primary prompt emits content or tool activity.
An absent command, incompatible peer or unsupported run keeps ordinary queueing.
Queue message always queues. Issue runs, channel-backed sessions and quick-action
refreshes are outside this adapter's scope.

## Activation and delivery

The daemon does not advertise Hermes Supplement support at task start. After
native steering becomes ready, it activates the existing server capability through
the authenticated claim endpoint with
`active_turn_guidance: "hermes-acp-steer-v1"`. Activation is idempotent and cannot
claim input. The server locks the exact running task and excludes other providers,
issues, channels and quick-action refreshes. A transient activation error can retry
only while the adapter remains ready.

The normal Send transaction reads the capability at send time. Existing
`chat_task_supplement` rows, input ownership, submission replay, retry/resume
accounting and cancellation settlement are reused. No migration, extra receipt
store, UI action or realtime event is added.

Each claimed message becomes one concurrent standard ACP `session/prompt` with a
single text block containing `/steer <formatted instruction>`. The adapter waits
only for that command's normal ACP response, not for model incorporation. A normal
`end_turn` acknowledges the command. The exact native command acknowledgement is
excluded from the assistant transcript, and the command response cannot finish
the primary prompt or replace its output/usage.

Expected acknowledgement signatures survive RPC completion or timeout until
observed or session closure. Each exact preview is consumed once per command,
including identical previews; ordinary text with a similar prefix is preserved.

No private capability, immutable owner-turn notification, guidance-status lookup
or Hermes incorporation receipt is requested. The adapter does not resend a
command after transport loss. Refusal, cancellation, malformed results and a
bounded response timeout use the shared negative acknowledgement and automatic
Multica queue fallback.

## Guarantees and limits

`delivered` means that the native Hermes command completed normally; it does not
mean the model incorporated or followed the instruction. This matches a handoff
contract rather than a model-incorporation contract.

Hermes itself can turn `/steer` into a subsequent prompt if the active turn ends
before it handles the command. A successful command response therefore does not
prove delivery to the original turn. If the response is lost or delayed, automatic
Multica fallback can duplicate input that Hermes already accepted or queued. This
is at-least-once delivery, without exact-turn or exactly-once guarantees.

Primary completion closes readiness. Existing Multica rules decide what happens
to pending/in-flight guidance on completion, automatic retry, Stop, revocation and
cancellation grace-period expiry. Provider side effects cannot be made atomic
with a Multica server receipt.

## Verification without accounts

Default Go tests use controlled ACP subprocesses. They cover native command
advertisement, session matching, refused/cancelled/lost command responses,
transcript filtering, primary-result isolation and no command replay.

Source integration is opt-in: set `MULTICA_HERMES_SOURCE` to an unmodified Hermes
checkout and `MULTICA_HERMES_TEST_PYTHON` to an isolated interpreter with its test
dependencies. Run `TestHermesNativeGatewayACPTransport` and
`TestHermesNativeGatewayChatSupplementEndToEnd` with `-race` against a managed
disposable database. The probe in `server/pkg/agent/testdata/hermes_native_gateway.py`
uses the real stock ACP command routing and gateway execution owner. Only its
loopback model, tool barrier and transport observation are controlled. No installed
agent executable, account, hosted model or receipt-bearing Hermes patch is used.
