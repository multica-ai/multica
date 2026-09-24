-- Global agents (#8775): an agent definition owned by a user rather than a
-- workspace. It is materialised in each workspace the owner enables it in as an
-- ordinary `agent` row linked back through agent.global_agent_id, so dispatch,
-- runtimes, tasks, chats and permissions keep working on workspace-scoped rows
-- unchanged. The linked rows carry a copy of the synced fields below; the
-- application rewrites every linked row in the same transaction that changes
-- the global row.
--
-- Only identity and behaviour are synced. Runtime binding, model, thinking
-- level, env, MCP config, skills and access stay per workspace because they
-- reference workspace-scoped rows or machine-local state.
--
-- No foreign keys (repository rule): owner_id and agent.global_agent_id are
-- maintained by the application. Deleting a global agent unlinks its
-- workspace agents in the same transaction instead of cascading.
CREATE TABLE global_agent (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
        CONSTRAINT global_agent_description_length CHECK (char_length(description) <= 255),
    instructions TEXT NOT NULL DEFAULT '',
    avatar_url TEXT,
    conversation_starters JSONB NOT NULL DEFAULT '[]'::jsonb
        CONSTRAINT global_agent_conversation_starters_check CHECK (
            jsonb_typeof(conversation_starters) = 'array'
            AND jsonb_array_length(conversation_starters) <= 3
        ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- NULL for every ordinary workspace agent. Set only on a linked copy.
ALTER TABLE agent ADD COLUMN global_agent_id UUID;
