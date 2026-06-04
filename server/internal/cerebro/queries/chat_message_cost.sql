-- name: GetChatSessionMessageCosts :many
-- Per-task token totals + cost for every task in this chat session, so the
-- chat UI can render a price badge under each assistant reply (FIR-31). Keyed
-- by task_id (= chat_message.task_id) and split by model so pricing stays
-- correct when a single run spans models. The handler sums per task and
-- prefers the gateway-reported spend over the pricing-table estimate, exactly
-- like the session total in GetChatSessionUsage.
SELECT
    tu.task_id,
    tu.model,
    COALESCE(SUM(tu.input_tokens), 0)::bigint       AS total_input_tokens,
    COALESCE(SUM(tu.output_tokens), 0)::bigint      AS total_output_tokens,
    COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS total_cache_read_tokens,
    COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
    COALESCE(SUM(tu.cost_cents), 0)::bigint          AS total_cost_cents
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.chat_session_id = $1
GROUP BY tu.task_id, tu.model;
