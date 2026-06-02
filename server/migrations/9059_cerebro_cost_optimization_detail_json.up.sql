-- FIR-2765: optional JSON detail on cost-optimization measurements (e.g.
-- context_duplication top_overlap_sections).
ALTER TABLE cerebro_cost_optimization_measurement
    ADD COLUMN IF NOT EXISTS detail_json JSONB;
