# Multica Mobile (iOS)

Expo + React Native iOS client for Multica. Independent from web/desktop — shares types and pure utilities from `@multica/core/`. See [`AGENTS.md`](./AGENTS.md) for mobile architecture and development rules; `package.json` records the current dependency versions.

## Just want to use it on your phone? (no development)

Multica isn't on the App Store yet — until that changes, anyone who wants it on their iPhone builds from source. One command:

```bash
pnpm ios:mobile:device:prod:release
```

This connects to the same backend as `multica.ai`, so your existing account just works. To use a private backend or your own bundle ID, copy `apps/mobile/.env.production.example` to `apps/mobile/.env.production.local` and edit the copy — it overrides the committed `.env.production` key by key and is gitignored, so personal values stay local.

**Prerequisites**: Mac with Xcode, a free Apple ID added under Xcode → Settings → Accounts, iPhone connected via USB with [Developer Mode enabled](https://docs.expo.dev/guides/ios-developer-mode/). Walk through Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) (pick **Development build → iOS Device**) if any of that is missing.

Xcode signs the build with the "Personal Team" your Apple ID automatically owns — created silently the first time you signed into Xcode, no setup needed. The first build downloads CocoaPods + compiles React Native from source — expect 10–20 minutes. Subsequent builds reuse Xcode's cache.

**If Xcode rejects signing with "No matching provisioning profiles found"** — rare, happens if someone has claimed the default bundle id `ai.multica.mobile` on Apple's developer portal. Pick any reverse-domain you own and re-run:

```bash
export EXPO_BUNDLE_IDENTIFIER_PROD=com.yourname.multica
pnpm ios:mobile:device:prod:release
```

**If your Apple ID belongs to more than one Apple Developer team** — a personal team plus an employer's, say — the build signs with the first identity it finds, which may not be the team you meant, and it keeps reusing that choice on every later build. Pin the right one (find the id in the Apple Developer Portal under Membership):

```bash
export EXPO_APPLE_TEAM_ID=ABCDE12345
pnpm ios:mobile:device:prod:release
```

**7-day signing limit**: a free Apple ID signs builds for 7 days. After that, plug back into the Mac and re-run the command to re-sign. An Apple Developer Program account ($99/yr) extends this to 1 year.

Everything below is for app developers — you can ignore the rest if you only wanted a personal install.

## Scripts

| Command | What it does | Backend |
|---|---|---|
| `pnpm dev:mobile` | Metro only (reuse existing install) | local (`.env.development.local`) |
| `pnpm dev:mobile:staging` | Metro only (reuse existing install) | staging (`.env.staging`) |
| `pnpm dev:mobile:prod` | Metro only (reuse existing install) | production (`.env.production`, overridden by `.env.production.local`) |
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

### Regenerating iOS after the scene lifecycle migration

The app uses Expo SDK 57's scene lifecycle support for builds linked against
the iOS 27 SDK. `app.config.ts` enables `ios.enableSceneSupport` through
`expo-build-properties`; Expo owns window creation and forwards scene lifecycle
and linking events. This requires Expo 57.0.23+ and expo-build-properties
57.0.20+; adding a scene manifest alone to an SDK 55 app is insufficient.
See [Expo's migration guidance](https://github.com/expo/fyi/blob/main/ios-scene-lifecycle.md).

SDK 57's scene backport does not include the later scene-aware presenter lookup
used by native modules such as `expo-image-picker`. The workspace applies
`apps/mobile/patches/expo-modules-core@57.0.21.patch`, a direct backport of Expo's
[#46956](https://github.com/expo/expo/pull/46956) and
[#48319](https://github.com/expo/expo/pull/48319), so avatar, chat, and issue
image pickers and document attachments resolve their presenting window from the
foregrounded scene. pnpm requires patched dependencies to be registered at the
workspace root, but the patch source stays with the mobile app so patch-only
changes remain in the mobile CI scope. Keep the patch until Expo backports the
change to SDK 57 or mobile moves to SDK 58+.

`eslint-config-expo` and TypeScript are temporarily excluded from
`expo install --check`: upgrading them enables additional compiler lint rules
that need a focused cleanup. [#9165](https://github.com/multica-ai/multica/issues/9165)
tracks removing the exclusions and aligning both tools with SDK 57.

After updating dependencies, regenerate an existing native project once (save
any local native customizations first; `ios/` is generated and ignored):

```bash
cd apps/mobile
APP_ENV=production pnpm exec expo prebuild --clean --platform ios
```

Use `development` or `staging` instead for those variants, then run the usual
build script. Keep normal development builds on the incremental prebuild path.
For native validation, test a Release cold launch to sign-in, cold and warm
`multica://` links, background/foreground transitions, and the Debug dev-client
in each variant. Also open the image picker from profile, chat, and the issue
editor, select an image, and confirm the upload completes. From the issue editor,
also attach a document and confirm its picker dismisses and the upload completes.
Unit tests do not replace this device validation.

## First-time setup

`.env.staging` is committed (public staging URL). `.env.development.local` is gitignored — copy the template once:

```bash
cp apps/mobile/.env.example apps/mobile/.env.development.local
# then edit EXPO_PUBLIC_API_URL inside it to your Mac's LAN IP, e.g. http://192.168.1.42:8080
```

If your Apple ID isn't on the Multica Apple Developer team yet, also set `EXPO_BUNDLE_IDENTIFIER_DEV` to a reverse-domain you own (e.g. `com.yourname.multica.dev`). For a personal production build, set `EXPO_BUNDLE_IDENTIFIER_PROD` in `.env.production.local`.

For staging builds, export `EXPO_BUNDLE_IDENTIFIER_STAGING=com.yourname.multica.staging` before running an `ios:*:staging` script. Each override applies only to its named variant, so development, staging, and production can stay installed side by side.

If your Apple ID belongs to more than one Apple Developer team, also set `EXPO_APPLE_TEAM_ID` to the team that should sign your builds. Unlike the bundle id overrides it applies to every variant, and it is re-applied on each run — so it also fixes a checkout that has already latched onto the wrong team.

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

Edit `EXPO_PUBLIC_API_URL` in `.env.staging`, `.env.production.local`, or `.env.development.local` (whichever variant you're running). Then:

- For an installed **Debug build**: restart Metro (`pnpm dev:mobile:staging`) so the next JS bundle picks up the new value.
- For an installed **Release build**: re-run the `ios:mobile:device:staging:release` command — the value is baked into the embedded bundle at build time.

For local backend testing, use your Mac's LAN IP (`ipconfig getifaddr en0`), not `localhost`.
