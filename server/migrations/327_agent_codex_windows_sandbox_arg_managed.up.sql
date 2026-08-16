-- Token values cannot identify whether Multica injected the canonical Codex
-- Windows sandbox prefix or a user explicitly supplied the same setting.
-- Existing rows default to user-owned so this migration never rewrites args.
ALTER TABLE agent
    ADD COLUMN codex_windows_sandbox_arg_managed BOOLEAN NOT NULL DEFAULT FALSE;
