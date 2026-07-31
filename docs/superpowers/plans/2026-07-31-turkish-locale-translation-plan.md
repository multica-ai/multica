# Turkish Locale Translation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Translate all newly added user-visible Turkish locale strings and fix the current PR frontend test failure.

**Architecture:** Preserve the existing JSON locale structure and Turkish terminology. Each translation task owns a disjoint set of locale files, retains placeholders and plural suffixes exactly, and replaces English fallback values with natural Turkish. A separate task diagnoses and fixes the failing frontend test without changing unrelated upstream behavior.

**Tech Stack:** JSON locale bundles, TypeScript landing dictionaries, Vitest, TypeScript, GitHub Actions.

## Global Constraints

- Keep `issue` as `issue`; do not translate it as “sorun”.
- Use project terminology: `agent` → “ajan”, `runtime` → “çalışma ortamı”, `workspace` → “çalışma alanı”, `squad` → “ekip”, `inbox` → “Gelen Kutusu”, `board` → “pano”, `project` → “proje”, `label` → “etiket”.
- Preserve every `{{placeholder}}`, ICU interpolation, plural suffix, URL, CLI command, file path, and product name exactly.
- Do not remove locale keys to make parity pass; every EN leaf must have a Turkish counterpart and no Turkish-only leaf may remain.
- Do not modify non-Turkish locales except where a test fix proves it is required.
- Keep changes ASCII unless the Turkish translation itself requires Turkish characters.

## Task 1: Translate Core Product Locales

**Files:** `packages/views/locales/tr/common.json`, `editor.json`, `inbox.json`, `labels.json`, `layout.json`, `members.json`, `my-issues.json`, `search.json`, `workspace.json`.

- [ ] Identify every leaf whose value still equals the EN value and translate it naturally into Turkish.
- [ ] Preserve all placeholders, key names, and technical terms.
- [ ] Run `pnpm --filter @multica/views exec vitest run locales/parity.test.ts`.
- [ ] Commit with `feat(i18n): translate Turkish core locale strings`.

## Task 2: Translate Agents, Runtimes, and Skills

**Files:** `packages/views/locales/tr/agents.json`, `runtimes.json`, `skills.json`.

- [ ] Translate all newly added user-visible English fallback values.
- [ ] Keep runtime/provider/CLI names unchanged and preserve command examples.
- [ ] Run the locale parity test.
- [ ] Commit with `feat(i18n): translate Turkish agent locale strings`.

## Task 3: Translate Issues, Chat, and Modals

**Files:** `packages/views/locales/tr/issues.json`, `chat.json`, `modals.json`.

- [ ] Translate all newly added user-visible strings, including accessibility labels, failure messages, archive actions, quick actions, and issue creation fields.
- [ ] Keep `issue`, `task`, `agent`, and status identifiers consistent with existing Turkish copy.
- [ ] Preserve plural forms and interpolation placeholders exactly.
- [ ] Run the locale parity test.
- [ ] Commit with `feat(i18n): translate Turkish issue and chat strings`.

## Task 4: Translate Settings, Autopilots, and Usage

**Files:** `packages/views/locales/tr/settings.json`, `autopilots.json`, `usage.json`.

- [ ] Translate all new visible settings, scheduling, access, delivery, profile, usage, error, and leaderboard strings.
- [ ] Keep cron syntax, provider names, and technical identifiers unchanged.
- [ ] Run the locale parity test.
- [ ] Commit with `feat(i18n): translate Turkish settings and automation strings`.

## Task 5: Translate Onboarding, Projects, Squads, and Billing

**Files:** `packages/views/locales/tr/onboarding.json`, `projects.json`, `squads.json`, `billing.json`, `auth.json`.

- [ ] Translate all new user-visible strings while preserving onboarding variables and account/payment terminology.
- [ ] Keep product names, URLs, and placeholders unchanged.
- [ ] Run the locale parity test.
- [ ] Commit with `feat(i18n): translate Turkish onboarding and workspace strings`.

## Task 6: Fix Frontend Test Failure

**Files:** Determine from the current GitHub Actions `frontend-test` log; modify only the failing test or its directly affected implementation.

- [ ] Reproduce the failing test locally from the exact current branch.
- [ ] Trace the failure to its root cause instead of weakening the assertion.
- [ ] Implement the smallest compatible fix.
- [ ] Run the specific failing test and the relevant package test suite.
- [ ] Commit with `fix(test): resolve frontend test failure`.

## Task 7: Final Verification

- [ ] Run `pnpm --filter @multica/views exec vitest run locales/parity.test.ts`.
- [ ] Run `pnpm typecheck`.
- [ ] Run the relevant frontend test suite from Task 6.
- [ ] Check `git diff --check` and scan for conflict markers.
- [ ] Review the PR diff and push the branch.
