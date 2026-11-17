# Android builds

The Android client shares the React Native screens, API client and realtime
subscriptions with iOS. It connects to the public production backend by default.
It does not run agents on the phone or register for background push notifications.
Foreground updates resume by reconnecting and refreshing caches after returning
from the background.

## Prerequisites

- Node.js 22+ and the repository's pinned pnpm (`corepack pnpm --version`).
- JDK 17, with `JAVA_HOME` set.
- Android SDK, with `ANDROID_HOME` set: platform 36, build-tools 36.0.0,
  NDK 27.1.12297006 and CMake 3.22.1.
- Linux or macOS, `unzip`, and disk space for native dependencies.
- A release keystore, alias and password file stored outside this repository.

Install JS dependencies from the repository root:

```bash
pnpm install --frozen-lockfile
```

For Debug development, configure `.env.development.local` inside `apps/mobile`
using `.env.example`, then run `pnpm -C apps/mobile android`.
Release builds regenerate the ignored `android/` directory before Gradle.
Keep native changes in `app.config.ts` and config plugins.
The Android plugin keeps fbjni aligned with React Native's C++ runtime; Shiki's
dynamic native dependency must not upgrade that ABI independently.

## Signing

Use the existing authorized release key when available. For a personal install
without one, create a dedicated key outside Git. On Linux/macOS:

```bash
umask 077
mkdir -p "$HOME/.local/share/multica/android-signing"
chmod 700 "$HOME/.local/share/multica/android-signing"
openssl rand -base64 48 > "$HOME/.local/share/multica/android-signing/password"
keytool -genkeypair -storetype PKCS12 -keyalg RSA -keysize 4096 -validity 10000 \
  -alias multica-release -dname "CN=Multica Local Release" \
  -keystore "$HOME/.local/share/multica/android-signing/release.p12" \
  -storepass:file "$HOME/.local/share/multica/android-signing/password"
```

Run key generation only once. Keep the key and password in protected persistent
storage, with an encrypted backup under the signing owner's control. Losing the
key prevents updates over this application ID. A personal key is not the
publisher's official signing identity. Never upload keys or passwords to Git,
issue attachments or logs.

Set paths in your shell; these variables contain paths, not passwords:

```bash
export MULTICA_ANDROID_KEYSTORE="$HOME/.local/share/multica/android-signing/release.p12"
export MULTICA_ANDROID_STORE_PASSWORD_FILE="$HOME/.local/share/multica/android-signing/password"
export MULTICA_ANDROID_KEY_ALIAS=multica-release
# If the key password differs:
# export MULTICA_ANDROID_KEY_PASSWORD_FILE=/protected/path/key-password
pnpm -C apps/mobile android:release
```

The config plugin disables Expo's debug signing for Release. The build script
aligns the unsigned APK for 16 KB pages, signs it with `apksigner`, verifies
signature/alignment, rejects debug certificates and debuggable apps, and checks
that the JS bundle is embedded. Metro is unnecessary for this APK.

The APK and JSON/SHA-256/signature sidecars are in
`apps/mobile/dist/android/production/`. JSON records the source commit/dirty flag,
endpoints, app ID, version, minimum/target SDK, ABIs, SHA-256 and certificate digest.
Build from a clean committed checkout for a handoff.

## Variants and private backends

| Command (inside `apps/mobile`) | Environment files, in precedence order | Default application ID |
| --- | --- | --- |
| `pnpm android:release` | `.env.production.local`, `.env.production` | `ai.multica.mobile` |
| `pnpm android:release:staging` | `.env.staging` | `ai.multica.mobile.staging` |
| `pnpm android:release:dev` | `.env.development.local` | `ai.multica.mobile.dev` |

Exported variables take precedence over files. The release script disables Expo's
automatic dotenv loading so staging cannot acquire production endpoints during
Gradle bundling. Minimum Android version: 7.0 (API 24). Default ABIs: `arm64-v8a`
and `x86_64`. Override `MULTICA_ANDROID_ABIS` for other React Native architectures.

For self-hosting, open **Server settings** on the login page or in Settings.
Save API URL and Web URL independently; no rebuild is required. Both persist
across restarts. **Restore Cloud defaults**, then Save, returns to Cloud.
Custom ports and proxy prefixes are retained (API `https://host:8443/backend`,
Web `https://host:8443/app`). The API field is a backend root: a trailing `/api`
is removed because request paths add it. Auth and WS use `/auth` and `/ws` under
the same root. The distributed APK requires HTTPS and device-trusted certificates;
it never bypasses TLS. Changing either URL clears the old session, workspace,
query caches and drafts before the new instance can receive requests.

Environment variables only choose the initial install defaults; saved runtime
settings take precedence. `localhost` refers to the phone. Test infrastructure
must provide a device-reachable HTTPS endpoint; do not enable global cleartext
or disable TLS verification to reach an isolated HTTP-only backend.

Application ID overrides: `EXPO_ANDROID_PACKAGE_PROD`,
`EXPO_ANDROID_PACKAGE_STAGING`, `EXPO_ANDROID_PACKAGE_DEV`.
Increase `EXPO_ANDROID_VERSION_CODE` for each distributed update. Keep the
application ID and signing key stable:

```bash
EXPO_ANDROID_VERSION_CODE=8 pnpm -C apps/mobile android:release
adb install -r apps/mobile/dist/android/production/multica-0.1.6-9.apk
```

## Verification

Run the mobile checks in `AGENTS.md`, then build Release. Menus use `useActionSheet`
(native iOS, scrollable Android menu with Back/cancel). Icons retain SF Symbols
on iOS and use Ionicons on Android. Android pickers use full-height modal routes
with native close controls. OS photo/document pickers supply attachments without
broad media access; camera and microphone are disabled.
Android forms use keyboard-controller frame events to avoid covering focused
inputs in edge-to-edge windows; authentication forms also scroll on small screens.

Record the exact APK hash, commit, device, system version and network for:

- Install/cold start without Metro, login and session restoration.
- Workspace switch, task create/edit, comments/replies/reactions and attachments.
- Chat, inbox, run records, links, long-press, Back, keyboard and safe areas.
- Offline/online reconnect and background/foreground cache recovery.
- Long chat/task records with Markdown, code, images and arriving messages:
  scrolling, typing, navigation and reading-position preservation.

Use synthetic data or an explicitly authorized account. Compare with the browser
on the same physical phone/network/data when available. Record times, frame
traces and memory where measured; emulator timings do not establish phone
performance. List untested scenarios explicitly. Unit checks cannot prove native
rendering or account flows.
