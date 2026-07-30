-- CEREBRO-PATCH(company-brain-rollback-tombstones): FIR-3924 preserves the
-- non-secret identity graph needed to restore legacy Company Brain routes
-- during a bounded rollback window. This migration creates schema only: it
-- neither snapshots nor changes Connections, permissions, approvals, audits,
-- aliases, credentials, agents, or cutover state.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'workspace_connection_workspace_id_id_name_unique'
    ) THEN
        ALTER TABLE workspace_connection
            ADD CONSTRAINT workspace_connection_workspace_id_id_name_unique
            UNIQUE (workspace_id, id, name);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_tool_policy_workspace_id_id_unique'
    ) THEN
        ALTER TABLE cerebro_tool_policy
            ADD CONSTRAINT cerebro_tool_policy_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_tool_policy_workspace_identity_unique'
    ) THEN
        ALTER TABLE cerebro_tool_policy
            ADD CONSTRAINT cerebro_tool_policy_workspace_identity_unique
            UNIQUE (
                workspace_id, id, tool_key, layer, subject_id, resource_pattern
            );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_approval_request_workspace_id_id_unique'
    ) THEN
        ALTER TABLE cerebro_approval_request
            ADD CONSTRAINT cerebro_approval_request_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_approval_request_workspace_identity_unique'
    ) THEN
        ALTER TABLE cerebro_approval_request
            ADD CONSTRAINT cerebro_approval_request_workspace_identity_unique
            UNIQUE (workspace_id, id, capability, resource);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_approval_audit_workspace_approval_id_unique'
    ) THEN
        ALTER TABLE cerebro_approval_audit
            ADD CONSTRAINT cerebro_approval_audit_workspace_approval_id_unique
            UNIQUE (workspace_id, approval_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_tool_policy_audit_workspace_id_id_unique'
    ) THEN
        ALTER TABLE cerebro_tool_policy_audit
            ADD CONSTRAINT cerebro_tool_policy_audit_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_tool_policy_audit_workspace_identity_unique'
    ) THEN
        ALTER TABLE cerebro_tool_policy_audit
            ADD CONSTRAINT cerebro_tool_policy_audit_workspace_identity_unique
            UNIQUE (
                workspace_id, id, tool_key, layer, subject_id, resource_pattern
            );
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_capability_alias_id_capability_unique'
    ) THEN
        ALTER TABLE cerebro_capability_alias
            ADD CONSTRAINT cerebro_capability_alias_id_capability_unique
            UNIQUE (id, capability_id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS cerebro_company_brain_rollback_window (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id                UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    company_brain_connection_id UUID        NOT NULL,
    snapshot_sha256             TEXT        NOT NULL,
    starts_at                   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at                  TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_rollback_window_workspace_id_id_unique
        UNIQUE (workspace_id, id),
    CONSTRAINT cerebro_company_brain_rollback_window_logical_unique
        UNIQUE (workspace_id, company_brain_connection_id),
    CONSTRAINT cerebro_company_brain_rollback_window_logical_fk
        FOREIGN KEY (workspace_id, company_brain_connection_id)
        REFERENCES cerebro_company_brain_connection (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_rollback_window_snapshot_valid
        CHECK (snapshot_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT cerebro_company_brain_rollback_window_duration_valid
        CHECK (
            expires_at > starts_at
            AND expires_at <= starts_at + interval '14 days'
        )
);

CREATE TABLE IF NOT EXISTS cerebro_company_brain_connection_tombstone (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id           UUID        NOT NULL,
    rollback_window_id     UUID        NOT NULL,
    legacy_connection_id   UUID        NOT NULL,
    legacy_connection_name TEXT        NOT NULL,
    metadata_sha256        TEXT        NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_connection_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, legacy_connection_id),
    CONSTRAINT cerebro_company_brain_connection_tombstone_name_unique
        UNIQUE (workspace_id, rollback_window_id, legacy_connection_name),
    CONSTRAINT cerebro_company_brain_connection_tombstone_full_identity_unique
        UNIQUE (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        ),
    CONSTRAINT cerebro_company_brain_connection_tombstone_window_fk
        FOREIGN KEY (workspace_id, rollback_window_id)
        REFERENCES cerebro_company_brain_rollback_window (workspace_id, id)
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_connection_tombstone_connection_fk
        FOREIGN KEY (
            workspace_id, legacy_connection_id, legacy_connection_name
        )
        REFERENCES workspace_connection (workspace_id, id, name)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_connection_tombstone_name_valid
        CHECK (btrim(legacy_connection_name) <> ''),
    CONSTRAINT cerebro_company_brain_connection_tombstone_metadata_valid
        CHECK (metadata_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_company_brain_connection_tombstone_connection
    ON cerebro_company_brain_connection_tombstone (
        workspace_id, legacy_connection_id
    );

CREATE TABLE IF NOT EXISTS cerebro_company_brain_permission_tombstone (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID        NOT NULL,
    rollback_window_id   UUID        NOT NULL,
    legacy_connection_id UUID        NOT NULL,
    legacy_connection_name TEXT      NOT NULL,
    permission_id        UUID        NOT NULL,
    tool_key             TEXT        NOT NULL,
    layer                TEXT        NOT NULL,
    subject_id           UUID        NOT NULL,
    resource_pattern     TEXT        NOT NULL,
    metadata_sha256      TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_permission_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, permission_id),
    CONSTRAINT cerebro_company_brain_permission_tombstone_full_identity_unique
        UNIQUE (
            workspace_id, rollback_window_id, permission_id,
            tool_key, layer, subject_id, resource_pattern
        ),
    CONSTRAINT cerebro_company_brain_permission_tombstone_connection_fk
        FOREIGN KEY (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        REFERENCES cerebro_company_brain_connection_tombstone (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_permission_tombstone_permission_fk
        FOREIGN KEY (
            workspace_id, permission_id,
            tool_key, layer, subject_id, resource_pattern
        )
        REFERENCES cerebro_tool_policy (
            workspace_id, id, tool_key, layer, subject_id, resource_pattern
        )
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_permission_tombstone_connection_match
        CHECK (
            tool_key = 'connection:' || lower(btrim(legacy_connection_name))
        ),
    CONSTRAINT cerebro_company_brain_permission_tombstone_metadata_valid
        CHECK (metadata_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_company_brain_permission_tombstone_connection
    ON cerebro_company_brain_permission_tombstone (
        workspace_id, rollback_window_id, legacy_connection_id
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_permission_tombstone_permission
    ON cerebro_company_brain_permission_tombstone (workspace_id, permission_id);

CREATE TABLE IF NOT EXISTS cerebro_company_brain_approval_tombstone (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID        NOT NULL,
    rollback_window_id   UUID        NOT NULL,
    legacy_connection_id UUID        NOT NULL,
    legacy_connection_name TEXT      NOT NULL,
    approval_id          UUID        NOT NULL,
    capability           TEXT        NOT NULL,
    resource             TEXT        NOT NULL,
    metadata_sha256      TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_approval_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, approval_id),
    CONSTRAINT cerebro_company_brain_approval_tombstone_connection_fk
        FOREIGN KEY (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        REFERENCES cerebro_company_brain_connection_tombstone (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_approval_tombstone_approval_fk
        FOREIGN KEY (workspace_id, approval_id, capability, resource)
        REFERENCES cerebro_approval_request (
            workspace_id, id, capability, resource
        )
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_approval_tombstone_connection_match
        CHECK (
            capability = 'connection:' || lower(btrim(legacy_connection_name))
            OR left(
                capability,
                length('connection:' || lower(btrim(legacy_connection_name))) + 1
            ) = 'connection:' || lower(btrim(legacy_connection_name)) || ':'
        ),
    CONSTRAINT cerebro_company_brain_approval_tombstone_metadata_valid
        CHECK (metadata_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_company_brain_approval_tombstone_connection
    ON cerebro_company_brain_approval_tombstone (
        workspace_id, rollback_window_id, legacy_connection_id
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_approval_tombstone_approval
    ON cerebro_company_brain_approval_tombstone (workspace_id, approval_id);

CREATE TABLE IF NOT EXISTS cerebro_company_brain_approval_audit_tombstone (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID        NOT NULL,
    rollback_window_id UUID        NOT NULL,
    approval_id        UUID        NOT NULL,
    approval_audit_id  UUID        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_approval_audit_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, approval_audit_id),
    CONSTRAINT cerebro_company_brain_approval_audit_tombstone_approval_fk
        FOREIGN KEY (workspace_id, rollback_window_id, approval_id)
        REFERENCES cerebro_company_brain_approval_tombstone (
            workspace_id, rollback_window_id, approval_id
        )
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_approval_audit_tombstone_audit_fk
        FOREIGN KEY (workspace_id, approval_id, approval_audit_id)
        REFERENCES cerebro_approval_audit (workspace_id, approval_id, id)
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_company_brain_approval_audit_tombstone_approval
    ON cerebro_company_brain_approval_audit_tombstone (
        workspace_id, rollback_window_id, approval_id
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_approval_audit_tombstone_audit
    ON cerebro_company_brain_approval_audit_tombstone (
        workspace_id, approval_id, approval_audit_id
    );

CREATE TABLE IF NOT EXISTS cerebro_company_brain_permission_audit_tombstone (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID        NOT NULL,
    rollback_window_id  UUID        NOT NULL,
    permission_id       UUID        NOT NULL,
    permission_audit_id UUID        NOT NULL,
    tool_key            TEXT        NOT NULL,
    layer               TEXT        NOT NULL,
    subject_id          UUID        NOT NULL,
    resource_pattern    TEXT        NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_permission_audit_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, permission_audit_id),
    CONSTRAINT cerebro_company_brain_permission_audit_tombstone_permission_fk
        FOREIGN KEY (
            workspace_id, rollback_window_id, permission_id,
            tool_key, layer, subject_id, resource_pattern
        )
        REFERENCES cerebro_company_brain_permission_tombstone (
            workspace_id, rollback_window_id, permission_id,
            tool_key, layer, subject_id, resource_pattern
        )
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_permission_audit_tombstone_audit_fk
        FOREIGN KEY (
            workspace_id, permission_audit_id,
            tool_key, layer, subject_id, resource_pattern
        )
        REFERENCES cerebro_tool_policy_audit (
            workspace_id, id, tool_key, layer, subject_id, resource_pattern
        )
        ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_company_brain_permission_audit_tombstone_permission
    ON cerebro_company_brain_permission_audit_tombstone (
        workspace_id, rollback_window_id, permission_id
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_permission_audit_tombstone_audit
    ON cerebro_company_brain_permission_audit_tombstone (
        workspace_id, permission_audit_id,
        tool_key, layer, subject_id, resource_pattern
    );

CREATE TABLE IF NOT EXISTS cerebro_company_brain_tool_alias_tombstone (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID        NOT NULL,
    rollback_window_id   UUID        NOT NULL,
    legacy_connection_id UUID        NOT NULL,
    legacy_connection_name TEXT      NOT NULL,
    alias_id             UUID        NOT NULL,
    capability_id        TEXT        NOT NULL,
    metadata_sha256      TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_tool_alias_tombstone_identity_unique
        UNIQUE (workspace_id, rollback_window_id, alias_id),
    CONSTRAINT cerebro_company_brain_tool_alias_tombstone_connection_fk
        FOREIGN KEY (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        REFERENCES cerebro_company_brain_connection_tombstone (
            workspace_id, rollback_window_id,
            legacy_connection_id, legacy_connection_name
        )
        ON DELETE CASCADE,
    CONSTRAINT cerebro_company_brain_tool_alias_tombstone_alias_fk
        FOREIGN KEY (alias_id, capability_id)
        REFERENCES cerebro_capability_alias (id, capability_id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_tool_alias_tombstone_connection_match
        CHECK (
            capability_id =
                'connection:' || lower(btrim(legacy_connection_name))
            OR left(
                capability_id,
                length('connection:' || lower(btrim(legacy_connection_name))) + 1
            ) = 'connection:' || lower(btrim(legacy_connection_name)) || ':'
        ),
    CONSTRAINT cerebro_company_brain_tool_alias_tombstone_metadata_valid
        CHECK (metadata_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_company_brain_tool_alias_tombstone_connection
    ON cerebro_company_brain_tool_alias_tombstone (
        workspace_id, rollback_window_id, legacy_connection_id
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_tool_alias_tombstone_alias
    ON cerebro_company_brain_tool_alias_tombstone (alias_id);

CREATE OR REPLACE FUNCTION cerebro_protect_active_company_brain_rollback()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    rollback_expires_at TIMESTAMPTZ;
BEGIN
    IF TG_TABLE_NAME = 'cerebro_company_brain_rollback_window' THEN
        IF TG_OP = 'INSERT' THEN
            IF NEW.starts_at > clock_timestamp()
                OR NEW.expires_at <= clock_timestamp() THEN
                RAISE EXCEPTION USING
                    ERRCODE = '23514',
                    MESSAGE =
                        'rollback window must start now and expire in the future';
            END IF;
            RETURN NEW;
        END IF;
        IF TG_OP = 'UPDATE' THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE =
                    'active rollback metadata cannot be deleted or shortened';
        END IF;
        rollback_expires_at := OLD.expires_at;
    ELSE
        IF TG_OP = 'UPDATE' THEN
            RAISE EXCEPTION USING
                ERRCODE = '23514',
                MESSAGE =
                    'active rollback metadata cannot be deleted or shortened';
        END IF;
        IF TG_OP = 'INSERT' THEN
            SELECT expires_at
            INTO rollback_expires_at
            FROM cerebro_company_brain_rollback_window
            WHERE workspace_id = NEW.workspace_id
              AND id = NEW.rollback_window_id;

            IF rollback_expires_at <= clock_timestamp() THEN
                RAISE EXCEPTION USING
                    ERRCODE = '23514',
                    MESSAGE =
                        'tombstones cannot be added after rollback expiry';
            END IF;
            RETURN NEW;
        END IF;

        SELECT expires_at
        INTO rollback_expires_at
        FROM cerebro_company_brain_rollback_window
        WHERE workspace_id = OLD.workspace_id
          AND id = OLD.rollback_window_id;
    END IF;

    IF rollback_expires_at > clock_timestamp() THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            MESSAGE =
                'active rollback metadata cannot be deleted or shortened';
    END IF;

    RETURN OLD;
END
$$;

DROP TRIGGER IF EXISTS protect_active_company_brain_rollback_window
    ON cerebro_company_brain_rollback_window;
CREATE TRIGGER protect_active_company_brain_rollback_window
    BEFORE INSERT OR UPDATE OR DELETE ON cerebro_company_brain_rollback_window
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_connection_tombstone
    ON cerebro_company_brain_connection_tombstone;
CREATE TRIGGER protect_active_company_brain_connection_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_connection_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_permission_tombstone
    ON cerebro_company_brain_permission_tombstone;
CREATE TRIGGER protect_active_company_brain_permission_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_permission_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_approval_tombstone
    ON cerebro_company_brain_approval_tombstone;
CREATE TRIGGER protect_active_company_brain_approval_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_approval_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_approval_audit_tombstone
    ON cerebro_company_brain_approval_audit_tombstone;
CREATE TRIGGER protect_active_company_brain_approval_audit_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_approval_audit_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_permission_audit_tombstone
    ON cerebro_company_brain_permission_audit_tombstone;
CREATE TRIGGER protect_active_company_brain_permission_audit_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_permission_audit_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();

DROP TRIGGER IF EXISTS protect_active_company_brain_tool_alias_tombstone
    ON cerebro_company_brain_tool_alias_tombstone;
CREATE TRIGGER protect_active_company_brain_tool_alias_tombstone
    BEFORE INSERT OR UPDATE OR DELETE
    ON cerebro_company_brain_tool_alias_tombstone
    FOR EACH ROW
    EXECUTE FUNCTION cerebro_protect_active_company_brain_rollback();
