-- Rows the original CHECK would reject must go before the constraint returns.
DELETE FROM cerebro_analytics_quality_measurement WHERE measurement_type = 'satisfaction';
ALTER TABLE cerebro_analytics_quality_measurement
    DROP CONSTRAINT IF EXISTS cerebro_analytics_quality_measurement_measurement_type_check;
ALTER TABLE cerebro_analytics_quality_measurement
    ADD CONSTRAINT cerebro_analytics_quality_measurement_measurement_type_check
    CHECK (measurement_type IN ('judge_gate', 'skill_observation', 'evaluator'));
