-- Add a per-installation `region` so one Multica deployment can serve
-- BOTH mainland Feishu (open.feishu.cn / accounts.feishu.cn) and Lark
-- international (open.larksuite.com / accounts.larksuite.com) at the same
-- time. Before this column the open-platform host was a single
-- deployment-wide value (the MULTICA_LARK_HTTP_BASE_URL /
-- MULTICA_LARK_CALLBACK_BASE_URL env knobs, defaulting to open.feishu.cn),
-- so a given deployment could talk to only one cloud at a time.
--
-- The device-flow installer already auto-detects the tenant: Lark emits
-- user_info.tenant_brand="lark" mid-poll and RegistrationService swaps the
-- accounts host to accounts.larksuite.com. finishSuccess now persists that
-- detected region here, and every outbound REST + WebSocket call resolves
-- its open-platform host from this column via InstallationCredentials.Region.
--
-- NOT NULL DEFAULT 'feishu' is the safe backfill: every installation that
-- exists today was created against mainland Feishu (the only host the old
-- code reached without an env override), so 'feishu' is correct for all
-- pre-migration rows. The CHECK mirrors the lark.Region enum in
-- server/internal/integrations/lark/types.go — keep the two in lockstep.
ALTER TABLE lark_installation
    ADD COLUMN region TEXT NOT NULL DEFAULT 'feishu'
        CHECK (region IN ('feishu', 'lark'));
