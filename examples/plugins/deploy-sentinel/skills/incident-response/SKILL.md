---
name: incident-response
description: How this team handles a production incident issue — what to establish before proposing a fix, and what an agent may and may not do on its own.
---

# Incident response

An incident issue is not a bug report. The goal is to stop the bleeding first and
explain it second, and those two are frequently in tension.

## Establish, in this order

1. **Blast radius.** Who is affected and how badly. An error rate that doubled
   from 0.01% is not the same incident as one that doubled from 4%.
2. **When it started.** Get a timestamp before you get a theory. The first
   timestamp people offer is usually when somebody *noticed*, not when it began.
3. **What changed.** Call `correlate_deploys` with the service and a window that
   starts before the timestamp from step 2. An empty result is informative: it
   means this is probably not a deploy, and you should stop looking there.

Do not skip to step 3. A deploy that landed in the window is not automatically
the cause, and the fastest way to waste an hour is to roll back the first
plausible change and watch the incident continue.

## Rollback

`request_rollback` files a change request. It does not roll anything back — a
human approves it. So filing one is cheap, and you should file it as soon as you
have a specific deploy and a reason, rather than waiting until you are certain.

The `reason` you write goes into the change record verbatim and is what the
approver reads at 3am. Write the evidence, not the conclusion:

- Good: "error rate on checkout-api went 0.2% → 6% within 90s of deploy d-4821;
  no other deploy in the window; the diff touches the retry path."
- Bad: "this deploy broke checkout."

Deploys older than the configured rollback window are refused. That is
deliberate: past that point a rollback is usually more dangerous than a fix
forward, and it needs a human deciding, not a tool call.

## What to write on the issue

Update the issue as you go rather than at the end. Someone else may be reading
it to decide whether to escalate, and an issue that goes quiet for twenty
minutes reads as "nobody is on this".

Keep the timeline in the issue description and the reasoning in comments. The
description is what someone reads six months later during a postmortem; the
comments are what your colleagues read right now.

## What not to do

- Do not change issue status to done because the error rate recovered. Recovery
  and resolution are different; an incident stays open until somebody explains
  it.
- Do not close the loop silently. If you correlated deploys and found nothing,
  say so on the issue — that is a real result and it stops the next person
  repeating it.
