# Browser Regression Guide

This suite is prepared for later execution by `agent-browser`. It covers the Skills page local-directory import feature for issue `OPE-12`; it does not execute browser tests by itself.

## Format

- `testcase/ui-selectors.json` is not present in this repository at generation time.
- `browser-regression-cases.md` therefore uses scenario format, not executable operation syntax.
- Do not treat the cases as ready-to-run scripts until selector bindings are added or a human-guided browser run converts them to executable steps.

## Scope

- Verify the Skills page exposes a Local import flow in the Add Workspace Skill dialog.
- Verify Codex, Claude Code, and `SKILL.md` local directory inputs produce browser-visible parsed skill states.
- Verify successful imports surface visible success and selected-skill state.
- Verify unsupported directories show a browser-visible validation error before import.

## Preconditions

- Use a workspace member account that can create workspace skills.
- Start from an existing workspace slug and navigate to the Skills page.
- Prepare the fixture directories named in `browser-regression-cases.md` on the same machine running the browser.
- Keep fixture skill names unique per run unless a later regression task intentionally tests duplicate handling.

## Selector Guidance

- Add `testcase/ui-selectors.json` before converting these scenarios to executable operation syntax.
- When a selector manifest exists, semantic keys should be resolved through `pages` first and `dynamic` second.
- If a manifest mapping is missing, use visible text, role, label, dialog title, toast text, and the scenario notes as fallback locator guidance.
- Concrete CSS selectors, XPath, or final `data-testid` values belong in `testcase/ui-selectors.json`, not in these generated scenario cases.

## Suggested Semantic Keys

- `skills.page_heading`
- `skills.add_skill_button`
- `skill_dialog.title`
- `skill_dialog.local_tab`
- `skill_dialog.local.select_directory_button`
- `skill_dialog.local.directory_input`
- `skill_dialog.local.supported_formats_hint`
- `skill_dialog.local.detected_skill_row`
- `skill_dialog.local.detected_skill_count`
- `skill_dialog.local.detected_skill_name`
- `skill_dialog.local.detected_skill_files_badge`
- `skill_dialog.local.import_button`
- `skill_dialog.local.import_button_disabled`
- `skill_dialog.local.error_no_skill_files`
- `skill_list.selected_skill_name`
- `skill_editor.main_content`
- `toast.skill_imported_count`

## Fixture Guidance

- `codex-basic` should include `AGENTS.md` plus at least one supporting file so the files badge can be asserted.
- `claude-basic` should include at least two `.claude/commands/*.md` command files so multiple parsed skills are visible.
- `skillmd-basic` should include a nested `SKILL.md` package and a supporting file in the same package directory.
- `empty` should contain no supported text files so the UI remains in the Local tab and shows the validation error.

## Execution Notes

- Prefer direct directory upload on the hidden Local tab directory input when the runner supports it.
- If direct directory upload is unavailable, use the visible Select Directory control with runner assistance.
- Wait for visible parsed rows, toast text, selected skill state, or validation error rather than fixed sleeps.
- Assertions should remain browser-observable and should not depend on backend implementation details beyond visible success or error state.
