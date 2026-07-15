# Multica mini apps

Mini apps are versioned internal tools that run inside Multica. Each app has a named owner, an immutable published version, approved Registry or Connection scopes, isolated backend execution, optional interactive views, and JSON workflows.

Use the catalog at **Apps** to create, preview, publish, roll back, disable, or delete an app. Publishing never expands access: an administrator must approve the exact scope ceiling separately. Every workflow run records its human principal and app version.

## Safety model

- Frontends run in an opaque sandboxed iframe.
- Each backend runs in its own resource-limited container and filesystem mount.
- Registry keys are short-lived, app-bound, person-bound, and minted per step.
- Connection credentials stay on the Multica server.
- User responses resume the exact waiting workflow request.
- Internal exceptions are logged server-side and masked in user responses.

Read [sdk.md](sdk.md) for app code, [manifest.md](manifest.md) for packaging, [workflows.md](workflows.md) for automation, and [when-to-build-a-mini-app.md](when-to-build-a-mini-app.md) before choosing this architecture.

