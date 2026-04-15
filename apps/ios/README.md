# Multica iOS

This package is the native iOS app for Multica, built with Expo Router and
React Native.

## Prerequisites

- macOS with Xcode installed.
- Node.js 22.
- pnpm.
- An iPhone simulator from Xcode, or a physical iPhone connected by USB.
- For physical device installs, sign in to Xcode with an Apple ID and trust the
  developer profile on the iPhone after the first install.

From the repository root, install dependencies:

```bash
pnpm install
```

## Environment

The iOS app reads these variables:

```bash
EXPO_PUBLIC_API_URL=http://localhost:8080
EXPO_PUBLIC_WS_URL=ws://localhost:8080/ws
EXPO_PUBLIC_APP_URL=http://localhost:3000
```

`apps/ios/scripts/with-env.sh` loads `MULTICA_ENV_FILE` first, then falls back to
the repository `.env.worktree` or `.env`. If `EXPO_PUBLIC_*` values are missing,
it derives them from `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_WS_URL`, and
`MULTICA_APP_URL` where possible.

For a deployed environment, create a local env file at the repository root:

```bash
cat > .env.ios.production <<'EOF'
EXPO_PUBLIC_API_URL=https://multica-api.copilothub.ai
EXPO_PUBLIC_WS_URL=wss://multica-api.copilothub.ai/ws
EXPO_PUBLIC_APP_URL=https://multica-app.copilothub.ai
EOF
```

Do not commit local env files.

## Local Development

Start the Multica backend from the repository root:

```bash
make dev
```

In another terminal, run the iOS app:

```bash
cd apps/ios
pnpm ios
```

This opens the iOS simulator and starts Metro. The simulator can reach the Mac
host through `localhost`, so the default local URLs work for simulator testing.

To start only Metro:

```bash
cd apps/ios
pnpm start
```

## Physical iPhone Development

For a physical iPhone using a local backend, `localhost` points to the phone, not
the Mac. Use the Mac LAN IP address:

```bash
ipconfig getifaddr en0
```

Create a local env file at the repository root:

```bash
cat > .env.ios.device <<'EOF'
EXPO_PUBLIC_API_URL=http://YOUR_MAC_LAN_IP:8080
EXPO_PUBLIC_WS_URL=ws://YOUR_MAC_LAN_IP:8080/ws
EXPO_PUBLIC_APP_URL=http://YOUR_MAC_LAN_IP:3000
EOF
```

Install a development build on the connected iPhone:

```bash
cd apps/ios
MULTICA_ENV_FILE=../../.env.ios.device \
  scripts/with-env.sh pnpm exec expo run:ios --device "Your iPhone Name"
```

Keep the terminal open while using the development build; it depends on Metro.

## Install A Standalone Release Build

Use this when you want an app that opens directly on your iPhone without a Metro
dev server. The app still talks to the configured Multica API and WebSocket
server.

```bash
cd apps/ios
MULTICA_ENV_FILE=../../.env.ios.production \
  scripts/with-env.sh pnpm exec expo run:ios \
  --device "Your iPhone Name" \
  --configuration Release
```

If iOS refuses to open the app after install, trust the developer profile:

1. Open Settings on the iPhone.
2. Go to General -> VPN & Device Management.
3. Select the developer profile for your Apple ID.
4. Tap Trust.

Then launch Multica from the home screen.

## Useful Checks

From the repository root:

```bash
pnpm --filter @multica/core typecheck
pnpm --filter @multica/core exec vitest run api/client.test.ts utils.test.ts
```

From `apps/ios`:

```bash
scripts/with-env.sh pnpm exec expo export --platform ios --output-dir .context/ios-export-check
```

## Troubleshooting

- `Failed to create a new MMKV instance`: rebuild the native app with
  `newArchEnabled` enabled. The current `app.json` enables it.
- `Incompatible React versions` or invalid hook calls: run `pnpm install` from
  the repository root so the workspace uses the locked React and React Native
  versions.
- Login works in simulator but not on iPhone: replace localhost URLs with the
  Mac LAN IP, or use the deployed production URLs.
- WebSocket connects locally but not against deployment: check
  `EXPO_PUBLIC_WS_URL`; deployed HTTPS APIs should normally use a `wss://` URL.
- Release install succeeds but launch fails with a trust error: trust the Apple
  developer profile in iOS Settings.
