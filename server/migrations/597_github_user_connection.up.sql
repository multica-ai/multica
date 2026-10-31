-- MUL-8433: a Multica user's personal GitHub account, authorized through the
-- GitHub App's user-to-server OAuth flow. PR writes made from Multica (merge,
-- approve, close, ...) are sent to GitHub with this token, so GitHub applies
-- the person's own repository permissions and branch protection.
--
-- One row per Multica user, shared across workspaces. Both tokens are sealed
-- with MULTICA_GITHUB_SECRET_KEY (secretbox) and never stored in plaintext.
-- GitHub rotates the refresh token on every use, so refreshes lock this row.
--
-- No FK to "user" by design (repository rule). The primary key is built
-- concurrently in 598/599, so no migration builds an index inline.
CREATE TABLE github_user_connection (
    user_id                  UUID        NOT NULL,
    github_user_id           BIGINT      NOT NULL,
    login                    TEXT        NOT NULL,
    avatar_url               TEXT,
    access_token_encrypted   BYTEA       NOT NULL,
    -- NULL when the App does not expire user tokens.
    access_token_expires_at  TIMESTAMPTZ,
    refresh_token_encrypted  BYTEA,
    refresh_token_expires_at TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
