# @multica/cerebro-profile

Cerebro user-communication-profile feature (JEH-304). Owns:

- `core/` — schema, presets, compile (token-budget aware), tokens estimator, react-query hooks (queries + mutations).
- `views/` — `AgentProfileTab` settings UI.

Lives in cerebro-zone so upstream-sync conflicts only when upstream
introduces a competing concept. The API methods (`getMyProfile`,
`upsertMyProfile`, `deleteMyProfile`) and the `UserProfileResponse`
type stay in `@multica/core/api` + `@multica/core/types` because the
api-client itself is upstream-zone-with-CEREBRO-PATCH.
