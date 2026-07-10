-- FIR-2283 followup point 2 — a session (a comment thread) started by an Issue
-- workflow carries its workflow phase, so the UI can badge it "Plan" / "Build" /
-- "Review" next to Open/Resolved, and grouping can name the first session "Plan".
--
-- Nullable and free-text (not an enum) on purpose: an ordinary, non-workflow
-- session leaves it NULL and renders no badge, and a future phase name never
-- needs a migration to add. The engine writes only the three values it knows
-- today ('plan', 'build', 'review'); the reader treats anything else as a
-- generic capitalized label.
ALTER TABLE cerebro_session
    ADD COLUMN IF NOT EXISTS phase TEXT;
