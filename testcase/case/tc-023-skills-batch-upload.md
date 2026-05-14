Purpose: Verify that skills can be batch-uploaded to an agent, including support for dot directories like `.claude` and `.codex`.

Preconditions: The Multica web app is reachable. The user is signed in. The user owns an agent. Multiple skill files or directories are prepared for upload, including at least one dot directory (e.g., `.claude/` containing SKILL.md).

User flow: Navigate to an agent's detail page. Open the Skills tab. Locate the batch upload or add-skills interface. Select multiple skill files/directories for upload (including a dot directory). Submit the batch upload. Wait for the upload to complete.

Expected results: The batch upload interface accepts multiple files/directories simultaneously. Dot directories (`.claude`, `.codex`) are recognized and uploaded successfully (not filtered out). After upload, all skills appear in the agent's skill list with their correct names. If a skill with a duplicate name already exists, the system either auto-renames with a suffix or shows an appropriate error. Each uploaded skill's SKILL.md content is preserved.

Notes for automation: The batch upload may use a file picker dialog or drag-and-drop area. Verify skill count before and after upload. Check that dot-prefixed directory names are preserved in the skill listing.
