# @multica/cerebro-dictation

Cerebro fork-only Whisper dictation. The headless layer (hook + utilities)
that any focused text input in Multica can use to record audio, send it to
the cerebro transcribe endpoint, and insert the resulting text at the
caret. Push-to-talk in the chat input is the primary trigger today; the
hook is intentionally generic so issue/comment/settings text fields can
reuse it later.

Slice 1 ships only this headless package — the actual UI (`MicButton`)
and backend (`/api/cerebro/dictation/transcribe`) land in slices 2 and 3.
The feature is gated behind the `cerebro_voice_dictation_enabled` flag in
`@multica/cerebro-feature-flags`. (The two voice-output toggles live next to
it: `cerebro_voice_output_enabled` and `cerebro_voice_summary_enabled` — see
JEH-740.)

## Owns

- `useDictation()` — React hook wrapping the `MediaRecorder` lifecycle
  (permission → record → transcribe via injected function → result).
- `insertAtCaret()` — DOM utility that inserts text at the cursor of any
  `<textarea>`, single-line text `<input>`, or `contenteditable` element,
  preserving existing input and dispatching the right events so consumers
  (TipTap, controlled forms) see the change.
- `Transcriber` interface — the contract slice 3's API client implements.

## May land here

- Audio capture configuration (codec preference list, max duration,
  silence trim) — but only the headless logic. UI surfacing of those
  options goes in `views/`.
- The `MicButton` view (slice 2) under `views/`.
- A test harness for mocking `MediaRecorder` / `getUserMedia` in the few
  places that compose the hook.

## May NOT land here

- The transcribe HTTP client itself — that lives in `@multica/core/api`
  alongside other cerebro endpoints, and is passed in as the
  `transcribe` option. Keeping HTTP out of this package means it can be
  unit-tested without mocking the network.
- Any upstream chat-input modifications — those land via a CEREBRO-PATCH
  marker in `packages/views/chat/components/chat-input.tsx` (slice 2).

## Imports

- May import from: `@multica/cerebro-feature-flags`.
- May NOT import from: `@multica/core`, `@multica/views`, `@multica/ui`,
  any other `cerebro-*` package, or `apps/*`. The point is that any
  text-input in any package can depend on this without pulling in
  view-layer or app-layer code.
