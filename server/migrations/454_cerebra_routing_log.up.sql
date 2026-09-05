CREATE TABLE IF NOT EXISTS cerebra_routing_log (
    id                  TEXT        NOT NULL PRIMARY KEY,  -- UUIDv7
    task_id             TEXT,                               -- nullable: chat tasks may not have an issue task id at routing time
    issue_id            TEXT,                               -- nullable
    session_id          TEXT,                               -- nullable (chat_session.id)
    runtime_id          TEXT        NOT NULL,
    chosen_model        TEXT        NOT NULL,
    tier                TEXT        NOT NULL,               -- simple | standard | heavy
    matched_rule        TEXT        NOT NULL,               -- e.g. "keyword:refactor" or "token_count:4200"
    tool_chain_expected BOOLEAN     NOT NULL DEFAULT FALSE,
    fallback_used       BOOLEAN     NOT NULL DEFAULT FALSE,
    latency_ms          INT         NOT NULL DEFAULT 0,
    status              TEXT        NOT NULL DEFAULT 'ok',  -- ok | fallback | error
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Optional recommended columns:
    policy_reason       TEXT,
    candidate_count     INT,
    classifier_version  TEXT,
    estimated_cost      NUMERIC(12, 8),
    input_tokens        INT,
    output_tokens       INT
);
