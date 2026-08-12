CREATE INDEX CONCURRENTLY memoryhub_compensation_binding_idx ON memoryhub_compensation (binding_id) WHERE binding_id IS NOT NULL;
