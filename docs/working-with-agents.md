# Working with Multica Agents — What to Expect

Audience: anyone who assigns issues to agents, comments on their work, or
chats with them — without configuring or authoring them. If you *build*
agents, read the [operator guide](./agent-runtime-context-humans.md); this
guide is about being a good customer of one.

---

## 1. The trust model — read this before your first assignment

An agent run has **full tool permissions and no ability to ask you
anything**. Permission prompts are bypassed and the "ask the user" tool is
disabled by the platform. When an agent hits an ambiguity, it decides
autonomously; when it is truly stuck, it marks the issue `blocked` and
explains why in a comment — it never pauses to check with you mid-run.

Practical rule: **scope requests the way you'd brief a contractor who has
root access and no phone.** Say what "done" looks like, name the exact repo
and branch, state what must NOT be touched. A vague destructive request
("clean up the old deploy stuff") gets executed, not clarified.

## 2. Lifecycle of an assignment

What you'll see, in order, after assigning an issue to an agent:

1. **Queued** — the run waits for a free slot. Usually seconds, can be
   longer if the workspace is busy.
2. **`in_progress`** — the agent picked it up. It reads the issue, its
   metadata, and the 10 most recent comments before doing anything.
3. **Work happens invisibly.** You do not see terminal output or partial
   progress. No news mid-run is normal.
4. **Exactly one result comment** — the agent's entire deliverable. Agents
   are instructed to post one final comment per run, never progress updates.
5. **`in_review`** — the agent is done and waiting on *you*. Or `blocked`,
   with the blocker explained in the comment.
6. **You close it.** Agents never move issues to done — reviewing the result
   and closing (or commenting with follow-up) is the human's job.

Commenting on the issue after step 5 triggers a **new run** that answers
your comment. If you post several comments quickly, they may be *coalesced*:
one run answers all of them in a single reply. Your second comment not
getting its own dedicated answer is by design, not neglect.

## 3. What the agent remembers (and what it doesn't)

Each run starts **fresh**. There is no standing memory of past
conversations. The only continuity between runs on the same issue:

- **the issue itself** — title, description, comment history,
- **issue metadata** — a small key-value scratchpad agents use for durable
  facts like a PR URL or a blocker reason.

Consequences:

- Don't write "as we discussed" or "like last time" — restate the fact.
- Context from a *different* issue is invisible. If MUL-101 established a
  decision that MUL-102 needs, put it in MUL-102's description or a comment.
- Chat conversations and issue work are separate worlds; a chat agreement
  does not carry into an assignment.

## 4. What code the agent sees

The agent checks out repos into a **fresh worktree of the remote's default
branch** (`main`/`master`) unless a specific ref is named. It never sees:

- your local working copy or anything uncommitted,
- branches you haven't pushed,
- pushed branches you didn't name.

If the work builds on a branch, **name it explicitly** in the issue or the
handoff note: "branch off `feature/api-v2`, based on commit `abc123`".

## 5. Silence, mentions, and how threads end

Two behaviors that look broken but are correct:

- **Silence is a designed response.** If your comment was a pure
  acknowledgment ("thanks, looks good"), the agent may run and post
  *nothing*. Agents are told not to post "no reply needed" filler. A silent
  run after your thank-you is the thread ending, not the agent dying.
- **Every `@mention` of an agent starts a new run.** Mentions are triggers,
  not decoration. Mentioning an agent "for visibility" costs a full run;
  two agents mentioning each other as sign-offs loop forever (they are
  instructed not to, but don't invite it). Mention an agent only when you
  want it to *do something now*.

Mentioning a **human** sends them a notification. Plain issue links
(`MUL-123`) are side-effect-free.

## 6. Troubleshooting

| Symptom | Likely cause | What to do |
| --- | --- | --- |
| Agent ran, nothing appeared on the issue | Run failed before the result comment, or the work happened but the comment step was skipped | Check the run's status in the agent activity view; re-trigger with a comment asking for the result |
| Issue stuck `in_progress` for a long time | Run is genuinely long, or it died mid-run | Long builds/tests are synchronous by design — give it time; if clearly dead, comment to trigger a fresh run |
| Agent went silent after my reply | Your reply read as an acknowledgment — silence is the designed thread-end | Nothing. If you wanted action, phrase it as a request |
| My comment never got its own answer | Coalesced into an earlier run's reply, or answered together with adjacent comments | Read the latest agent comment fully; it likely covers yours |
| Quick-create produced no issue | Quick-create is one-shot and never retries; the error went to your inbox notification | Check the inbox notification, then create again (nothing was half-created) |
| Agent worked on the wrong branch / stale code | No ref was named, so it used the remote default branch | Name branch + base commit explicitly and re-assign |
| Two agents replying to each other in a loop | Sign-off mentions between agents | Remove the mention chain (edit/stop), report the agent pair to its owner |
| Agent did something out of scope | The request left the boundary implicit — agents can't ask for clarification | Tighten the issue description; ask the agent owner to add the restriction to the agent's identity instructions |

## 7. Examples

**A good issue for an agent assignee:**

> **Title:** Add retry with backoff to the webhook sender
>
> **Description:** In `svc-notify` (branch `main`), webhook POSTs in
> `internal/sender/` fail permanently on the first 5xx. Add retry: 3
> attempts, exponential backoff starting at 2s, only for 5xx and network
> errors. Do not retry 4xx. Done = unit tests for both paths pass and a PR
> is opened against `main`. Don't touch the queue consumer.

Named repo, named branch, explicit done-criterion, explicit boundary. The
agent has everything it needs and nothing to guess.

**A bad one:** "Webhooks are flaky, can you make them more robust?" — the
agent will pick *a* definition of robust, and it can't ask you which.

**A good handoff note** (when assigning on someone's behalf):

> Only touch `internal/sender/`; the flakiness in the consumer is tracked
> separately in MUL-88. Base your work on branch `feature/notify-v2`.

**Asking for changes on a result:** comment on the issue like you'd review
a PR — concrete, bounded, one comment per round:

> The backoff cap is missing — cap total retry time at 30s. Everything else
> is good; keep the tests as they are.

## 8. Glossary

| Term | Meaning |
| --- | --- |
| **Run / task** | One agent execution: triggered, works, posts (or stays silent), exits. Agents don't idle between runs |
| **Task kind** | What triggered the run: assignment, comment, chat, quick-create, or autopilot. Determines where output goes |
| **Agent Identity** | The agent's configured instructions — its standing orders, written by the agent's owner. On assignments they override the platform workflow |
| **Runtime owner** | Whose account/token the agent acts under. Everything the agent can read or write is scoped to this — not to you |
| **Task initiator** | Who triggered this run (you, when you assign or comment). The agent attributes the request to you but doesn't gain your access |
| **Issue metadata** | Per-issue key-value scratchpad; the only agent memory that survives between runs |
| **Handoff note** | Free-text scoping instruction attached when assigning; the agent obeys it but doesn't reply to it |
| **Coalescing** | Comments arriving while a run is starting get answered together in one reply |
| **Autopilot** | A scheduled/webhook/manual trigger that runs an agent without any issue |

---

*Deeper reading: [runtime overview](./agent-runtime-user-guide.md) — how the
platform assembles agent context; [operator guide](./agent-runtime-context-humans.md)
— configuring agents, skills, and workspace context.*
