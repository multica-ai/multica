# Issue activity token usage

Run this read-only report from a checkout with Node.js 22 and an authenticated
Multica CLI. In an agent task, it uses the injected `MULTICA_CLI_PATH` and task
credential. It does not write to Multica, change agent instructions, or start runs.

```sh
node scripts/issue-activity-usage.mjs <issue-key>
node scripts/issue-activity-usage.mjs <issue-key> --local-rollouts
node scripts/issue-activity-usage.mjs <issue-key> --local-rollouts --output json
node scripts/issue-activity-usage.mjs <issue-key> --rollout '<run-id>=/path/to/rollout.jsonl'
```

The first command reports existing issue totals and coverage. `--local-rollouts`
also looks for Codex logs under the returned runs' task-local `codex-home/sessions`
directories on this machine. It never searches the host owner's provider home.
Missing local files leave tokens unclassified. An explicit `--rollout` must name
a run on this issue and its session metadata must match the API's run session.
Use only local logs you are authorized to read; another runtime's files may not
be available. There is no raw transcript export: JSON evidence contains token
counters, models, activity labels, and source line numbers, without prompts or
tool arguments. Local paths and run IDs remain in the JSON report.

## Reading the result

- Token counters come from the existing `issue usage` and `issue runs` APIs.
  Scope is this issue, all reported attempts and statuses, including failures and
  cancellations. Child issues and direct-room chat runs are excluded.
- Activity attribution is **a rule-based estimate**, not a provider measurement
  of intent. Each Codex `token_count` snapshot is subtracted from its preceding
  cumulative snapshot within the run window. A resumed session requires its
  pre-run baseline. API completion timestamps at second precision include that
  entire second; per-model reconciliation still has to match exactly.
  Duplicate snapshots add no tokens. Counter resets and
  incomplete records are exposed and prevent activity attribution for that log.
- Development covers patches, commits, and local build/test commands. Planning
  covers explicit source reads and investigation commands. Agent communication
  covers issue comments, handoffs, assignments, and execution-state queries. CI
  checks cover explicit `gh`/`glab` pipeline, job, and check queries. This counts
  the AI response performing the check, not CI runner time or compute cost.
- Mixed batches, shell scripts/pipelines, dynamic commands, unsupported tools,
  and responses without a recognized tool action stay **other/unclassified**.
  A read during development can be labeled planning; inspect the JSON's source
  line evidence when deciding whether that attribution fits your workflow.
  Context inherited from earlier activities is charged to the current model
  response, so these figures do not measure a causal saving from removing it.
- Codex input includes cache details. The report subtracts cache reads and writes
  from input, matching `server/pkg/agent/codex.go`; output already includes
  reasoning. Claude and other providers' stored API buckets are already
  normalized and remain unclassified without supported call-level records.
  The script does not reinterpret their raw provider counters.
- Call totals must match **every counter for each reported provider/model** before
  any activity attribution is used. A mismatch stays visible and the whole model's
  reported usage stays unclassified; counts are never scaled to force a match.
  Activity totals are reconciled to run totals, and run totals to `issue usage`.
  If API snapshots disagree, percentages are withheld; rerun after completion.
- Percentages use the sum of disjoint input, output, cache-read, and cache-write
  tokens reported by Multica. They are **not cost percentages**. A zero attributed
  bucket means no tokens were assigned by these rules, not proof no such activity
  occurred. Runs without usage are `null`/`미보고`, and active or unreported runs
  make the issue total a lower bound. A reported zero remains a genuine zero.
  Missing prices and `uncosted_*` counters must not be interpreted as free usage.

The table shows the five largest reported runs. JSON includes per-model
reconciliation and per-response evidence for examining large activities. Compare
cache-read volume, uncached input, output, and unclassified coverage separately
before proposing smaller context reads, fewer repeated queries, or shorter
handoff messages. The report does not estimate money or promise a saving.

```sh
node --test scripts/issue-activity-usage.test.mjs
```
