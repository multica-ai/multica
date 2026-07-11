# Cerebro distribution

## Channel ownership

Cerebro clients update from the public `firtal-group/homebrew-tap` GitHub Releases channel. The private `firtal-group/firtal-cerebro` repository builds the artifacts; the release workflow mirrors CLI files and publishes Electron files to the public channel. Installed clients never require GitHub authentication.

Server and CLI release coordinates live in `server/internal/cerebro/forkdist`. Electron consumes the matching constants from `apps/desktop/src/main/cerebro-distribution.ts`. The Runtimes view asks the server for the latest version instead of calling GitHub from the browser.

## Runtime update flow

Every HTTP or WebSocket heartbeat compares a standalone runtime's reported CLI version with the cached latest Firtal release. An older runtime receives an update through the existing pending-update queue. Desktop-managed runtimes are excluded. Before a server-triggered update starts, the daemon acquires the same claim barrier used by periodic auto-update; active tasks and in-flight claims therefore defer the update instead of being interrupted.

The existing boot check and periodic update loop remain fallback paths.

## Desktop release security

CI release packaging uses platform signing when credentials are available. Until Firtal has those credentials, macOS and Windows installers are published unsigned; the build disables certificate auto-discovery and macOS notarization instead of failing the release. Configure these GitHub Actions secrets to enable signing later:

- macOS: `MACOS_CSC_LINK`, `MACOS_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`
- Windows: `WINDOWS_CSC_LINK`, `WINDOWS_CSC_KEY_PASSWORD`
- public release publishing: `HOMEBREW_TAP_GITHUB_TOKEN` with contents write access to `firtal-group/homebrew-tap`

With the secrets present, macOS uses Developer ID signing and Electron Builder notarization, and Windows uses the configured code-signing certificate. Without them, users may see the operating system's unknown-publisher warning. Linux packages do not require platform signing. Secrets are referenced only by name and are never stored in the repository.

Electron Builder uploads installers, blockmaps, and `latest*.yml` metadata to the public channel. Windows arm64 uses `latest-arm64.yml`; other platform metadata uses Electron Builder's standard names.

## Deployment model

Application deployment continues from `main` through Firtal's normal continuous deployment. Version tags create distributable CLI and desktop artifacts; they are not a production application deployment and do not use any Multica.ai release mechanism.

Run `bash scripts/cerebro/check-distribution-boundary.sh` locally. CI runs the same check and rejects upstream update coordinates.
