CREATE INDEX CONCURRENTLY agent_task_queue_memory_gate_due_idx ON agent_task_queue (memory_gate_state, memory_gate_next_wakeup, memory_gate_lease_expires_at) WHERE status = 'queued';
