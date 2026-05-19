# AI Skill Finder and Agent Draft Design

Date: 2026-05-19
Status: Ready for user review

## Summary

Agent templates reduced the blank-page problem for creating agents, but two related onboarding problems remain:

- Users often do not know which skills exist or which ones fit their goal.
- Users often do not know how to write a useful agent profile and instructions.

This design introduces two staged capabilities:

1. **AI Skill Finder**: a user describes the job they want done, a host agent recommends relevant skills from a curated index, and the user imports selected skills.
2. **AI Agent Draft**: a user describes the agent they want, a host agent drafts the agent profile, instructions, and skill recommendations, and the user reviews before creating or finalizing the agent.

Both capabilities reuse Multica's existing daemon-driven agent task model. The backend does not call an LLM directly and does not introduce SSE, server-side model keys, or a new streaming channel.

## Goals

- Add an AI-assisted path for discovering skills.
- Add an AI-assisted path for drafting an agent.
- Reuse the quick-create task architecture: enqueue a task, let a configured agent run locally or in its runtime, collect structured output, notify the user.
- Keep users in control: AI output is reviewable before importing skills or creating/finalizing an agent.
- Use a curated skill index so the model cannot invent arbitrary install URLs.
- Ship in phases so Skill Finder has standalone value before Agent Draft.

## Non-Goals

- Server-side LLM calls.
- Streaming progress UI beyond existing task/inbox mechanics.
- Real-time GitHub Code Search or marketplace crawling.
- Installing skills directly into a user's local agent home directory.
- Reintroducing the old template chooser inside the create-agent dialog.
- Supporting users with zero usable host agents for AI-assisted flows. Users can create their first agent through the existing manual/template path first.

## Current State

Relevant existing pieces:

- Agent templates exist on the backend and API surface.
- The create dialog currently supports manual creation and duplication.
- Quick-create issues already enqueue an agent task with a structured context payload and complete through daemon-side CLI/tool output.
- The daemon can build custom prompts based on task context.
- The inbox already communicates async completion outcomes to users.
- The skill import endpoint already supports GitHub and skills.sh URLs.

The planned `docs/agent-quick-create-plan.md` Phase 2/3 direction remains valid, but Phase 1 has evolved: templates exist, and the old create-dialog template chooser was intentionally removed. New AI flows should therefore live as explicit entry points rather than crowding the core create dialog.

## Architecture

### Core Pattern

Both flows use the same pattern:

1. User submits a prompt and chooses a host agent.
2. Backend enqueues an `agent_task_queue` row with no issue and a typed JSON context.
3. Daemon claims the task and sees the context type.
4. Daemon builds a specialized prompt.
5. Host agent runs and emits structured output through a controlled CLI command or output file channel.
6. Daemon reports completion to the backend.
7. Backend writes an inbox item with the structured result.
8. User reviews and applies the result in the UI.

### New Task Context Types

Add context discriminators:

- `skill-find`
- `agent-draft`

The context should include:

- `type`
- `prompt`
- `requester_id`
- version field, starting at `1`

The version field keeps future prompt/output changes explicit.

## Phase 1: AI Skill Finder

### User Flow

1. User opens a "Find skills" entry point from Skills or Agents.
2. User enters a natural-language goal.
3. User chooses a host agent capable of running the task.
4. UI submits `POST /api/skills/find`.
5. Backend returns `202 Accepted` with `task_id`.
6. User can continue working.
7. When complete, an inbox item shows recommended skill cards.
8. User selects one or more recommendations.
9. UI imports selected skills through the existing skill import endpoint.

### Backend API

Add:

`POST /api/skills/find`

Request:

```json
{
  "prompt": "I want to review SQL migrations and query performance",
  "agent_id": "host-agent-id"
}
```

Response:

```json
{
  "task_id": "task-id"
}
```

The handler should:

- Validate `prompt` is non-empty.
- Validate `agent_id` resolves to an accessible agent in the workspace.
- Enqueue a task using the selected agent and its runtime.
- Store `context = { "type": "skill-find", "version": 1, "prompt": "...", "requester_id": "..." }`.
- Publish existing task queued signals where appropriate.

### Curated Skill Index

Add a repository-owned curated index, likely under `server/internal/agenttmpl/skill_index.json` or a new sibling package if naming needs to be broader.

Each entry should contain:

- `name`
- `description`
- `source_url`
- `tags`
- optional `category`

The host agent prompt must instruct the model to recommend only from this index. The output must include `source_url` values exactly from the index.

### Daemon Prompt

Add a `buildSkillFindPrompt` path in `server/internal/daemon/prompt.go`.

The prompt should:

- Restate the user's goal.
- Provide the curated skill index as JSON.
- Ask for 3-5 recommendations when possible.
- Require a structured output command.
- Forbid invented URLs.
- Require concise reasons tied to the user's goal.

### Structured Output Channel

Add a controlled CLI command used by the agent to hand results back to the daemon.

Preferred shape:

```bash
multica skill find --output-results '<json>'
```

The command should not call the network. It writes the JSON to a daemon-specified output path from an environment variable. This mirrors the quick-create model: the agent uses CLI/tooling, but the daemon/backend own the final product behavior.

Expected result JSON:

```json
[
  {
    "name": "SQL Review",
    "description": "Reviews schema changes and query plans.",
    "source_url": "https://github.com/example/skills/tree/main/sql-review",
    "reason": "Matches migration and query-performance review."
  }
]
```

