# Secretary evaluation rubric

The rubric the spec sets out under "Testing and evaluation", turned into yes/no checks. The
boss can read it. Each scenario in [scenarios.json](scenarios.json) names the rubric items it
exercises in its `properties`; the score sheet is written by [run-eval.ts](run-eval.ts).

## Scale

Each dimension has checks. Score the dimension from its checks, not by feel:

| Score | Meaning |
|---|---|
| 2 | Every applicable check is yes. |
| 1 | At least one applicable check is yes, and not all are. |
| 0 | No applicable check is yes. |
| N/A | The scenario does not contain what the dimension tests (for example no quiet squad for R5). N/A does not count in the total. |

The total is the sum over applicable dimensions, out of 2 times the number of applicable
dimensions. Report it as a fraction and as a percentage.

Checks marked **[auto]** are computed by the runner in live mode. Everything else is a
manual read of the Secretary's output and the cited records. In plumbing mode no Secretary
output exists, so every dimension is marked manual and the runner records only the wiring
checks in [run-eval.ts](run-eval.ts).

## R1. The answer names the real problem

- R1a. The first sentence answers the boss's question, in the boss's words.
- R1b. The central fact of the scenario is named: the hidden dependency, the unowned risk, the
  stale queue, the quiet squad, or the metric that is behind. **[auto]** every `must_mention`
  term appears in the assessment (a keyword check; a human confirms the meaning).
- R1c. Activity counts (issues closed, runs) do not stand in for the answer.

## R2. Completion and outcome are justified

- R2a. The completion or outcome is a range or a reading, with the reason for each end.
- R2b. The reason is a fact in the record (a count, a date, a metric reading), not a feeling.
- R2c. No bare percentage, and no "on track" or "green", without support in the same paragraph.

## R3. Risks are ranked and owned

- R3a. Each risk has a cause, a likelihood or a sense of it, and an impact.
- R3b. Each risk names an owner, or says it has none and who should own it.
- R3c. The risks are in an order, with the reason for the first one.
- N/A when the answer lists no risks and the question did not ask for them.

## R4. Claims are traceable

- R4a. **[auto]** The assessment links to at least one issue, comment, run or metric.
- R4b. Each key claim has a source, or is marked as an inference or as not checked.
- R4c. An unconfirmed claim is not presented as fact.

## R5. Economy and skipping

- R5a. **[auto]** Each squad the scenario says to skip is named in the answer with a skip word
  (skipped, not asked, left out).
- R5b. A squad the scenario marks as forbidden was not asked. **[auto]** from the child issues
  the runner read back (the wiring check).
- R5c. The skip is said plainly, not dropped silently.
- N/A when the scenario has no squad to skip.

## R6. Weak answers are challenged

- R6a. The weak, contradicted or stale answer is named, with its squad.
- R6b. The challenge is a comment on the child issue, not only words in chat. **[auto]** in live mode.
- R6c. The answer does not accept the leader's status over the contrary evidence.
- N/A when the scenario has no weak answer (`challenge_required` is false).

## R7. Short for a busy boss

- R7a. **[auto]** The assessment is within the scenario's `max_words` (default 350).
- R7b. The first line is a headline the boss can read in ten seconds.
- R7c. Any decision needed from the boss is numbered, so the boss can reply "D1: B".

## R8. The language is the boss's

- R8a. Squads, projects and metrics are named as the company names them, with no internal ids
  in the prose.
- R8b. **[auto]** The text contains none of the platform's internal terms (`child_done`,
  `wakeup`, `origin_id`, `squad_request`, `chat_session_id`).
- R8c. What is not known is said in plain words, once, not hedged in every sentence.

## Regression rule

A change to a skill is accepted only when, on the same scenarios and the same mode:

- no applicable dimension scores lower than in the previous saved run, and
- the total does not fall by more than one point.

Record each run in the runbook with the skill versions it used (the runner writes
`skills.json` with a hash of every shipped skill file beside the outputs).
