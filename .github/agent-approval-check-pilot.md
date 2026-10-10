# Agent approval check pilot

This repository is piloting Anthropic's `agent-approval-check` in observation mode.

## What counts as agent-authored

The check marks a PR as agent-authored when either of these signals appears:

- a commit or `Co-authored-by` trailer uses one of the configured agent emails
- the PR author or an `APPROVED` review matches one of the configured agent bot logins

Human-only PRs are unaffected. During the pilot we are **not** making this a required branch protection check.

## Identities included in this pilot

See `.github/agent-identities.yaml` for the source of truth. The first-pass map covers:

- `multica-agent` / `github@multica.ai`
- `claude[bot]`, `claude-code[bot]`, `noreply@anthropic.com`
- `openai-codex[bot]`, `noreply@openai.com`, `codex@openai.com`
- `hermes-seaeye[bot]`, `hermes@nousresearch.com`

## Promote-to-required follow-up

Promote `agent-approval-check` to a required status only after reviewing a sample of real PRs for both error classes:

1. **False positives**: human-only PRs incorrectly flagged as agent-authored.
2. **False negatives**: agent-authored PRs that bypass the check because the emitted login or email is missing from the map.

Promotion bar for this repo:

- zero human-only false positives in the observation sample
- no missed `multica-agent`, Claude, Codex, or Hermes-authored PRs in the same sample
- native GitHub review requirements remain enabled alongside this check