The daemon should validate the output enough to reject non-array results and entries whose `source_url` is not in the curated index.

### Completion and Inbox

On success, backend writes an inbox item with:

- request prompt
- recommendations
- source task id

On failure or invalid output, backend writes a failure inbox item with a concise message and the task id.

The UI should render recommendation cards from inbox data. Importing skills remains a user action.

## Phase 2: AI Agent Draft

### User Flow

1. User opens "Draft agent with AI" from Agents.
2. User describes the desired agent.
3. User chooses a host agent.
4. UI submits `POST /api/agents/ai-draft`.
5. Backend enqueues an `agent-draft` task and returns `202`.
6. Completion inbox item shows a draft.
7. User reviews name, description, instructions, and skill recommendations.
8. User creates the agent using existing create APIs, then imports or attaches selected skills.

### Backend API

Add:

`POST /api/agents/ai-draft`

Request:

```json
{
  "prompt": "Create an agent that reviews frontend accessibility and visual polish",
  "host_agent_id": "host-agent-id"
}
```

Response:

```json
{
  "task_id": "task-id"
}
```

The handler should be parallel to Skill Finder:

- Validate prompt and host agent.
- Enqueue typed context.
- Return immediately with task id.

### Draft Output

The result should be a draft, not a committed agent.

Expected JSON:

```json
{
  "name": "Frontend QA",
  "description": "Reviews UI for accessibility, responsiveness, and polish.",
  "instructions": "You review frontend changes...",
  "skills": [
    {
      "name": "Web App Testing",
      "source_url": "https://github.com/example/skills/tree/main/webapp-testing",
      "reason": "Useful for Playwright-driven behavioral checks."
    }
  ],
  "summary": "Focused on frontend review and user-visible regression checks."
}
```

The daemon/backend should validate:

- `name` is non-empty and within existing agent limits.
- `description` is within existing limits.
- `instructions` is non-empty.
- every skill URL appears in the curated skill index.

### Applying the Draft

Initial implementation should keep apply behavior explicit:

- The inbox item opens a review surface.
- The user can create the agent from the draft.
- Skill imports remain explicit or bundled into the final create action only after the user confirms.

This avoids silently creating a bad agent from a weak prompt and fits the existing product model where humans approve critical workspace objects.

## Frontend Entry Points

Recommended initial entry points:

- Skills page: "Find skills" button.
- Agents page: "Draft with AI" secondary action near "New agent".
- Inbox: result renderers for `skill_find_done`, `skill_find_failed`, `agent_draft_done`, and `agent_draft_failed`.

Avoid embedding these flows inside every form in the first release. The create-agent dialog already has a tight manual/duplicate job; AI flows are async and should have their own review surface.

## Error Handling

### Enqueue Errors

Return 400 for empty prompts or invalid host agent IDs. Return 403 if the host agent is inaccessible. Return 409 or 422 if the selected host agent cannot run because it has no usable runtime.

### Runtime Errors

Task failures should follow existing task failure handling. The requester gets a failure inbox item. The task remains visible in agent activity.

### Invalid AI Output

Invalid JSON, unknown skill URLs, empty recommendations, or oversized fields should not partially apply anything. The user receives an inbox failure with a short explanation. Logs keep the detailed parse/validation error.

### Skill Import Errors

Skill import happens after review. If one import fails, the UI should report which source failed and leave other imported skills intact only if the existing import flow already behaves that way. Batch import atomicity is not required for the first release.

## Testing

### Backend

Add tests for:

- `POST /api/skills/find` rejects empty prompt.
- `POST /api/skills/find` rejects inaccessible host agent.
- `POST /api/skills/find` enqueues a task with `context.type = "skill-find"`.
- `POST /api/agents/ai-draft` rejects empty prompt.
- `POST /api/agents/ai-draft` enqueues a task with `context.type = "agent-draft"`.
- Task completion writes the correct inbox item for valid structured output.
- Invalid structured output writes a failure inbox item.

### Daemon and CLI

Add tests for:

- skill-find prompt contains the user's prompt and curated index.
- agent-draft prompt contains the user's prompt and curated index.
- `multica skill find --output-results` writes only to the expected daemon output path.
- output validation rejects URLs outside the curated index.
- output validation rejects malformed JSON.

### Frontend

Add focused view tests for:

- Skill Finder submit disabled for empty prompt or missing host agent.
- Submitted Skill Finder request shows async confirmation.
- Skill Finder inbox result renders recommendation cards.
- Agent Draft inbox result renders draft fields and skill recommendations.
- Applying a draft calls existing create/import APIs in the expected order.

## Rollout

Ship in two independent slices:

1. **Skill Finder**
   - Backend enqueue endpoint
   - daemon prompt/output channel
   - curated index
   - inbox result renderer
   - import selected recommendations

2. **Agent Draft**
   - Backend enqueue endpoint
   - daemon prompt/output validation
   - inbox draft renderer
   - explicit create-from-draft action

Skill Finder can land first and provide standalone value. Agent Draft should reuse its curated skill index and output validation.

## Open Implementation Choices

The following are implementation choices, not product ambiguities:

- Whether the curated skill index lives in `server/internal/agenttmpl` or a new `server/internal/skillindex` package.
- Whether the output channel is implemented as a dedicated temp file per task or as a daemon-managed path under the task workdir.
- Whether the first frontend surface is a dialog or a lightweight page. The preferred first version is a dialog for submission and an inbox-driven review result.

Any choice must preserve the core constraints: no server-side LLM, no invented URLs, no silent agent creation.
