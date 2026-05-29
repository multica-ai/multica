Purpose: Verify that workspace skills can be imported from a local directory selected in the browser, preserving `SKILL.md` and supporting files.

Preconditions: The Multica web app is reachable. The user is signed in and has access to a workspace. A local directory is prepared with `SKILL.md` at its root and at least one supporting text file, e.g. `guide.md`.

User flow:
1. Navigate to the workspace Skills page.
2. Click "New skill".
3. Choose "Upload local directory".
4. Select the prepared local directory.
5. Verify the dialog lists the detected skill name and supporting file count.
6. Click "Import 1 Skill".
7. When import completes, click "Done".
8. Open the imported skill detail page.

Expected results:
- The "Upload local directory" option is visible in the New skill dialog.
- The directory picker accepts a folder containing `SKILL.md`.
- The parser detects the skill name and description from `SKILL.md` frontmatter.
- Supporting text files in the selected directory are imported and visible in the skill file tree.
- Duplicate skill names are skipped or surfaced with a clear duplicate/skip message, not imported twice.
- Existing URL import and "Copy from runtime" flows remain visible in the same New skill dialog.

Notes for automation: Use a fixture directory containing `SKILL.md` plus one supporting file. Browser automation may need to set files on the hidden `input[type=file][webkitdirectory]` directly because native directory pickers cannot be controlled reliably.
