-- 9166_cerebro_purge_advisory_intake_rows: clean policy slate for the three
-- machine-intake capabilities whose workspace-layer row becomes a live
-- off-switch in FIR-4220 slice 2 (autopilot_webhook, daemon_runtime_callback,
-- gateway_channel_delivery). While these keys were advisory, legacy rows may
-- still exist from before the Settings write rejection landed. Once the intake
-- points start consulting the workspace layer, a stale stored Deny would
-- silently switch webhooks/daemon callbacks off on deploy — so the slate is
-- wiped first. Rows authored after this migration are intentional and read.
DELETE FROM cerebro_tool_policy
WHERE tool_key IN (
    'autopilot_webhook',
    'daemon_runtime_callback',
    'gateway_channel_delivery'
);
