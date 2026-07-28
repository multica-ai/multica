DROP TRIGGER IF EXISTS prevent_ai_impact_observation_mutation_trigger
    ON cerebro_ai_impact_observation;
DROP FUNCTION IF EXISTS prevent_ai_impact_observation_mutation();

DROP TRIGGER IF EXISTS enforce_ai_impact_project_workspace_trigger
    ON cerebro_ai_impact_project_binding;
DROP FUNCTION IF EXISTS enforce_ai_impact_project_workspace();

DROP TABLE IF EXISTS cerebro_ai_impact_observation;
DROP TABLE IF EXISTS cerebro_ai_impact_metric;
DROP TABLE IF EXISTS cerebro_ai_impact_project_binding;
DROP TABLE IF EXISTS cerebro_ai_impact_operating_loop;
DROP TABLE IF EXISTS cerebro_ai_impact_function;
