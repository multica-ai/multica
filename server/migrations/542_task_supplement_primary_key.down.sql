-- Roll back the supplement migration batch together (538-543). PostgreSQL
-- drops the attached index with the constraint; 541 recreates it on upgrade.
ALTER TABLE task_supplement DROP CONSTRAINT IF EXISTS task_supplement_pkey;
