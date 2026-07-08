-- Per-user IANA timezone for *viewing* reports — the third leg of the
-- Operational / Scheduling / Viewing trio (see docs/timezone-architecture-rfc.md).
--
-- Distinct from:
--   * agent_runtime.timezone (Operational): where a runtime physically runs.
--     That column is dropped later in this migration set — the hour-of-day
--     activity heatmap now uses the viewer's tz instead.
--   * autopilot_trigger.timezone (Scheduling): the tz the user had in mind
--     when authoring a "run at 9am" rule.
--
-- This column answers: when this user looks at a dashboard, which calendar
-- day should "today" be? Two members of the same workspace sitting in
-- different regions each see their own "today" rendered from the same
-- underlying UTC data.
--
-- NULL means "fall back to the browser-detected tz at render time". New
-- users land on NULL, so there is no required onboarding step; the
-- frontend resolves it transparently. A user who explicitly picks an
-- IANA name in Preferences pins the value here so it survives across
-- devices and sessions.
--
-- This column never affects how data is materialised — all rollups stay
-- in UTC. It is read-only from the rollup pipeline's perspective and is
-- only consumed at query time to drive `DATE(bucket_hour AT TIME ZONE @tz)`.
ALTER TABLE "user"
    ADD COLUMN timezone TEXT NULL;

COMMENT ON COLUMN "user".timezone IS
    'User-preferred IANA timezone for report rendering (Viewing tz). '
    'NULL means "use the browser-detected tz at render time". Affects '
    'dashboards, charts, and any "today" label shown to this user. Does '
    'not affect data materialisation — all rollups remain in UTC.';
