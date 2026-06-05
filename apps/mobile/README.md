# Multica Mobile (iOS)

Expo + React Native iOS client for Multica. Independent from web/desktop — shares only types from `@multica/core/`. See [`CLAUDE.md`](./CLAUDE.md) for the locked tech-stack baseline and import rules.

## Just want to use it on your phone? (no development)

Multica isn't on the App Store yet — until that changes, anyone who wants it on their iPhone builds from source. One command:

```bash
pnpm ios:mobile:device:prod:release
```

This connects to the same backend as `multica.ai`, so your existing account just works.

**Prerequisites**: Mac with Xcode, a free Apple ID added under Xcode → Settings → Accounts, iPhone connected via USB with [Developer Mode enabled](https://docs.expo.dev/guides/ios-developer-mode/). Walk through Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) (pick **Development build → iOS Device**) if any of that is missing.

Xcode signs the build with the "Personal Team" your Apple ID automatically owns — created silently the first time you signed into Xcode, no setup needed. The first build downloads CocoaPods + compiles React Native from source — expect 10–20 minutes. Subsequent builds reuse Xcode's cache.

**If Xcode rejects signing with "No matching provisioning profiles found"** — rare, happens if someone has claimed the default bundle id `ai.multica.mobile` on Apple's developer portal. Pick any reverse-domain you own and re-run:

```bash
export EXPO_BUNDLE_IDENTIFIER_PROD=com.yourname.multica
pnpm ios:mobile:device:prod:release
```

**7-day signing limit**: a free Apple ID signs builds for 7 days. After that, plug back into the Mac and re-run the command to re-sign. An Apple Developer Program account ($99/yr) extends this to 1 year.

Everything below is for app developers — you can ignore the rest if you only wanted a personal install.

## Scripts

| Command | What it does | Backend |
|---|---|---|
| `pnpm dev:mobile` | Metro only (reuse existing install) | local (`.env.development.local`) |
| `pnpm dev:mobile:staging` | Metro only (reuse existing install) | staging (`.env.staging`) |
| `pnpm dev:mobile:prod` | Metro only (reuse existing install) | production (`.env.production`) |
| `pnpm ios:mobile` | Full rebuild + install on **iOS Simulator**, Debug | local |
| `pnpm ios:mobile:staging` | Full rebuild + install on **iOS Simulator**, Debug | staging |
| `pnpm ios:mobile:prod` | Full rebuild + install on **iOS Simulator**, Debug | production |
| `pnpm ios:mobile:device` | Full rebuild + install on **USB iPhone**, Debug | local |
| `pnpm ios:mobile:device:staging` | Full rebuild + install on **USB iPhone**, Debug | staging |
| `pnpm ios:mobile:device:staging:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | staging |
| `pnpm ios:mobile:device:prod` | Full rebuild + install on **USB iPhone**, Debug | production |
| `pnpm ios:mobile:device:prod:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | production |

`dev:*` runs Metro only — assumes the matching variant is already installed. `ios:mobile*` does a full native rebuild + install.

Bundle id and display name switch on `APP_ENV` (see `app.config.ts`), so Dev / Staging / Production variants can coexist on the same device or simulator.

## First-time setup

`.env.staging` is committed (public staging URL). `.env.development.local` is gitignored — copy the template once:

```bash
cp apps/mobile/.env.example apps/mobile/.env.development.local
# then edit EXPO_PUBLIC_API_URL inside it to your Mac's LAN IP, e.g. http://192.168.1.42:8080
```

If your Apple ID isn't on the Multica Apple Developer team yet, also uncomment and set `EXPO_BUNDLE_IDENTIFIER_DEV` to a reverse-domain you own (e.g. `com.yourname.multica.dev`). This **only** overrides the dev variant — staging / production bundle ids are intentionally not overridable so variants can coexist.

## Build it onto your iPhone

Two paths, depending on what you want to do:

### Day-to-day development (Mac in front of you)

```bash
pnpm ios:mobile:device:staging
```

Produces a **Debug build** with `expo-dev-launcher` embedded. Every launch the app probes Metro on your Mac and pulls fresh JS — perfect for hot-reload, painful when the Mac is asleep or you're on a different WiFi.

### Standalone / "just use it" (walk away from the Mac)

```bash
pnpm ios:mobile:device:staging:release
```

Produces a **Release build**. No `expo-dev-launcher`, no Metro probe, no "Downloading…" screen. Splash → app, exactly like an App Store install. Trade-off: every JS change requires re-running this command.

Both paths share the same prerequisites: Mac with Xcode, free Apple ID added under Xcode → Settings → Accounts, iPhone connected via USB with Developer Mode enabled. Follow Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) — pick **Development build → iOS Device** — if any of that is missing.

