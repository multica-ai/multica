# Native dictation (Windows Desktop)

The mic in Chat, ticket comments/replies, and both ticket-creation composers delegates to the user's running Codex Desktop global dictation UI. Multica does not record audio or call a transcription API. Web, mobile, macOS and Linux do not receive this adapter or a mic entry.

## Setup and behavior

1. Run the official packaged Codex Desktop in the same Windows interactive session. The helper validates its Windows package identity, not just its executable name. An unpackaged build is deliberately not supported.
2. In Codex, configure the global dictation toggle as `Ctrl+Alt+Shift+D`, with no conflicting binding. This bridge expects one unconditional `globalDictationToggle` entry in Codex's `keybindings.json`; it never edits that file. `CODEX_HOME`, when set, must be absolute.
3. Focus a Multica composer, release keyboard modifiers, and click the mic. The editor preserves its selection while taking focus. Lazy comment/reply editors are mounted before dispatch.
4. Finish dictation in the native Codex UI. Text insertion and recording are owned by Codex. Multica does not submit the resulting text automatically.

A success notice means only that the shortcut was dispatched, not that audio recording or transcription succeeded. Account eligibility, subscription usage, microphone permission, availability and future compatibility remain controlled by Codex; this feature promises no free or unlimited usage. A missing app, unavailable account feature or failed bridge does not enable an API fallback.

The exact configuration shape checked by this bridge is a JSON array containing
one `{ "command": "globalDictationToggle", "key": "Ctrl+Alt+Shift+D" }` object,
with no `when` property and no second binding using that chord. Modifier order
and `Control`/`Ctrl` spelling are normalized. This is a third-party Desktop
configuration contract, not a public transcription API or proof of global
hotkey registration. A future Codex schema change fails closed; do not infer
compatibility from the Codex CLI version. Validate recording/insertion against
the actual Desktop version and keyboard layout before relying on it.

## Boundaries

- The renderer exposes a parameterless adapter. The main process admits only a registered, focused, trusted top-level renderer. Sandboxed preload captures a trusted mic click or completed Enter/Space activation, and exposes its one-shot consumer only in a dedicated isolated world. Admission expires after two seconds, clears on window blur, and requires a connected mic and focused editable input. Main-world property overrides, synthetic clicks and direct IPC cannot mint an activation.
- The mic DOM marker is UI routing, not proof of human intent against a compromised renderer that deceives the user with a fake button. The renderer still controls DOM content; do not describe this as complete renderer-compromise containment.
- Windows Desktop reserves exactly `Ctrl+Alt+Shift+D` before renderer dispatch, including an AltGr `KeyD` event. If Codex does not consume the OS hotkey, it cannot insert a stray chord character into a Multica draft. Other chords/platforms are unchanged. Dispatch success still does not prove that Codex owns the hotkey.
- Multica reads only the bounded keybindings file. It does not read Codex auth files, sessions, cookies, credentials or a keychain, and does not send anything to a Multica server for dictation.
- The bundled Multica CLI implements the private `--desktop-dictation-v1` helper before ordinary CLI profile/update handling. It accepts only a read-only `probe`, or `toggle` with a canonical nonzero decimal native window handle.
- The Windows helper verifies the current foreground window, official Codex package identity and interactive session, and rejects held modifiers/submit keys. It sends only the fixed chord, with bounded partial-injection cleanup and no toggle retry. It does not elevate, change permissions, start/stop agents or execute user scripts.
- Only one toggle can be pending across all Multica windows. Unknown state, a missing/old bundled helper or a timeout fails closed.
- If a partial SendInput cleanup also fails, the UI asks the user to press/release Ctrl, Alt, Shift and D before typing and check Codex's state before toggling again. There is one cleanup attempt, never a second toggle.

## Verification

Unit tests use mocked Electron/IPC and fake native platforms. A real lazy comment/reply + mic + Tiptap test uses only a fake host adapter. Windows ABI tests encode INPUT structures without sending keyboard input. They cover focus, frame/origin, gesture, configuration, native identity, held-key, partial-input, lazy editor and no-network/no-audio behavior.

Run `pnpm --filter @multica/desktop exec electron scripts/native-dictation-smoke.mjs`
for a separate hidden Electron/Chromium fixture. It loads the actual isolated
preload and main guard with config/native calls forbidden, proves a trusted
Chromium mic activation is one-shot, rejects main-world property spoofing and
synthetic clicks, and intercepts the unconsumed chord. It uses a temporary
profile and Chromium-local input events, never OS SendInput, installed app
profiles, credentials, microphone access or audio. Tested on Windows x64 with
Electron 39.8.7. This is boundary evidence, not recording acceptance.

Real recording and transcript insertion require a manual acceptance check with an eligible signed-in Codex account and microphone permission. Do not treat the read-only helper probe or the automated tests as end-to-end audio verification.
