ALTER TABLE comment
  ADD COLUMN source_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;
