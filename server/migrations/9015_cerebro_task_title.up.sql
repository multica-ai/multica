-- Short human-friendly task title — generated at enqueue time from the
-- triggering comment (LLM call if ANTHROPIC_API_KEY is set, else a
-- truncated fallback). Surfaced in the inline "agent thinking" row in
-- channels, AgentLiveCard, inbox, and the Tasks list so users can tell
-- "Summary of CAC week 18" apart from "Lookup #firtal-test inventory" —
-- where today both display only the issue/channel title.
--
-- Distinct from trigger_summary: trigger_summary is the verbatim comment
-- snapshot for provenance; title is a short generated label for display.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS title TEXT;
