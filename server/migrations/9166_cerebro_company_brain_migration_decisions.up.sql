-- CEREBRO-PATCH(company-brain-migration-decisions): FIR-3924 stores the
-- versioned, non-secret decision for each agent migration conflict before
-- cutover. This migration creates schema only: it does not classify agents,
-- save decisions, or change Connections, credentials, permissions, agents,
-- approvals, aliases, prompts, or cutover state.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'cerebro_company_brain_connection_workspace_id_id_unique'
    ) THEN
        ALTER TABLE cerebro_company_brain_connection
            ADD CONSTRAINT cerebro_company_brain_connection_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'agent_workspace_id_id_unique'
    ) THEN
        ALTER TABLE agent
            ADD CONSTRAINT agent_workspace_id_id_unique
            UNIQUE (workspace_id, id);
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS cerebro_company_brain_migration_decision (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id                UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    company_brain_connection_id UUID        NOT NULL,
    agent_id                    UUID        NOT NULL,
    census_version              BIGINT      NOT NULL,
    conflict_code               TEXT        NOT NULL,
    affected_reference          TEXT        NOT NULL,
    outcome                     TEXT        NOT NULL,
    status                      TEXT        NOT NULL,
    consequence                 TEXT        NOT NULL,
    recommended_choice          TEXT        NOT NULL,
    safe_remediation            TEXT        NOT NULL,
    observed_state              TEXT        NOT NULL,
    expected_state              TEXT        NOT NULL,
    owner_user_id               UUID,
    saved_decision              TEXT,
    decided_by_user_id          UUID,
    decided_at                  TIMESTAMPTZ,
    decision_note               TEXT,
    evidence_sha256             TEXT        NOT NULL,
    evidence_at                 TIMESTAMPTZ NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_company_brain_migration_decision_identity_unique
        UNIQUE (
            workspace_id, company_brain_connection_id, agent_id,
            census_version, conflict_code, affected_reference
        ),
    CONSTRAINT cerebro_company_brain_migration_decision_connection_fk
        FOREIGN KEY (workspace_id, company_brain_connection_id)
        REFERENCES cerebro_company_brain_connection (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_migration_decision_agent_fk
        FOREIGN KEY (workspace_id, agent_id)
        REFERENCES agent (workspace_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_migration_decision_owner_fk
        FOREIGN KEY (workspace_id, owner_user_id)
        REFERENCES member (workspace_id, user_id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_migration_decision_decider_fk
        FOREIGN KEY (workspace_id, decided_by_user_id)
        REFERENCES member (workspace_id, user_id)
        ON DELETE RESTRICT,
    CONSTRAINT cerebro_company_brain_migration_decision_census_version_valid
        CHECK (census_version > 0),
    CONSTRAINT cerebro_company_brain_migration_decision_conflict_code_valid
        CHECK (
            conflict_code = upper(btrim(conflict_code))
            AND conflict_code ~ '^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$'
        ),
    CONSTRAINT cerebro_company_brain_migration_decision_reference_valid
        CHECK (btrim(affected_reference) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_outcome_valid
        CHECK (
            outcome IN (
                'automatic',
                'owner_decision',
                'cannot_migrate',
                'do_not_migrate'
            )
        ),
    CONSTRAINT cerebro_company_brain_migration_decision_status_valid
        CHECK (status IN ('pending', 'resolved', 'blocked')),
    CONSTRAINT cerebro_company_brain_migration_decision_saved_valid
        CHECK (
            saved_decision IS NULL
            OR saved_decision IN ('migrate', 'do_not_migrate')
        ),
    CONSTRAINT cerebro_company_brain_migration_decision_consequence_valid
        CHECK (btrim(consequence) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_recommendation_valid
        CHECK (btrim(recommended_choice) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_remediation_valid
        CHECK (btrim(safe_remediation) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_observed_valid
        CHECK (btrim(observed_state) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_expected_valid
        CHECK (btrim(expected_state) <> ''),
    CONSTRAINT cerebro_company_brain_migration_decision_evidence_valid
        CHECK (evidence_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT cerebro_company_brain_migration_decision_outcome_state_valid
        CHECK (
            (
                outcome = 'automatic'
                AND status = 'resolved'
                AND saved_decision = 'migrate'
                AND owner_user_id IS NULL
                AND decided_by_user_id IS NULL
                AND decided_at IS NULL
                AND decision_note IS NULL
            )
            OR (
                outcome = 'owner_decision'
                AND owner_user_id IS NOT NULL
                AND (
                    (
                        status = 'pending'
                        AND saved_decision IS NULL
                        AND decided_by_user_id IS NULL
                        AND decided_at IS NULL
                        AND decision_note IS NULL
                    )
                    OR (
                        status = 'resolved'
                        AND saved_decision = 'migrate'
                        AND decided_by_user_id IS NOT NULL
                        AND decided_at IS NOT NULL
                        AND decision_note IS NOT NULL
                        AND btrim(decision_note) <> ''
                    )
                )
            )
            OR (
                outcome = 'cannot_migrate'
                AND status = 'blocked'
                AND saved_decision IS NULL
                AND owner_user_id IS NULL
                AND decided_by_user_id IS NULL
                AND decided_at IS NULL
                AND decision_note IS NULL
            )
            OR (
                outcome = 'do_not_migrate'
                AND status = 'resolved'
                AND saved_decision = 'do_not_migrate'
                AND owner_user_id IS NOT NULL
                AND decided_by_user_id IS NOT NULL
                AND decided_at IS NOT NULL
                AND decision_note IS NOT NULL
                AND btrim(decision_note) <> ''
            )
        )
);

CREATE INDEX IF NOT EXISTS idx_company_brain_migration_decision_review
    ON cerebro_company_brain_migration_decision (
        workspace_id, company_brain_connection_id,
        census_version DESC, status, outcome
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_migration_decision_agent
    ON cerebro_company_brain_migration_decision (
        workspace_id, agent_id, census_version DESC
    );

CREATE INDEX IF NOT EXISTS idx_company_brain_migration_decision_pending_owner
    ON cerebro_company_brain_migration_decision (
        workspace_id, owner_user_id, created_at
    )
    WHERE outcome = 'owner_decision' AND status = 'pending';
