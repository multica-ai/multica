-- Durable per-input settlement survives an OnSettled callback before Add starts.
ALTER TABLE chat_message ADD COLUMN IF NOT EXISTS channel_typing_settled boolean NOT NULL DEFAULT false;

-- No foreign keys: cleanup anchors must survive session, task and installation
-- deletion. The snapshot contains encrypted installation credentials only.
CREATE TABLE IF NOT EXISTS channel_typing_reaction (
 id uuid NOT NULL,
 workspace_id uuid NOT NULL,
 chat_session_id uuid NOT NULL,
 chat_message_id uuid NOT NULL,
 installation_id uuid NOT NULL,
 channel_message_id text NOT NULL,
 installation_snapshot jsonb NOT NULL,
 reaction_id text NOT NULL DEFAULT '',
 add_finished boolean NOT NULL DEFAULT false,
 cleanup_required boolean NOT NULL DEFAULT false,
 cleaned_at timestamptz,
 retry_after timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now()
);
