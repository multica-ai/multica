-- CEREBRO-PATCH(sqlc-user-profile): cerebro modification of upstream file
-- CEREBRO-PATCH(sqlc-user-profile-v2): JEH-1031 — query reflects 4 scope
-- ratings + custom_prompt + prompt_mode columns.
-- name: GetUserProfile :one
SELECT * FROM user_profile
WHERE user_id = $1;

-- name: UpsertUserProfile :one
INSERT INTO user_profile (
    user_id, persona, language, length_pref, autonomy_pref,
    git_pref, code_pref, computer_pref, process_pref,
    anti_patterns, custom_prompt, prompt_mode
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (user_id) DO UPDATE SET
    persona = EXCLUDED.persona,
    language = EXCLUDED.language,
    length_pref = EXCLUDED.length_pref,
    autonomy_pref = EXCLUDED.autonomy_pref,
    git_pref = EXCLUDED.git_pref,
    code_pref = EXCLUDED.code_pref,
    computer_pref = EXCLUDED.computer_pref,
    process_pref = EXCLUDED.process_pref,
    anti_patterns = EXCLUDED.anti_patterns,
    custom_prompt = EXCLUDED.custom_prompt,
    prompt_mode = EXCLUDED.prompt_mode,
    updated_at = now()
RETURNING *;

-- name: DeleteUserProfile :exec
DELETE FROM user_profile WHERE user_id = $1;
