# Secretary analysis: worked examples

## Writing an ask

Weak (no context, asks for everything, pre-digests nothing and asks for nothing specific):

> Send me the status of the checkout project.

Good (the question, why now, what is known, what is wanted, when):

> The boss is deciding on Thursday whether to move the checkout launch from 14 to 28
> November. Give the boss your honest completion range for the checkout project and the
> reason for each end of the range. I know the payment provider sandbox passed on 2
> November; I do not know the state of the tax rules. Name the two risks you would
> watch most, with an owner and what would raise the likelihood. Answer by 17:00 today.

## Reading an answer

Weak answer:

> Checkout is 80 percent done and on track. Risks are low.

What to do: open the cited issues, see the open items, and challenge in a comment:

> You say 80 percent. Against what plan: the acceptance list in the project, or the
> story count? Which items are open? Your risk list is empty. Name the one thing that
> would move the date.

Good answer, same squad:

> Completion 65 to 75 percent against the acceptance list (11 of 16 items done; see
> the linked list). Items open: tax calculation for two regions (blocked on the
> provider, owner Finance Lead), refund flow (not started). Schedule: at risk for 14
> November; on track for 28 November if the provider answers by 10 November. Risks:
> provider answer late (owner: Build lead, likelihood medium, impact: date slips a
> fortnight). I did not check the load test; the last run is from 30 October.

What you do with it: accept the range and the reasons, take the provider risk into
the assessment, and say the load test was not checked.

## Across answers

Two leaders disagree: the build squad says the release is green, the release squad's
queue shows three failed deploys since Monday. Name both, say which one the evidence
supports, and say what you could not confirm. Do not average them.

## Writing a decision

Weak:

> Some decisions for you: checkout, the supplier, and hiring.

Good:

> D1. Move the checkout launch: 14 November (on the current risk, 40 percent chance of
> a clean launch) or 28 November (on track, owner Release lead). Recommendation: 28
> November. The owner is the boss.
>
> D2. Supplier contract: sign the 12-month term now, or the 3-month term at 15 percent
> more. Recommendation: 3-month term; the 12-month term is not needed before the
> provider answer.

## Writing a directive

The boss said in chat: "Go with 28 November for checkout, and tell Release to hold the
14 November date." The directive quotes that, states the objective (launch 28
November), the constraint (no launch before the provider answer), the priority and the
date, the report it came from, and what done means (the launch is live and the
checklist is signed). The directive is for the Release lead, not for the squad as a
whole.

## Challenge limit

After eight challenges on one child issue with no change, close it as unresolved and
say in the assessment what is missing and who holds it. Do not keep asking.
