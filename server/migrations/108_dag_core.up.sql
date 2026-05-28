-- Optimized DAG core contract for multica-dag.
--
-- This is intentionally a small append-only substrate, not a second issue
-- system. Product tables such as issue/agent_task_queue can project into this
-- graph later; the durable source for graph mutations is dag_event.

CREATE TABLE dag_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    record_ids TEXT[] NOT NULL CHECK (array_length(record_ids, 1) IS NOT NULL),
    agent_id TEXT NOT NULL,
    dvt JSONB NOT NULL,
    operation TEXT NOT NULL CHECK (operation IN (
        'create',
        'update',
        'delete',
        'link',
        'unlink',
        'assert',
        'cite',
        'conflict',
        'resolve_conflict'
    )),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT dag_event_dvt_is_object CHECK (jsonb_typeof(dvt) = 'object'),
    CONSTRAINT dag_event_dvt_has_dot CHECK (dvt ? 'dot'),
    CONSTRAINT dag_event_dvt_has_context CHECK (dvt ? 'context'),
    CONSTRAINT dag_event_dvt_dot_agent_matches CHECK (dvt #>> '{dot,agent_id}' = agent_id),
    CONSTRAINT dag_event_dvt_dot_counter_positive CHECK (((dvt #>> '{dot,counter}')::bigint) > 0)
);

CREATE INDEX idx_dag_event_workspace_created ON dag_event(workspace_id, created_at, id);
CREATE INDEX idx_dag_event_record_ids_gin ON dag_event USING GIN (record_ids);
CREATE INDEX idx_dag_event_payload_gin ON dag_event USING GIN (payload jsonb_path_ops);

CREATE TABLE dag_record_projection (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    type TEXT NOT NULL,
    created_event_id UUID NOT NULL REFERENCES dag_event(id) ON DELETE RESTRICT,
    tombstoned_event_id UUID REFERENCES dag_event(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id),
    CONSTRAINT dag_record_id_not_empty CHECK (length(trim(id)) > 0),
    CONSTRAINT dag_record_type_not_empty CHECK (length(trim(type)) > 0)
);

CREATE INDEX idx_dag_record_projection_type ON dag_record_projection(workspace_id, type);

CREATE TABLE dag_link_projection (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    from_id TEXT NOT NULL,
    to_id TEXT NOT NULL,
    type TEXT NOT NULL,
    event_id UUID NOT NULL REFERENCES dag_event(id) ON DELETE RESTRICT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, from_id, to_id, type),
    FOREIGN KEY (workspace_id, from_id) REFERENCES dag_record_projection(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, to_id) REFERENCES dag_record_projection(workspace_id, id) ON DELETE CASCADE,
    CONSTRAINT dag_link_no_self_loop CHECK (from_id <> to_id),
    CONSTRAINT dag_link_type_not_empty CHECK (length(trim(type)) > 0)
);

CREATE INDEX idx_dag_link_projection_to ON dag_link_projection(workspace_id, to_id, type) WHERE active;
CREATE INDEX idx_dag_link_projection_from ON dag_link_projection(workspace_id, from_id, type) WHERE active;

CREATE TABLE dag_fact_projection (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    predicate TEXT NOT NULL,
    args JSONB NOT NULL,
    event_id UUID NOT NULL REFERENCES dag_event(id) ON DELETE RESTRICT,
    grounded_by TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    confidence DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT dag_fact_args_is_array CHECK (jsonb_typeof(args) = 'array'),
    CONSTRAINT dag_fact_predicate_not_empty CHECK (length(trim(predicate)) > 0),
    CONSTRAINT dag_fact_confidence_range CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1))
);

CREATE INDEX idx_dag_fact_workspace_predicate ON dag_fact_projection(workspace_id, predicate);
CREATE INDEX idx_dag_fact_args_gin ON dag_fact_projection USING GIN (args jsonb_path_ops);
CREATE INDEX idx_dag_fact_grounded_by_gin ON dag_fact_projection USING GIN (grounded_by);

CREATE TABLE dag_citation_chain_projection (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    assertion_id TEXT NOT NULL,
    citations JSONB NOT NULL,
    event_id UUID NOT NULL REFERENCES dag_event(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, assertion_id),
    CONSTRAINT dag_citation_chain_citations_array CHECK (jsonb_typeof(citations) = 'array')
);

CREATE TABLE dag_conflict_state (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    left_fact_id UUID NOT NULL REFERENCES dag_fact_projection(id) ON DELETE CASCADE,
    right_fact_id UUID NOT NULL REFERENCES dag_fact_projection(id) ON DELETE CASCADE,
    severity TEXT NOT NULL DEFAULT 'requires_review' CHECK (severity IN ('requires_review', 'blocking')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    reason TEXT NOT NULL DEFAULT '',
    event_id UUID REFERENCES dag_event(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT dag_conflict_distinct_facts CHECK (left_fact_id <> right_fact_id)
);

CREATE INDEX idx_dag_conflict_state_workspace_status ON dag_conflict_state(workspace_id, status, created_at);

CREATE TABLE dag_schema_dependency (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    type_name TEXT NOT NULL,
    depends_on_type TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, type_name, depends_on_type),
    CONSTRAINT dag_schema_dependency_no_self_edge CHECK (type_name <> depends_on_type)
);
