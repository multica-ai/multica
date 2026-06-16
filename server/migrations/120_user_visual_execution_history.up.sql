-- Per-user opt-in for the visual execution-history view (MUL-1).
--
-- When enabled, opening an agent run renders its execution history as a
-- read-only, chat-style transcript (reusing the chat renderer) instead of
-- the raw technical timeline; in-progress runs stream live. The choice is
-- a personal viewing preference — like theme/language/timezone — so it
-- lives on the user row, not on workspace settings.
--
-- Defaults to false: the raw log view stays the default until a member
-- opts themselves in from Settings -> Preferences. Reverting the feature
-- only requires ignoring the column (the .down.sql drops it).
ALTER TABLE "user"
    ADD COLUMN visual_execution_history BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN "user".visual_execution_history IS
    'Per-user opt-in for the visual (chat-style, read-only) execution '
    'history view. false = raw log view (default). Personal viewing '
    'preference; does not affect how execution data is stored or captured.';
