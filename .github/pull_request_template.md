## Summary

<one-line problem statement>

**Approach:** <2–3 sentences on the chosen solution and why>.

Closes #<github-issue-number-if-any>

---

## Related Issue

- **Multica issue:** <multica issue URL or N/A>
- **Impl-plan / design doc:** <link or N/A>
- **GitHub issue (if any):** #<n>

---

## Type of Change

- [ ] feat — new feature
- [ ] fix — bug fix
- [ ] refactor — non-behavioral code change
- [ ] perf — performance improvement
- [ ] docs — documentation only
- [ ] test — tests only
- [ ] chore — tooling, deps, config
- [ ] ci — CI/CD pipeline

(check exactly one)

---

## Changes Made

### Backend
- `<path/to/file>` — <what changed>

### Frontend
- `<path/to/file>` — <what changed>

### Database / Migrations
- `<path/to/file>` — <what changed>

### Tests
- `<path/to/file>` — <what changed>

### Docs / Config / CI
- `<path/to/file>` — <what changed>

(omit empty subsections)

---

## Plan Adherence

- [ ] Implementation matches the linked plan / design doc
- [ ] Deviations from plan (explain below)
- [ ] N/A — no plan was required

**Deviations:** <none, or bullet list of what changed and why>

---

## How to Test

**Automated:**
```
<gate command(s), e.g. `pnpm test --filter=server`>
```
Result: <PASS/FAIL summary, coverage delta if relevant>

**Manual repro:**
1. <step>
2. <step>
3. <expected outcome>

---

## Risks & Mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| <e.g. migration is non-reversible> | Low | <e.g. backup taken; rollback path documented> |

(use `None identified.` if truly low-risk)

---

## Checklist

- [ ] Code follows project style (linter passes)
- [ ] Tests added/updated and passing locally
- [ ] No new warnings introduced
- [ ] DB migrations are reversible (or documented as one-way)
- [ ] No secrets, keys, or PII in diff
- [ ] Docs updated (README, ADRs, API contracts) where applicable
- [ ] Backward-compatible (or breaking change called out above)
- [ ] Feature flag in place for risky changes (or N/A)

---

## Linked PRs

(multi-repo stories only — list sibling PR URLs; omit section if single-repo)
- <repo>: <PR URL>

---

## Screenshots

(UI changes only — before/after, mobile + desktop if relevant; omit section otherwise)

---

## AI Disclosure

If any portion of this PR was authored or assisted by AI, declare it here:

- **Tool:** <e.g. Perplexity Computer / Claude / Copilot / N/A>
- **Scope:** <which parts — code, tests, docs, all>
- **Driven by:** <prompt or design doc link>
- **Human review:** required before merge

(write `Not AI-assisted.` if entirely human-authored)

---

<!--
This is the canonical Memphis Tours / Multica PR standard.
CodeRabbit auto-appends its `Summary by CodeRabbit` block — do not include it yourself.
Required sections: Summary, Related Issue, Type of Change, Changes Made, Plan Adherence,
How to Test, Risks & Mitigations, Checklist, AI Disclosure.
Optional sections: Linked PRs (single-repo stories), Screenshots (non-UI changes).
-->
