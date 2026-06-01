Purpose: Verify that workspace skills can be imported from local directories and private Git sources, preserving `SKILL.md`, supporting files, local runtime discovery behavior, and CLI agent skill membership edits.

Preconditions: The Multica web app is reachable. The user is signed in and has access to a workspace. A local directory is prepared with `SKILL.md` at its root and at least one supporting text file, e.g. `guide.md`. If private Git import is being verified, a private Gitee repository/file path and token are available for the test workspace.

User flow:
1. Navigate to the workspace Skills page.
2. Click "New skill".
3. Choose "Upload local directory".
4. Select the prepared local directory.
5. Verify the dialog lists the detected skill name and supporting file count.
6. Click "Import 1 Skill".
7. When import completes, click "Done".
8. Open the imported skill detail page.
9. Import the same skill source again and verify duplicate handling.
10. Import a private Gitee skill source, or use a fixture/mock that exercises the private contents API path.
11. Start a local runtime profile that has Claude/Copilot/OpenCode local skill directories and verify the daemon scan reports importable local skills, including `.claude/commands/*.md` command files where present.
12. Use `multica agent skills add <agent> <skill>` and `multica agent skills remove <agent> <skill>` against a disposable agent.

Expected results:
- The "Upload local directory" option is visible in the New skill dialog.
- The directory picker accepts a folder containing `SKILL.md`.
- The parser detects the skill name and description from `SKILL.md` frontmatter.
- Supporting text files in the selected directory are imported and visible in the skill file tree.
- Duplicate skill names are skipped or surfaced with a clear duplicate/skip message, not imported twice.
- Existing URL import and "Copy from runtime" flows remain visible in the same New skill dialog.
- Private Gitee file imports use the contents API path and succeed without requiring public raw-file access.
- Invalid local skill candidates are skipped with diagnostics and do not fail the entire local scan.
- Claude command markdown files, Copilot local skills, and OpenCode local skills are discovered as importable local skill candidates when present.
- CLI incremental `agent skills add/remove` updates the agent's skill set without replacing unrelated skills and reports a clear error for unknown skill IDs.

Notes for automation: Use a fixture directory containing `SKILL.md` plus one supporting file. Browser automation may need to set files on the hidden `input[type=file][webkitdirectory]` directly because native directory pickers cannot be controlled reliably. Private Gitee import can be verified with a disposable private repository or a handler-level test fixture if external credentials are unavailable.
