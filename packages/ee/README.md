## packages/ee

Commercial client features. Files here are covered
by [LICENSE](./LICENSE), not by the Multica License. The server half lives in
`server/ee`.

- **Dependency direction.** This package may import `@multica/core`, `@multica/ui`
  and `@multica/views`; those never import `@multica/ee`.
- **Attaching.** `packages/views/extensions` defines the slots open-source screens
  render. `registerEE()` fills them; the web and desktop apps call it once at
  startup. Without that call every screen renders as the open-source app.
- **Copy** lives in each feature's `locales/` and is added to the app's i18n
  instance at runtime.

| Feature |
| --- |
| `project-folders` (MUL-9390) |
