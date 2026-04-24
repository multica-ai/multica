---
title: Chat
description: One-to-one conversation with an agent outside any issue — fully sandboxed. The agent cannot see or change issues, and nobody else can see the conversation.
---

import { Callout } from "fumadocs-ui/components/callout";

**Chat is a one-to-one conversation between you and an [agent](/agents)** — stepping outside the [issue](/issues) board. The agent sees no issues and cannot change any issue, and the entire conversation is **fully private** (nobody else in the [workspace](/workspaces), including admins, can see it). It fits discussing an approach with an agent, brainstorming, or asking a question that does not belong to any issue.

## Why not just @-mention the agent?

[@-mention](/mentioning-agents) **pulls the agent into** an issue's context — it reads the issue description and every historical comment, and it can change the issue. Chat flips this: **it pulls you out of** the issue — the agent only sees this single conversation, has no awareness of any issue, and has no entry point to modify one.

Two rules of thumb:

- You want feedback grounded in the context of a specific issue → [@-mention](/mentioning-agents)
- You want to discuss a topic unrelated to any issue (or you do not want anyone else to see the discussion) → Chat

## Start a conversation

Open **Chat** from the sidebar, pick an agent, and start a new conversation. The interface feels like any messaging app: you send a message, the agent replies. Each message triggers a run in the background (an enqueued `task`), so replies may take a few seconds.

## What an agent can and cannot do in chat

Agents run in a **fully sandboxed** mode inside a conversation.

**Can do:**

- Answer the questions in your current message
- Use its configured [skills](/skills) and MCP
- Read and write files in its own working directory
- Call `multica` CLI commands that do not need issue context (for example, querying basic workspace info)

**Cannot do:**

- **See any issue** — the prompt the agent receives has no issue IDs, and commands like `multica issue list` return empty
- **Change any issue** — without issue context, API calls are blocked by permission checks
- **See other conversations** — conversations are fully isolated
- **@-mention anyone or any agent** — chat is a private space with no path to notify others

## How multi-turn context is preserved

Chat maintains multi-turn context via **provider session resumption** — the agent establishes a provider session on its first reply (for example, a Claude session), and the session ID is stored. On the next message, the task dispatch passes that ID back so the agent **resumes from where it left off** without re-reading history every time.

If **one turn fails**, Multica looks up the previous task that had established a session ID (whether that task succeeded or failed) and tries to resume — a single failure in the middle does not drop the memory of the whole conversation.

Note: not every provider actually implements session resumption — see the [**Providers Matrix**](/providers) for support status.

## Archive a conversation

Conversations you no longer want to see can be archived — right-click in the conversation list or use the "Archive" button on the detail page. After archiving:

- The conversation disappears from the active list (you can still find it in the "Archived" view)
- Historical messages, session ID, and the working directory are all preserved — nothing is deleted

<Callout type="warning">
**There is no "restore" button after archiving.** There is currently no entry point to move an archived conversation back to active. If you want to continue the thread later, you will need to start a new conversation. To revisit content in an archived conversation, open the "Archived" view and read through the history.
</Callout>

## Next

- [**Autopilots**](/autopilots) — let agents start work automatically on a schedule
- [**Assign issues to agents**](/assigning-issues) — bring the topic back onto the issue board
