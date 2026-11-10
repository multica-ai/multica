-- Focused inbox settings live in the existing preferences map. Every client
-- decodes the map as string -> string, so one non-string value breaks every
-- read of it. NOT VALID + VALIDATE keeps the ACCESS EXCLUSIVE lock brief.
-- strict: lax mode unwraps arrays in the filter, so ["x"] or [] would pass.
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_values_are_strings
    CHECK (
        jsonb_typeof(preferences) = 'object'
        AND NOT jsonb_path_exists(preferences, 'strict $.* ? (@.type() != "string")')
    ) NOT VALID;
ALTER TABLE notification_preference VALIDATE CONSTRAINT notification_preference_values_are_strings;

-- An absent inbox_mode means "all"; no backfill.
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_inbox_mode_valid
    CHECK (
        NOT (preferences ? 'inbox_mode')
        OR preferences ->> 'inbox_mode' IN ('all', 'focused')
    ) NOT VALID;
ALTER TABLE notification_preference VALIDATE CONSTRAINT notification_preference_inbox_mode_valid;

-- Covers every focused_* key, not a fixed list, so a new rule toggle needs no
-- constraint change. silent=true lets a non-object pass here; the string
-- constraint above rejects it.
ALTER TABLE notification_preference ADD CONSTRAINT notification_preference_focused_rules_valid
    CHECK (
        NOT jsonb_path_exists(
            preferences,
            '$.keyvalue() ? (@.key starts with "focused_" && !(@.value == "on" || @.value == "off"))',
            '{}',
            true
        )
    ) NOT VALID;
ALTER TABLE notification_preference VALIDATE CONSTRAINT notification_preference_focused_rules_valid;
