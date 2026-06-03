-- Project scheduling dates. Nullable: a project may have neither, either, or both.
-- start_date/target_date give the health signal a timeline to reference.
ALTER TABLE project ADD COLUMN start_date DATE;
ALTER TABLE project ADD COLUMN target_date DATE;
