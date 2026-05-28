# Mobile App Rules (apps/mobile/)

For cross-app sharing rules, see the root `CLAUDE.md` Sharing Principles section.

## Current Stack

- Expo / React Native app using `expo-router` only as the `src/app` entry shell.
- Runtime UI is the legacy `src/` React Navigation stack:
  `src/app/index.tsx` -> `src/runtime/providers.tsx` -> `src/navigation/root-navigator.tsx`.
- Do not use the removed root-level `app/`, `components/`, `data/`, or `lib/` mobile stack.
- UI components for this stack live under `src/components/` and use `StyleSheet` plus `src/theme/tokens.ts`.
- API/auth/runtime wiring comes from `@multica/core` through `CoreProvider`; do not import the removed mobile-owned `data/api.ts` or `data/auth-store.ts` paths.

## Environment

- Runtime config is read by `app.config.js` into Expo `extra`.
- Use `EXPO_PUBLIC_API_BASE_URL` for HTTP and `EXPO_PUBLIC_WS_URL` for WebSocket.
- For local device testing, use a host the device can reach. Do not use `localhost` for a real phone.

## Mobile Sharing Rules

- Mobile may import shared types and pure helpers from `@multica/core`.
- Mobile screens may use `@multica/core` hooks/stores through the existing `CoreProvider` setup.
- Keep mobile UI implementation inside `src/`; do not reintroduce the root-level iOS v1 stack from `fd0fe1d0`.

## Validation

- Run `pnpm --filter @multica/mobile typecheck` after mobile changes.
- Prefer scoped validation; repo-wide checks may include unrelated baseline failures.
