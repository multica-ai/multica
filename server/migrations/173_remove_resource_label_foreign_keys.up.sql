-- A short-lived pre-release deployment may already have applied the original
-- migration 162. Remove those database cascades before this release enables
-- resource labels; application transactions own cleanup from this point on.
ALTER TABLE agent_to_label DROP CONSTRAINT IF EXISTS agent_to_label_agent_id_fkey;
ALTER TABLE agent_to_label DROP CONSTRAINT IF EXISTS agent_to_label_label_id_fkey;
ALTER TABLE skill_to_label DROP CONSTRAINT IF EXISTS skill_to_label_skill_id_fkey;
ALTER TABLE skill_to_label DROP CONSTRAINT IF EXISTS skill_to_label_label_id_fkey;
