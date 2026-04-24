# Browser Regression Cases

Source of truth: issue `OPE-12` and the current local skill import implementation in the Skills page.

The repository does not currently include `testcase/ui-selectors.json`, so these cases are written as human-readable scenario specifications rather than executable operation scripts. Convert them to semantic-key operation syntax after selector bindings are added.

## Suite Setup

- The tester is signed in as a workspace member who can create workspace skills.
- The target page is the workspace Skills page.
- Prepare `fixtures/skills-local/codex-basic/` with root `AGENTS.md` and one supporting text file.
- Prepare `fixtures/skills-local/claude-basic/` with `.claude/commands/review.md` and `.claude/commands/test.md`.
- Prepare `fixtures/skills-local/skillmd-basic/` with `writer/SKILL.md` and one supporting file under `writer/`.
- Prepare `fixtures/skills-local/empty/` with no supported text skill files.

## Case LOC-001 Codex Local Directory Imports As One Skill

- Purpose: Verify that a workspace member can import a Codex-style local skill directory and see the imported skill selected.
- Preconditions: The workspace Skills page is accessible, the user can create skills, and `fixtures/skills-local/codex-basic/` contains `AGENTS.md` plus a supporting file.
- User flow: Navigate to the workspace Skills page. Open the Add Workspace Skill dialog. Switch to the Local tab. Select the Codex fixture directory. Confirm the import after one detected skill is shown.
- Expected results: The dialog shows the Local tab, a Select Directory action, and supported-format text mentioning Claude Code, Codex, and SKILL.md. After directory selection, it reports one detected skill and a files badge. After import, a success toast says one skill was imported, the imported skill is selected in the list, and the editor panel shows the imported content.
- Notes for automation: Bind selectors for the Skills page heading, add-skill button, Add Workspace Skill dialog, Local tab, directory input, detected skill count, files badge, import button, success toast, selected skill name, and editor content before converting this case to executable syntax.

## Case LOC-002 Claude Code Commands Are Detected As Multiple Skills

- Purpose: Verify that a Claude Code commands directory is parsed into multiple importable skills.
- Preconditions: The workspace Skills page is accessible and `fixtures/skills-local/claude-basic/` contains at least two command markdown files under `.claude/commands/`.
- User flow: Navigate to the workspace Skills page. Open the Add Workspace Skill dialog. Switch to the Local tab. Select the Claude Code fixture directory.
- Expected results: The dialog remains on the Local tab and reports two detected skills. The detected skill list includes entries derived from `review.md` and `test.md`. The primary import button indicates that two skills can be imported.
- Notes for automation: Bind selectors for the Local tab, hidden directory input, detected skill count, detected skill rows, and import button. This scenario does not need to submit the batch because the key browser-observable risk is multi-command parsing.

## Case LOC-003 Unsupported Directory Shows Validation Error

- Purpose: Verify that an unsupported or empty local directory is rejected before batch import.
- Preconditions: The workspace Skills page is accessible and `fixtures/skills-local/empty/` contains no supported markdown or text skill files.
- User flow: Navigate to the workspace Skills page. Open the Add Workspace Skill dialog. Switch to the Local tab. Select the empty fixture directory.
- Expected results: The dialog shows an error message saying no skill files were found and lists supported formats such as `.claude/commands/*.md`, `AGENTS.md`, and `SKILL.md` directories. No detected skill row is shown and the import action remains disabled.
- Notes for automation: Bind selectors for the Local tab, directory input, local-tab error message, detected skill rows, and disabled import button before converting this case to executable syntax.
