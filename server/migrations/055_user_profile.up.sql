-- Per-user agent communication profile (JEH-304).
-- One profile per user, global across workspaces. The compiled prompt
-- (see packages/core/profile/compile.ts) is injected into every agent call
-- so agents adapt to the user's communication preferences without per-chat
-- repetition.
--
-- Access control: NO row-level security. All reads/writes go through a
-- single repository function in server/internal/repository/user_profile.go
-- which enforces auth.user_id == row.user_id. Do not query this table from
-- anywhere else.

CREATE TABLE user_profile (
    user_id UUID PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE,
    persona TEXT NOT NULL CHECK (persona IN ('utalmodig', 'ekspert', 'grundig', 'larling')),
    language TEXT NOT NULL DEFAULT 'da' CHECK (language IN ('da', 'en')),
    length_pref SMALLINT NOT NULL CHECK (length_pref BETWEEN 0 AND 100),
    autonomy_pref SMALLINT NOT NULL CHECK (autonomy_pref BETWEEN 0 AND 100),
    tech_pref SMALLINT NOT NULL CHECK (tech_pref BETWEEN 0 AND 100),
    anti_patterns JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (
            jsonb_typeof(anti_patterns) = 'array'
            AND jsonb_array_length(anti_patterns) <= 20
        ),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