First build of either variant downloads CocoaPods + compiles React Native from source — expect 10-20 minutes. Subsequent builds reuse Xcode's DerivedData cache.

## Try it in the iOS Simulator (no iPhone needed)

```bash
pnpm ios:mobile:staging
```

Boots the simulator, builds, installs the dev-client. Faster to iterate than a device build because no signing / provisioning step. Same `dev:mobile:staging` Metro flow afterward.

## 7-day signing limit (device only)

A free Apple ID signs builds for **7 days only**, Debug and Release both. After that the app refuses to launch on the iPhone. Plug back into the Mac and re-run the corresponding `ios:mobile:device*` script to re-sign. Simulator builds are unaffected. The only workaround for the device limit is an Apple Developer Program account ($99/yr), which extends to 1 year.

## Pointing at a different backend

`EXPO_PUBLIC_API_URL` is now only the default backend bundled into the app. On the login screen, open **Server → Change server**, enter a Multica backend origin, and save it. The app probes `/api/config`, persists the URL in SecureStore, clears the local session, and uses that server for HTTP, file upload, WebSocket, login, workspace, chat, and issue requests after app restart.

Accepted examples:

- `https://api.example.com`
- `http://192.168.1.42:8080`

Use a backend origin only: no `/api`, workspace path, query string, or trailing route. For local backend testing from a phone or simulator on another device, use your Mac's LAN IP (`ipconfig getifaddr en0`), not `localhost`. The phone and server must be on the same network unless the backend is reachable through a public hostname or VPN.

Tap **Use default** on the login screen to clear the custom backend and return to the build's bundled `EXPO_PUBLIC_API_URL`. This avoids getting stuck if a self-host URL is mistyped or no longer reachable.

For self-hosted Multica, start the backend first, confirm `http://<host>:8080/health` returns JSON, then configure the mobile app with `http://<host>:8080`. If the backend exposes `daemon_app_url` from `/api/config`, mobile web links use that runtime app URL; otherwise they fall back to the build-time `EXPO_PUBLIC_WEB_URL`.

Changing `.env.*` still changes the default URL for a newly built bundle, but it is no longer required just to connect an installed app to a self-hosted backend.

## Crash reporting

Crash reporting uses `@sentry/react-native` and is opt-in per environment.
Local development stays quiet unless you explicitly enable it:

```bash
EXPO_PUBLIC_SENTRY_ENABLED=true
EXPO_PUBLIC_SENTRY_DSN=https://public-key@o000000.ingest.sentry.io/0000000
```

Use a staging DSN for TestFlight/device validation and a production DSN only
for production builds. `APP_ENV` is sent as the Sentry environment, and the
app records release/version, platform, redacted route, and a hashed workspace
slug. Do not include issue/comment text, attachment contents, audio, or
transcripts in manual crash contexts.

Source maps and native debug symbols are uploaded during EAS/native builds
when these build-time variables are present:

```bash
SENTRY_ORG=your-org
SENTRY_PROJECT=multica-mobile-staging
SENTRY_AUTH_TOKEN=sntrys_...
```

`SENTRY_AUTH_TOKEN` must live in your shell or EAS secrets, never in git. For
Expo export/EAS Update bundles, run the SDK upload helper after export:

```bash
npx sentry-expo-upload-sourcemaps dist
```

## Voice backend behavior

The iOS client records short audio locally and sends it only to the Multica backend speech proxy. It does not receive ASR/TTS provider keys. If the backend has speech disabled or the provider is unavailable, speech calls return typed recoverable errors such as `provider_missing`, `rate_limited`, `quota_exceeded`, or `provider_timeout`; the app should let the user continue by typing the message.

## Push notifications

The iOS app uses `expo-notifications` to request APNs-backed Expo push permission after a signed-in user has an active workspace. Device tokens are registered against the current user + workspace through `/api/mobile/push-tokens`; sign-out unregisters the current token before clearing auth.

Server delivery is driven by existing inbox events and notification preferences. `system_notifications=muted` disables lock-screen delivery while keeping in-app inbox/realtime behavior intact. Without `EXPO_ACCESS_TOKEN`, the backend records deliveries as dry-run `skipped` rows and does not call Expo. Set `EXPO_ACCESS_TOKEN` for real delivery; `MULTICA_EXPO_PUSH_URL` can override the Expo endpoint for tests, and `MULTICA_PUSH_DRY_RUN=true` forces dry-run.

Manual iOS validation still requires a signed device build: allow/deny permission, receive a background notification for assignment/mention/comment/agent completion/failure/block, tap it, and confirm the app routes to the target workspace issue. If a workspace is unavailable, the app falls back to workspace selection.
