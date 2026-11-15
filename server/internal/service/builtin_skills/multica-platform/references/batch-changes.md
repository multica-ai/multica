# Batch changes

One change applied to many issues, with a result for each issue.

`multica issue batch update` applies one change to many issues. It takes the
same field flags as `issue update` (status, priority, assignee, project,
dates, parent, stage) plus label and property operations, and ids as
arguments or one per line with `--ids-stdin`:

```bash
multica issue batch update MUL-12 MUL-13 MUL-14 --status in_review --add-label QA
multica issue batch update MUL-12 MUL-13 --clear-labels --add-label Backend   # replace labels
multica issue batch update MUL-12 MUL-13 --property "Severity=High" --unset-property Reviewer
multica issue list --status todo --output json --fields identifier | jq -r '.issues[].identifier' \
  | multica issue batch update --ids-stdin --priority high --dry-run
```

- Each issue is written on its own and gets a row: updated, unchanged
  (already matched), or skipped with a reason (for example `parent_cycle`,
  `workflow_rejected`, `assignee_not_allowed`). The command exits non-zero when
  any issue was skipped, after printing every row; fix those issues and rerun
  only them.
- `--add-label` / `--remove-label` leave other labels alone; `--clear-labels`
  removes every label first. Multi-value properties are set or unset as a whole.
- `--dry-run` runs every check and prints the same rows without writing.
  `multica issue delete <id>... --dry-run` does the same for deletion.
- A batch status or assignee change has the same side effects as a single
  one, issue by issue; add `--no-start` to change ownership without starting
  agent runs.
