# Multica HarmonyOS (鸿蒙)

HarmonyOS client for Multica, built on RNOH (React Native for OpenHarmony). Sibling of the iOS app [`apps/mobile`](../mobile) — same backend, same product semantics, independent codebase sharing only `@multica/core` types and pure utilities. See [`AGENTS.md`](./AGENTS.md) for platform rules.

**Status: full port** — feature/UI parity with `apps/mobile`: sign-in, workspace selection/switching, inbox (swipe actions, mark-read/archive), my-issues + filters, issue detail/timeline/editing/all attribute pickers/reactions/runs, projects, pins, search, chat (sessions, composer, mentions, attachments), settings/profile/notifications, and realtime WebSocket updates throughout. Divergences are documented per-area in the file headers; login-gated flows are verified on the DevEco simulator manually.

## Try it in the DevEco simulator

Requires DevEco Studio (default install path) with a phone emulator image. From the repository root:

```bash
pnpm install
pnpm harmony:mobile-harmony:staging:release   # self-contained build: embedded JS bundle, starts without Metro
```

Prefer hot reload while developing? Use the debug pair instead (`pnpm harmony:mobile-harmony:staging` + `pnpm dev:mobile-harmony:staging`) — the debug HAP loads JS over Metro, so it needs the bundler running on your Mac.

The app connects to the public staging backend (`multica-api.copilothub.ai`) — existing accounts work. Sign in with your email and the verification code.

## Scripts

| Command | What it does |
|---|---|
| `pnpm dev:mobile-harmony:staging` | Metro only (reuse installed debug app) |
| `pnpm harmony:mobile-harmony:staging` | Debug rebuild + install (JS over Metro) |
| `pnpm harmony:mobile-harmony:staging:release` | Self-contained build + install (embedded JS bundle; live Metro takes over when running) |
| `pnpm harmony:mobile-harmony` | Same, local backend variant (`.env.development.local`) |

An iOS Metro on 8081 and a Harmony Metro on 8082 can run side by side; the device's `localhost:8081` is reverse-forwarded by the run script.

## Environment variables

`MULTICA_API_URL` / `MULTICA_WEB_URL` (`.env.staging` committed; copy `.env.example` for local-backend work and use your Mac's LAN IP, not `localhost`). Values are inlined into the JS bundle at build time — restart Metro after changing them.

## Architecture notes

- **Matrix:** RNOH 0.82-stable — RN 0.82.1 / React 19.2.0 / `@react-native-oh/react-native-harmony` 0.82.30 / CLI 0.82.33 / metro 0.83.5. The oh-tpl native packages are bridged through adapter classes in `PackageProvider.ets`; do not downgrade to match their 0.77-era builds (the 0.77 C++ renderer composes nothing on the API 26 emulator).
- **Native shell:** `harmony/` is committed source; hvigor regenerates autolinking artifacts inside it on every build.
- **Styling:** explicit `StyleSheet` styles driven by the shared theme tokens (`lib/theme.ts`, ported from the iOS app). NativeWind v4 proved the styling pipeline works on RNOH but its className path regressed on the 0.82 matrix; tokens are the stable baseline (roadmap: revisit NativeWind).
- **Navigation:** hand-rolled stack + bottom tabs + bottom sheet (`src/navigation/`) in pure RN Animated — no react-navigation/screens/gesture-handler. Route names mirror the iOS route tree; formSheet-style routes render as sheets.
- **Markdown:** pure-JS pipeline (`lib/markdown/`) — marked.lexer splits segments, in-house prose renderer + CodeBlock + MarkdownImage (no Shiki highlighting; see roadmap).
- **Platform modules:** `MulticaSecureStorage` (Asset Store Kit), `MulticaClipboard` (pasteboard), `MulticaHaptics` (vibrator) are app-owned ArkTS TurboModules in `harmony/entry/src/main/ets/multica/`, registered via the RNOH 0.82 contract in `PackageProvider.ets` **and** mirrored as CPP `ArkTSTurboModule` proxies in `cpp/PackageProvider.cpp` (both sides are required — see AGENTS.md), with JS-side graceful fallbacks in `lib/native-modules.ts`. The netinfo tpl module and the safe-area TurboModule are wired the same way (their JS **component** layers remain unused — `lib/safe-area.tsx` provides the insets API over the module's constants).
- **Known limitations:** third-party native *components* (SafeAreaView/SVG fabric components) are not wired — autolinking/codegen produce empty output for tpl packages on this matrix, which is why icons render as font glyphs and safe-area uses the module-constants shim.

## Roadmap

Landed: navigation (stack/tabs/bottom sheet), markdown rendering (pure-JS pipeline), secure token storage (Asset Store Kit), netinfo, icons (font glyphs), haptics, clipboard, media picking (`MulticaMediaPicker` TurboModule), the full data layer, the shared UI kit, and all screens (inbox / my-issues / issue detail + pickers / projects / pins / chat / search / settings).

Remaining:

1. Simulator pass over login-gated flows (needs a staging account; injected `uitest` taps don't reach RN `Pressable`, so interaction testing is manual).
2. Image-sequence lightbox (swipe between a message's images) — single-image viewer ships today.
3. Agent presence indicators (`use-agent-presence` port) — components already accept the props.
4. i18n, release signing, and AppGallery distribution.
