# Multica Mobile (iOS + Android)

Expo + React Native mobile client for Multica. Independent from web/desktop — shares only types from `@multica/core/`. See [`CLAUDE.md`](./CLAUDE.md) for the locked tech-stack baseline and import rules.

Both platforms build from the same JS/TS source and the same `app.config.ts`. iOS is documented first (it shipped first); the [Android](#android) section below covers everything Android-specific.

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
| `pnpm dev:mobile:prod` | Metro only (reuse existing install) | production (`.env.production`) |
| `pnpm ios:mobile` | Full rebuild + install on **iOS Simulator**, Debug | local |
| `pnpm ios:mobile:staging` | Full rebuild + install on **iOS Simulator**, Debug | staging |
| `pnpm ios:mobile:prod` | Full rebuild + install on **iOS Simulator**, Debug | production |
| `pnpm ios:mobile:device` | Full rebuild + install on **USB iPhone**, Debug | local |
| `pnpm ios:mobile:device:staging` | Full rebuild + install on **USB iPhone**, Debug | staging |
| `pnpm ios:mobile:device:staging:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | staging |
| `pnpm ios:mobile:device:prod` | Full rebuild + install on **USB iPhone**, Debug | production |
| `pnpm ios:mobile:device:prod:release` | Full rebuild + install on **USB iPhone**, Release (standalone) | production |
| `pnpm android:mobile` | Full rebuild + install on **Android emulator/device**, Debug | local |
| `pnpm android:mobile:staging` | Full rebuild + install on **Android emulator/device**, Debug | staging |
| `pnpm android:mobile:prod` | Full rebuild + install on **Android emulator/device**, Debug | production |
| `pnpm android:mobile:prod:release` | Full rebuild + install, Release (standalone, signed) | production |

`dev:*` runs Metro only — assumes the matching variant is already installed. `ios:mobile*` / `android:mobile*` do a full native rebuild + install.

Bundle id and display name switch on `APP_ENV` (see `app.config.ts`), so Dev / Staging / Production variants can coexist on the same device or simulator.

## First-time setup

`.env.staging` is committed (public staging URL). `.env.development.local` is gitignored — copy the template once:

```bash
cp apps/mobile/.env.example apps/mobile/.env.development.local
# then edit EXPO_PUBLIC_API_URL inside it to your Mac's LAN IP, e.g. http://192.168.1.42:8080
```

If your Apple ID isn't on the Multica Apple Developer team yet, also uncomment and set `EXPO_BUNDLE_IDENTIFIER_DEV` to a reverse-domain you own (e.g. `com.yourname.multica.dev`). This **only** overrides the dev variant — staging / production bundle ids are intentionally not overridable so variants can coexist.

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

## Android

Builds from the same source as iOS — no separate codebase, no separate config. Unlike iOS it runs on Linux/Windows too, and Android signing has no 7-day expiry: a self-signed key you generate once is valid for as long as you set it to be.

### Prerequisites

| What | Why / where |
|---|---|
| **JDK 17** | AGP 8.x requires it. Older JDKs fail deep inside Gradle with errors that don't name the JVM as the cause. Set `JAVA_HOME` explicitly — do not rely on the system default. |
| **Android SDK** | Platform 36 + Build-Tools 36.0.0 + NDK 27.1. Install via Android Studio's SDK Manager or `sdkmanager`. Point `ANDROID_HOME` at it (commonly `~/Android/Sdk`). |
| **An emulator or a USB device** | `adb devices` must list exactly one. USB devices need USB debugging enabled in Developer options. |

```bash
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk   # adjust to your JDK 17 path
export ANDROID_HOME="$HOME/Android/Sdk"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$PATH"
```

Expo's [Set up your environment](https://docs.expo.dev/get-started/set-up-your-environment/) (pick **Development build → Android**) covers the SDK install in more depth.

### Day-to-day development

```bash
pnpm android:mobile:staging
```

Debug build with `expo-dev-launcher` embedded — same hot-reload flow as iOS, same `dev:mobile:staging` Metro loop afterward. Works against an emulator or a USB device interchangeably.

For a local backend from the **emulator**, `localhost` points at the emulator itself, not your machine — use `http://10.0.2.2:8080` in `.env.development.local`. From a **USB device**, use your machine's LAN IP.

### Standalone release build

```bash
pnpm android:mobile:prod:release
```

Produces a signed Release APK at `apps/mobile/android/app/build/outputs/apk/release/app-release.apk` — no dev-launcher, no Metro probe, installable and runnable on its own. Requires the signing setup below; without it the build still succeeds but signs with Android's debug key (see [Signing](#signing)).

The first release build compiles React Native and every native module from source across four ABIs — **expect 60–90 minutes** on a typical machine, and roughly 106 MiB of APK. Later builds reuse Gradle's cache and are far quicker. If Gradle's own distribution download crawls (it's the first thing `gradlew` does, before any of your code builds), a regional mirror in `android/gradle/wrapper/gradle-wrapper.properties` fixes it — but that file is prebuild output, so treat the change as local and temporary rather than something to commit.

To cut build time while iterating, restrict the ABIs to the ones you actually run:

```bash
pnpm android:mobile:prod:release --  -PreactNativeArchitectures=x86_64   # emulator only
```

**Whatever you pass must include the ABI of the device you're installing onto.** A mismatch is not caught at install time — `adb install` still prints `Success`, and the app dies on first launch with `SoLoaderDSONotFoundError: couldn't find DSO to load: libreactnative.so`. Check the target's ABI first:

```bash
adb shell getprop ro.product.cpu.abilist
```

A `x86_64,arm64-v8a` emulator is a specific trap: it *claims* arm64 support and will happily install an arm64-only APK (`dumpsys package` even reports `primaryCpuAbi=arm64-v8a`), yet the arm64 `.so` files never become loadable and the app can't start. Build with `x86_64` in the list for that emulator; use `arm64-v8a` for essentially any physical phone.

Don't diagnose this by looking for the app's `lib/` directory on device. These APKs ship `extractNativeLibs=false`, so `.so` files are mapped straight out of the APK and `/data/app/*/<pkg>-*/lib/` is **absent even when everything works** — verified on a healthy two-ABI install. The only reliable check is matching `aapt2 dump badging | grep native-code` against `adb shell getprop ro.product.cpu.abilist`.

Never trim ABIs by deleting `lib/<abi>/` out of a built APK — the APK becomes unstartable in exactly the same way, and it invalidates the signature. Pass `-PreactNativeArchitectures` and let Gradle build the variant you want.

Full four-ABI APKs run about 106 MiB (111165136 bytes); a two-ABI (`arm64-v8a,x86_64`) build is closer to 68 MiB (71380570 bytes). If something downstream imposes a file-size cap, trim ABIs at build time rather than post-processing the APK — and note in the hand-off which ABIs the artifact actually carries, since a trimmed build is not what you'd distribute.

### Signing

Android release builds must be signed with a key you control. The upstream React Native template signs `release` with the **debug** key — fine for a smoke test, unacceptable for anything distributed: the debug key is public, identical on every machine, and an APK signed with it can be impersonated by anyone.

`plugins/with-android-release-signing.js` (an Expo config plugin, wired up in `app.config.ts`) rewrites the generated `android/app/build.gradle` on every prebuild so `release` uses a real keystore. It has to be a plugin rather than a hand-edit: `android/` is prebuild output and gitignored, so any manual change there is erased by the next `expo prebuild` or lost on a fresh clone.

The plugin reads four Gradle properties and **nothing is stored in the repo**:

| Property | Meaning |
|---|---|
| `MULTICA_RELEASE_STORE_FILE` | Absolute path to the keystore |
| `MULTICA_RELEASE_KEY_ALIAS` | Key alias inside it |
| `MULTICA_RELEASE_STORE_PASSWORD` | Keystore password |
| `MULTICA_RELEASE_KEY_PASSWORD` | Key password |

If `MULTICA_RELEASE_STORE_FILE` is absent the config copies the debug keystore instead (`initWith signingConfigs.debug`), so a contributor who just wants a runnable APK isn't blocked on generating a keystore. That explicit fallback matters: `release` is unconditionally pointed at `signingConfigs.release`, so leaving the config empty would yield an *unsigned* release APK that `adb install` refuses — not a debug-signed one. Check which key an APK actually carries with `apksigner verify --print-certs`; the debug key's DN is `CN=Android Debug`, and a debug-signed release also comes out `v3 scheme: false` since the v2/v3 flags live in the credentialed branch.

#### Verify the signing config

```bash
pnpm android:mobile:verify-signing
```

Probes both branches through AGP's variant API and asserts each resolves to the keystore it should — the credentialed one to your release keystore, the uncredentialed one to `android/app/debug.keystore`. Run it after touching the plugin or the Gradle properties.

This lives outside `vitest` on purpose. The unit tests can only assert on the `build.gradle` text the plugin emits; they cannot see what Gradle makes of it. That gap is not hypothetical — an earlier version of this config emitted text that *read* like a debug fallback but resolved to an unsigned release variant, and the string-level tests passed the whole way. The probe needs an Android SDK and about a minute, so it stays off the default test path rather than slowing every run.

#### Generate a keystore

```bash
keytool -genkeypair -v \
  -keystore ~/.android/keystores/multica-release.keystore \
  -alias multica-release \
  -keyalg RSA -keysize 4096 -validity 10000 \
  -dname "CN=Multica, OU=Mobile, O=Multica, L=<city>, ST=<state>, C=<country>"
```

`keytool` prompts for the password. Use a generated one, not a memorable one — it never needs typing again after the next step.

#### Supply the credentials

Put them in `~/.gradle/gradle.properties` (outside the repo, `chmod 600`):

```properties
MULTICA_RELEASE_STORE_FILE=/home/you/.android/keystores/multica-release.keystore
MULTICA_RELEASE_KEY_ALIAS=multica-release
MULTICA_RELEASE_STORE_PASSWORD=…
MULTICA_RELEASE_KEY_PASSWORD=…
```

On CI, pass them as `ORG_GRADLE_PROJECT_MULTICA_RELEASE_*` environment variables from the secret store instead — same property names, no file on disk.

**Never commit the keystore or its passwords.** `apps/mobile/.gitignore` ignores `android/` wholesale, which covers a keystore placed there, but the safe habit is to keep it outside the repo entirely.

#### Back up the keystore

Losing it means losing the ability to ship an update that existing installs will accept — Android refuses to install an update signed by a different key, and the user's only path is uninstall + reinstall, which wipes app data. Store the keystore file and its passwords in whatever the team uses for secrets (password manager, encrypted vault) the moment you generate it, not later.

Builds are signed with **APK Signature Scheme v2 + v3**; v1 (JAR signing) is off because minSdk 24 makes it redundant. v3 is what carries proof-of-rotation, and it cannot be added to an already-published APK — which is why it's on from the first release rather than deferred to whenever a key rotation actually comes up.

### Package names

Same per-variant scheme as iOS, so dev / staging / production coexist on one device:

| Variant | Package | Label |
|---|---|---|
| development | `ai.multica.mobile.dev` | Multica (Dev) |
| staging | `ai.multica.mobile.staging` | Multica (Staging) |
| production | `ai.multica.mobile` | Multica |

Building a personal copy under a namespace you own? Override with `EXPO_ANDROID_PACKAGE_DEV` / `EXPO_ANDROID_PACKAGE_PROD`. Unlike iOS there's no provisioning profile forcing your hand — the override exists for parity and for anyone publishing their own fork.

## Pointing at a different backend

Edit `EXPO_PUBLIC_API_URL` in `.env.staging`, `.env.production`, or `.env.development.local` (whichever variant you're running). Then:

- For an installed **Debug build**: restart Metro (`pnpm dev:mobile:staging`) so the next JS bundle picks up the new value.
- For an installed **Release build**: re-run the corresponding `*:release` command (`ios:mobile:device:staging:release` / `android:mobile:prod:release`) — the value is baked into the embedded bundle at build time.

For local backend testing, `localhost` won't reach your machine from the phone or emulator:

- **iOS device / simulator, Android USB device** — use your machine's LAN IP (`ipconfig getifaddr en0` on macOS, `hostname -I` on Linux).
- **Android emulator** — use `http://10.0.2.2:<port>`, the emulator's alias for the host loopback.
