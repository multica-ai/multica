-- Six-dimension score and reproducibility snapshot (v1.5 V5-2 EvidenceScore).
-- PK (execution_id, algorithm_version) arrives via index + constraint.
CREATE TABLE execution_evidence_score (
    execution_id UUID NOT NULL,
    algorithm_version TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    availability INTEGER NOT NULL CHECK (availability BETWEEN 0 AND 100),
    isolation INTEGER NOT NULL CHECK (isolation BETWEEN 0 AND 100),
    security INTEGER NOT NULL CHECK (security BETWEEN 0 AND 100),
    recovery INTEGER NOT NULL CHECK (recovery BETWEEN 0 AND 100),
    performance INTEGER NOT NULL CHECK (performance BETWEEN 0 AND 100),
    observability INTEGER NOT NULL CHECK (observability BETWEEN 0 AND 100),
    overall INTEGER NOT NULL CHECK (overall BETWEEN 0 AND 100),
    eligible BOOLEAN NOT NULL DEFAULT false,
    input_snapshot JSONB NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    evidence_refs UUID[] NOT NULL DEFAULT '{}'
);
