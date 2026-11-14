# Multica for iOS

The iPhone and iPad client, built on `@multica/core`, the same logic the web and desktop apps run. It covers sign-in, the inbox, your issues, issue detail with comments and agent runs, conversations with agents, search, and switching workspaces. From the workspace menu you also reach projects, views, squads and autopilots, which you can create and edit on the phone, and per-workspace notification settings.

## Run it

You need macOS with Xcode, Node.js and pnpm. From the repository root:

```bash
pnpm install
pnpm ios:mobile                        # simulator, local backend (localhost:8080)
pnpm ios:mobile:prod                   # simulator, multica.ai
pnpm ios:mobile:device:prod:release    # your iPhone, multica.ai
```

`pnpm dev:mobile` starts only the bundler, for a build that is already installed.

## Preview screens without Xcode

```bash
pnpm --filter @multica/mobile preview:web
```

opens a browser gallery of the app's screens in iPhone-sized frames, rendered from the real components with sample data. Pick screens, theme and language with `?screens=inbox,issue,chat&scheme=dark&locale=en`.

## Configuration

| Variable | Purpose |
| --- | --- |
| `EXPO_PUBLIC_API_URL` | Backend URL. Default `http://localhost:8080`. |
| `EXPO_PUBLIC_WS_URL` | Realtime URL. Derived from the API URL by default. |
| `EXPO_PUBLIC_WEB_URL` | Web app URL that links open from. Default `http://localhost:3000`. |
| `EXPO_BUNDLE_IDENTIFIER_DEV` / `_STAGING` / `_PROD` | Bundle IDs to sign with an Apple ID outside the Multica team. |
| `EXPO_APPLE_TEAM_ID` | Signing team, when your Apple ID belongs to several. |

`.env.production` and `.env.staging` are committed. Put personal overrides in `.env.production.local`, which is not tracked.
