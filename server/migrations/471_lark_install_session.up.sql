-- Durable state for the Feishu/Lark device-flow bind ("scan the QR") session.
--
-- The session used to live only in RegistrationService's in-process map, so
-- `GET /lark/install/{session_id}/status` could only be answered by the exact
-- backend process that served `begin`. Any other process — a second replica, or
-- the same one after a restart/deploy — returned 404, which the dialog renders
-- as "安装会话已失效或丢失，请重新扫码" about 5s after the QR appears (MUL-7340).
--
-- This table is the status projection the browser reads. The device_code stays
-- in memory on the process that owns the polling goroutine: it is a bearer
-- credential (anyone holding it can complete the authorization), and only the
-- owning process ever needs it. Consequence: if that process dies mid-flow the
-- row stays 'pending' until expires_at, which the status read reports as an
-- expired session instead of a phantom 404.
--
-- No foreign keys by project rule — workspace/agent/initiator integrity is an
-- application-layer concern, and a dropped workspace simply orphans rows that
-- the expiry sweep removes anyway.
CREATE TABLE lark_install_session (
    -- The opaque handle handed to the browser. Minted by randomSessionID()
    -- (24 random bytes, base64url) — not a UUID, so TEXT rather than UUID.
    id              TEXT PRIMARY KEY,
    workspace_id    UUID NOT NULL,
    agent_id        UUID NOT NULL,
    -- Who clicked "Bind". The status endpoint authorizes reads against this
    -- (session initiator, or a workspace owner/admin).
    initiator_id    UUID NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'success', 'error')),
    -- Set only when status = 'success'.
    installation_id UUID,
    -- Stable code the frontend switches on; free-form tail for diagnostics.
    error_reason    TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    -- Lark's device_code lifetime (expires_in, currently 1h). A row still
    -- 'pending' past this is reported as expired, whoever owns the goroutine.
    expires_at      TIMESTAMPTZ NOT NULL,
    -- When the row may be swept. NULL while pending; set to now()+SessionTTL
    -- once terminal, so the dialog can still read the final status after the
    -- polling goroutine has exited.
    gc_after        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
