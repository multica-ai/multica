ALTER TABLE agent ALTER COLUMN max_concurrent_tasks SET DEFAULT 6;
UPDATE agent SET max_concurrent_tasks = 6 WHERE max_concurrent_tasks = 1;
