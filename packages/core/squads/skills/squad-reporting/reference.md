# Squad reporting: good and weak examples by dimension

Each dimension shows a weak answer and a good answer to the same question: "How is the
billing squad doing against the Q4 launch?"

## Completion

Weak:

> 80 percent complete.

Good:

> Completion 60 to 70 percent of the Q4 acceptance list (9 of 15 items done; see the
> linked list). The range is wide because the two items left in the invoicing stream
> have no estimate yet; the reason for the low end is that. Remaining: invoicing
> export, reconciliation job, and the tax report.

## Schedule

Weak:

> On track.

Good:

> Slipping by about a week for the 14 December date. The invoicing export has been in
> review for six days; the review queue for the squad has four items older than three
> days. If the export lands by 6 December, the date holds. Confidence: medium.

## Quality

Weak:

> Quality is good.

Good:

> Twelve runs in the last two weeks, two failed on the same flaky payment test (linked
> issue). Review found three blocking issues in the export code; all three are closed
> with a test. I did not check the staging data for reconciliation; the last test run
> there is from 3 weeks ago.

## Risk

Weak:

> Some risk with the provider.

Good:

> Risk 1. The payment provider has not confirmed the new API version. Cause: their
> release calendar moved. Likelihood medium. Impact: the export can't be tested
> end-to-end, so the date slips by up to two weeks. Owner: Billing lead. Response:
> ask the provider for a sandbox date by 10 December; if none, use the old API and file
> a follow-up. Needs the boss: no.
>
> Observed, not suspected: the provider sandbox returned errors on 3 December (linked
> run). Suspected: the calendar move is the cause; I have not confirmed it.

## Dependencies

Weak:

> We depend on the platform squad.

Good:

> Waiting on the platform squad for the webhook change (their issue, linked). Asked
> them on 1 December; no reply. It is overdue against our 5 December need. Without it,
> the reconciliation job can run but not sync. Owner of the ask: me.

## Capacity and cost

Weak:

> The team is busy.

Good:

> Two of our five developers are on the export; the third is covering support until
> 10 December. Budget: the squad has spent 62 percent of its monthly limit with two
> weeks left; at the current run rate it reaches 90 percent. Nothing needs the boss
> yet; if the export slips, the run rate will rise and I will raise it.

## Outcomes

Weak:

> Revenue is going up.

Good:

> Goal: monthly recurring revenue from the billing product, target 120,000 by 31
> December. Reading: 96,000 on 3 December, up from 91,000 on 26 November. The trend
> reaches about 110,000 at the current rate, which misses the target. The cause is the
> trial conversion (linked report), not churn. Confidence: medium; the November figures
> are not yet reconciled with the finance squad.

## Confidence

Weak:

> I'm pretty sure.

Good:

> Confidence in the date: low. The estimate rests on the export review finishing this
> week, which I have not seen. What would raise it: the review done, and the provider
> sandbox date.

## Recommendations

Weak:

> We should improve testing.

Good:

> Recommendation: add a reconciliation test on the staging data before 10 December.
> Owner: the QA member on the squad. I will do it without the boss. Needs the boss: no.
