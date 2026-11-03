# Project baseline CLI

`multica project baseline` implements Strixnova contract v3.3 section 7 using
the engineering API. The four chapter keys are `overview`, `domain_rules`,
`architecture`, and `exclusions_and_risks`.

```sh
multica project baseline get <project-id>
multica project baseline get <project-id> --chapter domain_rules
multica project baseline get <project-id> --chapter domain_rules --candidate --output json
multica project baseline get <project-id> --revision domain_rules:3 --output markdown

multica project baseline propose <project-id> --chapter domain_rules \
  --content-file ./rules.md --summary "Clarify settlement rules" --base 3
multica project baseline diff <project-id> --chapter domain_rules
multica project baseline diff <project-id> --chapter domain_rules --from 3 --to 4
multica project baseline adopt <project-id> --revision domain_rules:4
multica project baseline adopt <project-id> --revision domain_rules:4 --reject
multica project baseline export <project-id> --dir ./docs/baseline --output json
```

`get` defaults to chapter summaries. A selected chapter, a historical revision,
or `--output markdown` includes the Markdown body. `--candidate` selects current
candidates. Historical lookup is bounded and requires an exact chapter/version
match; a missing version reports `404 not_found` with exit code 2. JSON reads
return `engineering` and `chapters: [{chapter, revision}]`; `revision` is null
for a chapter without the selected version. Revision JSON retains server audit
fields. Historical reads return `engineering: null`.

`propose` requires a UTF-8 file under the current working directory, at most
64 KiB. Traversal and symlinks escaping that directory are rejected. A summary
is required and limited to 500 Unicode characters. With an existing adopted
version, specify `--base <number>` or explicitly choose `--base-current`.
Without either, only a first revision can be proposed. An existing candidate
requires `--replaces <number>`; inspect it before replacing. Optional
`--source-issue` accepts an issue identifier or UUID. These references are
resolved to IDs for the API; writes are never retried automatically.

`propose` and `diff` display server-computed anchor changes. Text output prints
each changed anchor's before/after body with line prefixes. It does not compute
another Diff or infer semantic warnings from words such as MUST/SHOULD. New diffs have no warnings. Historical warning codes print
`[BASELINE WARNING]` on stderr without special rule-downgrade labeling. JSON retains structured changes and warnings.
Candidate Diff JSON is `{diffs: [...]}`; historical Diff and proposal JSON are
the corresponding server objects.

`adopt` requires one to four explicit `--revision chapter:number` values, with
each chapter selected once. The CLI resolves those exact historical versions
and sends their stored `base_revision_id` as `expected_adopted_revision_id` in
one decision request. It never replaces the selected version with the current
candidate or refreshes the expected base. A concurrently replaced candidate or
changed base is rejected by the server. `--reject` uses the same selection and
atomic decision endpoint with `action=reject`.

Authorization remains with the server. Only human deciders using a session or
member PAT can adopt or reject. A task receives 403 `baseline_human_required`:
the CLI prints “基线只能由决定人采用或驳回”, adds the candidate's engineering
approval link and exits 1. If the app address or workspace slug is unavailable,
it directs the decider to the engineering overview. It never reads or switches
to another credential. `baseline_not_decider` and `baseline_credential_denied`
exit 1 without looking up an approval link.

After successful `propose` text output, stderr prints the new candidate's
engineering URL for review. The URL uses `MULTICA_APP_URL`,
`FRONTEND_ORIGIN`, or the selected profile's app URL, then `daemon_app_url` from
`GET /api/config`. Known cloud deployments can fall back to their public app
address (`https://api.multica.ai` maps to `https://multica.ai`; the fork's cloud
uses its configured cloud default). An arbitrary API address is never used as
an app address. The workspace slug comes from `GET /api/workspaces/<workspace-id>`.
If either the app address or slug is unavailable, the CLI instead asks the
decider to open the project's engineering overview on the dashboard.

The approval deep link is
`<app-url>/<workspace-slug>/projects/<project-id>/engineering?candidate=<revision-id>`.
The OL-92 Web/Desktop engineering view must preserve this workspace path and
candidate through login, then display that revision's Diff without adopting it
on navigation. A replaced or closed candidate must not silently select a newer
revision. This route is a frontend handoff contract; it is not implemented by
the CLI change.

Successful decision text prints Diff summaries followed by `open_issues` in
the server's `in_flight`/`queued` groups, including custom statuses and a
truncation notice. Decision JSON preserves the full server response. There is
no approval-comment proof or candidate UUID flag: the decision API uses explicit
chapter versions and authenticated human checks. Text output for `propose`,
`diff` and `adopt` retains the `+a ~m -r; unchanged u` summary and lists every
removed section's key and title. Historical warnings are repeated on stderr
after successful adoption, including JSON output, without changing exit code 0.

`export` writes only adopted versions to `01-overview.md`, `02-domain-rules.md`,
`03-architecture.md`, and `04-exclusions-and-risks.md`. Empty chapters have an
explicit unadopted marker. `--single-file ./PROJECT_BASELINE.md` combines all four
chapters and is mutually exclusive with `--dir`. Headers contain revision
numbers and adoption times, never audit UUIDs. Files are read-only snapshots;
edits must be proposed through the API to affect the baseline. Each file is
replaced atomically; a filesystem failure can leave earlier files updated.
Existing non-regular targets, including symlinks, are rejected. JSON output
lists exported paths, while completion notices go to stderr.

All five commands support `--output json`; text (or table) is the default.
Failures write only to stderr, without echoing baseline bodies even under
`--debug`. Exit codes are 0 for success, 1 for business/transport failures
(including 403/409/422), and 2 for invalid parameters or missing resources.
HTTP 400/404 also exit 2. Invalid, missing, or ambiguous project prefixes retain
the resolver's explanation (including matching IDs) and exit 2; failures while
fetching project candidates still use the transport/HTTP error handling.
Other CLI commands retain their existing exit conventions.


## Engineering configuration

```sh
multica project engineering get <project-id> --output json
multica project engineering set <project-id> --current-stage 9 --mode-summary "Verify release"
multica project engineering set <project-id> --stages-file ./stages.json
multica project engineering set <project-id> --clear-stages
```

`get` text output includes the enabled flag, current stage, approach summary and
a stage/title/goal table. JSON output is the original `engineering` object.
`set` requires at least one change; it does not expose enabled or decider-list
changes. `--stages-file` and `--clear-stages` are mutually exclusive (exit 2).
An empty `--mode-summary` clears the summary. Files must be UTF-8 JSON arrays
under the current directory; traversal and escaping symlinks are rejected, as
with baseline `--content-file`. Files are bounded by the API's 1 MiB request
limit; stage count and element validation are performed by the server.

```json
[{"stage": 1, "title": "Contract", "goal": "Accepted"},
 {"stage": 9, "title": "Release", "goal": ""}]
```

A task's `set` receives 403 and prints
“工程配置只能由人类决定人修改，请在工程总览设置中修改”, then exits 1.
The same 0/1/2 exit convention as baseline commands applies. Requests are not
retried and credentials are never changed.
