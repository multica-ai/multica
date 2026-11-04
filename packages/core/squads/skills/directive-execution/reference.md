# Directive execution: worked examples

## The directive

The boss said in chat: "Launch the referral programme before the end of the month, at
the current pricing, and keep the support squad out of it until it is live."

The directive quotes that, with the objective (referral programme live by 31 January),
the constraint (current pricing; support squad not involved until live), the priority
(high), and the done condition (the programme is live for all paying customers, with
tracking in place).

## Acknowledge

Weak:

> Got it.

Good:

> Understood: the referral programme live for all paying customers by 31 January, at
> the current pricing, with tracking. Support is not involved until it is live. Done
> means the programme works end to end for a new customer and the tracking shows the
> first referral. I will have a plan by tomorrow morning.

## Plan

Stages with done conditions and owners:

1. Design the referral flow and the reward rules (done: the design doc is approved by
   the product lead). Owner: the product analyst.
2. Build the referral code and the reward ledger (done: the staging test passes end to
   end). Owner: the developer.
3. Tracking and the dashboard (done: the first referral appears on the dashboard in
   staging). Owner: the analyst.
4. Release to paying customers (done: live for all paying customers). Owner: the
   release lead, through the release workflow.

What I do not know: whether the payment provider supports the reward credit. If it does
not, stage 2 changes; I will say so the day I find out.

## Decompose

Weak:

> Build it.

Good: one issue per stage, and within stage 2 one issue per piece: referral code
generation, reward ledger, credit application, and the end-to-end test. Each issue is
assigned to a member, its parent is the directive, and the credit application issue
names the provider dependency in its description.

## Milestone comment

Weak:

> Progress.

Good:

> Stage 1 done (design approved 14 January, linked). Stage 2 in progress: code
> generation and the ledger are merged; credit application waits on the provider's
> answer about reward credits (raised with the Secretary on 15 January). Date: still on
> track for 31 January if the answer comes by 20 January.

## Blocker

Weak:

> Blocked.

Good:

> Blocked on the reward credit: the payment provider's API does not support it for
> annual plans (linked provider doc). What I tried: a manual credit path in the ledger
> (works for monthly plans only). Needed: a decision on whether annual customers get
> the reward as a manual credit on renewal, or wait for the provider. If the answer
> comes after 20 January, the date moves to 14 February. Asking the Secretary.

## Close

Weak:

> Done.

Good:

> Closed: the referral programme is live for all paying customers as of 30 January. The
> first 14 referrals are on the dashboard (linked). Not included: annual-plan rewards,
> which wait on the provider; this is a follow-up that needs a decision from the boss
> through the Secretary. Evidence: the staging and production checks (linked).
