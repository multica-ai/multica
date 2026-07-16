# Multica mini apps

Mini apps are versioned internal tools that run as standalone surfaces inside Multica. Each app has a named owner, a Collection, an immutable published version, approved Registry or Connection scopes, isolated backend execution, and optional interactive views.

Use the data table at **Apps** to open or create an app. Open apps use the full content area, leaving only the normal desktop sidebar or mobile top bar around the app. App Collections and their inherited access grants are managed in **Settings → Collections**. Publishing never expands access: an administrator must approve the exact scope ceiling separately.

Creating an app opens the source editor with the complete starter package. Use **Import files** to replace the package, **Export package** to save a reviewable copy, **Preview** to inspect the frontend in a sandbox, and **Publish** to create an immutable semantic version with release notes. Published apps keep an **Edit app** action for the next version.

## Safety model

- Frontends run in an opaque sandboxed iframe.
- The app SDK crosses the sandbox through a host-validated message bridge; app code never receives the parent session.
- Each backend runs in its own resource-limited container and filesystem mount.
- A private health check must pass before a newly deployed version becomes current.
- Registry keys are short-lived, app-bound, person-bound, and minted per step.
- Connection credentials stay on the Multica server.
- User responses resume the exact waiting workflow request.
- Internal exceptions are logged server-side and masked in user responses.

Read [sdk.md](sdk.md) for app code, [manifest.md](manifest.md) for packaging, and [when-to-build-a-mini-app.md](when-to-build-a-mini-app.md) before choosing this architecture. Workflows are a separate product surface; [workflows.md](workflows.md) documents their backend integration contract, not an Apps editor.
