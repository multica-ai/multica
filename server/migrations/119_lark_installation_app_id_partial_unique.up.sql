-- Replace the unconditional UNIQUE(app_id) constraint with a partial
-- unique index that only enforces app_id uniqueness among ACTIVE rows.
-- With this in place, a revoked row no longer blocks a new installation
-- for the same app_id — the unbind→rebind path from #3950 works without
-- any Go changes. The revoke-first step in finishSuccess then meaningfully
-- covers the direct-rebind case (binding to a different agent without
-- explicitly unbinding first).
--
-- The dispatcher (GetActiveLarkInstallationByAppID) is updated to route
-- events only to rows with status = 'active', so a revoked row is never
-- used even if multiple rows per app_id exist.
ALTER TABLE lark_installation DROP CONSTRAINT lark_installation_app_id_key;
CREATE UNIQUE INDEX lark_installation_app_id_active_key
    ON lark_installation (app_id) WHERE status = 'active';
