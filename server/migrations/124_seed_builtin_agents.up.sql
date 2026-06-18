-- 124_seed_builtin_agents.up.sql
-- Seed the default global built-in agents so a freshly deployed system ships
-- with the standard workflow roster out of the box.
--
-- These are the cross-workspace agents (workspace_id IS NULL, is_builtin = TRUE)
-- that gate on the can_manage_workflows permission rather than ownership.
-- owner_id is left NULL on purpose: the source user does not exist on a fresh
-- deploy, and built-in agents are not owner-gated. plugin_id refers to the
-- external plugin catalog (no local FK), so it is carried over verbatim.
--
-- Fixed UUIDs match the reference environment and keep the seed idempotent:
-- re-running this migration (or running it where an admin already promoted an
-- agent with the same id) is a no-op.

INSERT INTO multica_agent (
    id, workspace_id, name, description, avatar_url, runtime_mode,
    runtime_config, runtime_id, visibility, status, max_concurrent_tasks,
    owner_id, instructions, custom_env, custom_args, mcp_config,
    model, thinking_level, plugin_id, is_builtin
) VALUES
    ('dd0683f4-d72c-4b49-8030-827f5b15df2e', NULL, '需求分析', '需求梳理', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '你是一个需求分析师，按要求进行需求梳理', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, 'fa87f958-9229-442b-8bc3-4b22a4d6f806', TRUE),

    ('5e2fccac-6257-4ea5-ac7a-a5d8a4765917', NULL, '方案设计', '方案设计', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, '365d045e-8487-467f-94e6-8237fa97f4a6', TRUE),

    ('4348e20d-eadc-4095-ac7a-cd480e927375', NULL, '任务拆解', '任务拆解', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, '8fabc295-cd8d-4514-9230-a00bc880a4bb', TRUE),

    ('c0bea924-c78f-43b1-8d50-449ec3c6b4cf', NULL, 'TDD 编码', 'TDD 编码', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, '665b5bbf-b859-498a-826f-9584322c2a42', TRUE),

    ('67cdded4-c49f-4fc3-b7e0-52aa2038db91', NULL, '测试生成', '测试生成', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, '10a3b7d2-1af0-41bc-9d9c-9ed812e230f4', TRUE),

    ('24a981c1-6ea6-4eab-9225-a5fe3da64477', NULL, '集成验证', '集成验证', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, '5d54963c-9bb5-4373-9e33-721898452d78', TRUE),

    ('a6f5d437-93c2-4623-ba0a-bcbb5cb8d1a6', NULL, '审核师', '审核验证', NULL, 'local',
     '{}'::jsonb, NULL, 'workspace', 'idle', 6,
     NULL, '对前面的任务结果进行审核验证，但当前是测试阶段，直接通过就行', '{}'::jsonb, '[]'::jsonb, NULL,
     NULL, NULL, NULL, TRUE)
ON CONFLICT (id) DO NOTHING;
