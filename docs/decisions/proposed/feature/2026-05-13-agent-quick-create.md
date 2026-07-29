# Decision: Lower the cost of creating a usable agent in three independent stages

Status: proposed

## Problem

Creating an agent that can actually do something takes five steps across several pages: open the agents page and create one, fill in name, description, runtime, and model, write instructions into an empty textarea, open the new agent's detail page and attach skills one at a time, and — if the workspace does not already have the skill — go elsewhere to import it first.

Two of those steps assume knowledge the user does not have. Attaching skills requires knowing in advance which skills exist and which are needed, which presumes familiarity with the skill ecosystem. Writing instructions from an empty box leaves most users guessing at what good instructions look like. The result is that creating a working agent feels like a project rather than an action.

## Proposal

Three stages, each shippable on its own and each useful without the ones after it.

| Stage | What the user gets | Needs AI | Independent |
|---|---|---|---|
| Template | Pick a template; its skills are imported and its instructions pre-filled | No | Yes |
| Skill Finder | Describe a need; get recommended skills; import them in one click | Yes | Yes — useful anywhere skills are chosen |
| AI Create Agent | Describe a need; the agent is created with skills found and instructions written | Yes | Depends on Skill Finder |

Stages 2 and 3 reuse the existing quick-create-issue infrastructure — dispatch a task to one of the user's agents, tool calling, inbox notification on completion. No SSE, no server-side LLM client, and no new WebSocket channel.

Skill Finder is an endpoint, not a `SKILL.md`. Its recommendations come from a curated list of vetted skill URLs and summaries committed in the repository, and the model chooses from that list rather than producing URLs.

Two soft blockers sit in the way of stage 1: `createSkillWithFiles` runs its own transaction and cannot be composed into a larger one, and importing a skill whose name already exists in the workspace needs find-or-create semantics. Stages 2 and 3 each need a CLI surface that does not exist yet — `multica skill find` and `multica agent create`.

Explicitly not in scope: the Anthropic plugin marketplace, ClawHub's discovery layer, installing skills into a user's local `~/.claude/skills/`, and server-side LLM calls.

## Alternatives considered

**Integrate the Anthropic plugin marketplace.** Rejected on structural mismatch. That marketplace ships plugin bundles — a manifest plus skills, agents, hooks, and MCP configuration. Multica has standalone skills only, with no plugin or bundle concept, so integrating means writing a plugin parser and a splitting strategy. `skills.sh` already covers the same authors, since it reads from GitHub raw and the skills inside those bundles usually exist standalone in the author's repository anyway.

**Call an LLM directly from the backend.** Rejected. The server has no LLM SDK at all today — every model call goes through daemon to runtime to CLI. Adding one means new infrastructure for the client, API-key management, and streaming. Quick-create reuses the user's own agent configuration, which means their instructions, model, and runtime preferences apply automatically and the usage lands in their quota. The costs are that the user must already have an agent, and that the extra hop adds latency behind a non-blocking 202.

**Ship Skill Finder as a `SKILL.md` instead of an endpoint.** Rejected. It would have to be installed into an agent before anyone could use it, turning a standalone capability into something with prerequisite configuration. It also has nowhere sensible to call: `npx skills` installs locally, which is the wrong target, and calling the Multica API from inside a skill means building a tool channel to get back to where the request started. As an endpoint, stage 3 calls it directly and both features share one implementation.

**Search GitHub Code Search live instead of curating a list.** Rejected for the first version. A curated list keeps quality controlled, avoids the rate limit when a template imports several skills at once, and — most importantly — stops the model inventing URLs, which it will otherwise do given a knowledge cutoff. A live `search_skills(query)` tool can be added once users report the list is too narrow.

**Keep ClawHub as an import source.** Import already supports it, but there is no discovery or search UI, so a user can only paste a URL, and ClawHub serves a competing platform's ecosystem with far lower reach than `skills.sh`. It is a separate HTTP client that has to be maintained as that API evolves. Whether to retire it should be decided by counting actual usage, separately from this work.

## Acceptance criteria

Stage 1 is done when a user can create a working agent from a template in one action, with the template's skills imported into the workspace and its instructions pre-filled, and when a failure to fetch any one skill rolls the whole import back and names the URL that failed.

Stage 2 is done when a user can describe a need anywhere skills are chosen and receive recommendations drawn only from the curated list.

Stage 3 is done when describing a need produces an agent whose detail page opens in an editable state — not presented as ready — so the user confirms what the model wrote.

## Risks

GitHub rate limits when a template imports several skills; the existing `GITHUB_TOKEN` support raises the ceiling to 5,000 per hour, which is enough if production is configured with one.

A skill repository referenced by a template can be deleted by its author. Import failure must roll back and name the URL, and the curated list needs a periodic CI job that fetches every entry and alerts on failures.

A model asked for instructions can write something unusable, which is why stage 3 hands the user an editable agent rather than a finished one.

Template format will need to change. A `version` field in the template JSON lets the backend keep reading older ones.

Out of scope and unsequenced: sharing templates across workspaces, skill version management, and server-side LLM infrastructure for streaming.
