# Phase 5 runbook: the Secretary as chief of staff, on the boss's machine

These steps cannot run in CI. They need Hermes through the daemon on the machine the boss
works on, with a model configured in Hermes itself. The spec
([secretary-delegation.md](../../secretary-delegation.md)) and the plan
([secretary-delegation-plan.md](../../secretary-delegation-plan.md), 13.8.3) are the reference.
Each step lists its expected result. No step has been run yet, so every row starts as **not
verified**. The boss signs off each one by writing the date and the observed result in the
Sign-off column, or by recording why it could not be done.

The API part of phase 5 is automated in `e2e/phase5-secretary.spec.ts`. Its skipped steps
point here. The evaluation set in `e2e/secretary-eval/` is the other half: see section 7.

## Prerequisites

- Phase 3 runbook section 1 is done, and the Secretary exists (Company setup has run).
- Hermes is online through the daemon: `multica daemon status` reports it running, and the
  Hermes runtime shows online in the Company setup checklist.
- Hermes memory is intact: the Secretary's memory lives on this machine.
- Company controls are running (not paused, not in quiet hours) for sections 2 to 6.
- The boss is an owner or admin of the workspace.

## 1. Hermes is online

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 1.1 | Run `multica daemon status`. | The daemon is running and the Hermes runtime is online. | not verified |
| 1.2 | In the Company, open the Secretary's chat and send "Are you there?". | The Secretary replies within a minute. Its reply is a chat message, not an error. | not verified |
| 1.3 | Send a second message straight after the first. | It queues behind the first run and the chat says so. Both are answered in order. | not verified |

## 2. A real question that needs two squads

Choose a question that a person would ask, and whose answer sits in two squads. Example: "Is the
spring launch on track, and what would slip it?"

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 2.1 | Ask the question in the Secretary's chat. Note the time. | The Secretary says in one or two lines whom it asked and about what, and when to expect the answer. It does not wait in the chat. | not verified |
| 2.2 | Open the Secretary desk (the Secretary's project). | A parent request is assigned to the Secretary, with one child per addressee, each assigned to a squad or a chief. The children carry the parent. | not verified |
| 2.3 | Open each child issue. | The leader has answered in a comment, with links to the issues or runs it read. Each answer is the leader's own assessment, not a copy of the Secretary's question. | not verified |
| 2.4 | Wait for the last child to close, or for the deadline. | The Secretary is woken once, and writes its assessment into the chat with `multica chat reply`. | not verified |
| 2.5 | Read the assessment. | The answer comes first. It names the decisions, ranks the risks with owners, says what was not checked, and ends with the run and token cost of the request. | not verified |
| 2.6 | Open two links the assessment cites. | Each link opens the issue, comment or run it names, and it says what the assessment claims. | not verified |

## 3. A challenge

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 3.1 | Read the leader answers from section 2 critically. Find one claim that is vague or unsupported, or ask a question that a leader's answer does not answer. | Either the leader answer is good enough, or the Secretary challenges it. | not verified |
| 3.2 | If a challenge was needed, open the child issue. | The Secretary has commented on the child issue, and the leader is woken to answer again. The challenge is a comment, not only chat text. | not verified |
| 3.3 | Count the challenges on one child. | No more than eight. If the limit is reached, the Secretary closes the item as unresolved and says so. | not verified |

## 4. A hand-over

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 4.1 | Reply in chat with a decision in ordinary words, for example "D1: go with B, and hold D2". | The Secretary restates what it understood for each decision and asks if it is wrong. | not verified |
| 4.2 | Confirm with "yes". | One directive per decision is created, each an issue for the addressee leader in the squad's project. | not verified |
| 4.3 | Open a directive issue. | It carries your own words, with the quote of the chat message, the objective, the constraints and the priority. | not verified |
| 4.4 | Open the same leader's issue after a few minutes. | The leader has acknowledged in a comment, planned, and created sub-issues in the project. It keeps the Secretary informed at milestones. | not verified |
| 4.5 | Try to hand over a proposal approval (for example "approve P-12") in chat. | The Secretary does not decide it. It points to the Approve and Reject buttons. | not verified |

## 5. A leader question relayed

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 5.1 | On a directive issue, have the leader comment that it needs a decision it cannot make, mentioning the Secretary. | The Secretary is woken, and does not ask you directly in the first place. | not verified |
| 5.2 | Check the Secretary's answer. | It answers from what it knows, or asks another leader, or brings the question to you in chat with a recommendation, and relays the answer. | not verified |
| 5.3 | Ask the leader to ask you directly by writing your name, without the Secretary. | Nothing reaches you through the leader's own chat unless you open it. The leader's comment goes to the Secretary's queue. | not verified |

## 6. A skill edit takes effect

| # | Step | Expected result | Sign-off |
|---|---|---|---|
| 6.1 | In the Skills page, open `secretary-analysis` and add a line such as "Always lead with decisions". Save. | The skill is saved, and its version changes. | not verified |
| 6.2 | Ask the Secretary a question that has an assessment. | The next answer leads with decisions. A run started before the edit is not changed. | not verified |
| 6.3 | Restore the skill to the starter text. | The next answer no longer leads with decisions. A re-run of company setup does not overwrite your edit (13.6.6). | not verified |

## 7. Timing (S5) and the evaluation set

Record the minutes from the question to the answer for each section 2 question. This is the
measurement for spike S5 and for the Hermes concurrency risk.

| # | Question | Sent at | Answer at | Minutes | Squads asked | Runs and tokens reported | Sign-off |
|---|---|---|---|---|---|---|---|
| 7.1 | Breadth 1: one squad | | | | | | not verified |
| 7.2 | Breadth 2: two squads (section 2) | | | | | | not verified |
| 7.3 | Breadth 3: a floor-wide question | | | | | | not verified |

A chat turn should end in under a minute. A cross-squad answer is expected in the time the
deadline sets (30 minutes for a status question, 2 hours for analysis). If an answer takes
longer, record why: the leader, the Secretary, or the queue behind a run.

The evaluation set is the regression check for skill changes. Run it on the boss's machine
with a real model only when the boss asks for it:

```bash
SECRETARY_EVAL_LIVE=1 node e2e/secretary-eval/run-eval.ts --mode live
```

It needs `MULTICA_TOKEN`, `MULTICA_WORKSPACE_ID`, `SECRETARY_EVAL_RUNTIME_ID` and
`SECRETARY_EVAL_SECRETARY_ID` in the environment. Plumbing mode (no model, scripted leaders)
runs without `SECRETARY_EVAL_LIVE`. It is the check that the wiring works, not the acceptance.
Score each run with [rubric.md](../../../e2e/secretary-eval/rubric.md). Results land in
`e2e/secretary-eval/results/`, which is not committed. Record the run's date, the mode, and
the totals here. A regression on the rubric blocks a skill change.

| # | Run | Mode | Skill versions (`skills.json`) | Total | Signed off by |
|---|---|---|---|---|---|
| 7.4 | First live run | live | | | not verified |

## Sign-off

| Section | Date | Observed result | Signed off by |
|---|---|---|---|
| 1 Hermes online | | | not verified |
| 2 Two-squad question | | | not verified |
| 3 Challenge | | | not verified |
| 4 Hand-over | | | not verified |
| 5 Leader question relayed | | | not verified |
| 6 Skill edit | | | not verified |
| 7 Timing and evaluation | | | not verified |
