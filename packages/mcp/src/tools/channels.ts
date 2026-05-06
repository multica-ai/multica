// Channels tools — read + post for the multi-participant chat surface.
// Posting a message that @-mentions an agent (or in a DM with an agent)
// dispatches a task on the server, so the LLM can use this surface to
// chat with agents in addition to the issue board.

import { z } from "zod";
import { defineTool } from "../tool.js";

export const channelListTool = defineTool({
  name: "multica_channel_list",
  title: "List channels",
  description:
    "Return channels and DMs the active actor belongs to, including per-channel unread counts. Use before posting to look up channel ids.",
  inputSchema: z.object({}),
  handler: async (_input, ctx) => {
    return ctx.client.get("/api/channels");
  },
});

export const channelGetTool = defineTool({
  name: "multica_channel_get",
  title: "Get channel",
  description: "Return one channel's metadata by id.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/channels/${input.id}`);
  },
});

export const channelHistoryTool = defineTool({
  name: "multica_channel_history",
  title: "List channel messages",
  description:
    "Return messages in a channel, newest first, paginated by created_at. Use the oldest 'created_at' you've already seen as 'before' to fetch the next older page.",
  inputSchema: z.object({
    channel_id: z.string().uuid(),
    limit: z
      .number()
      .int()
      .min(1)
      .max(200)
      .optional()
      .describe("Default 50, capped at 200."),
    before: z
      .string()
      .optional()
      .describe(
        "RFC3339 timestamp; only messages strictly older are returned. Pass the oldest 'created_at' from a prior page to paginate.",
      ),
    include_threaded: z
      .boolean()
      .optional()
      .describe(
        "Include thread replies inline. Default false — top-level only.",
      ),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/channels/${input.channel_id}/messages`, {
      query: {
        limit: input.limit,
        before: input.before,
        include_threaded: input.include_threaded,
      },
    });
  },
});

export const channelPostTool = defineTool({
  name: "multica_channel_post",
  title: "Post a channel message",
  description:
    "Post a message to a channel. To @-mention an agent (which dispatches a task to it), include the canonical mention markup: '[@AgentName](mention://agent/<agent-uuid>)'. In a DM with an agent, every member message implicitly addresses the agent — no @mention required.",
  inputSchema: z.object({
    channel_id: z.string().uuid(),
    content: z.string().min(1),
    parent_message_id: z
      .string()
      .uuid()
      .optional()
      .describe("Reply in a thread instead of the main timeline."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(`/api/channels/${input.channel_id}/messages`, {
      content: input.content,
      parent_message_id: input.parent_message_id ?? null,
    });
  },
});

export const channelMembersTool = defineTool({
  name: "multica_channel_members",
  title: "List channel members",
  description:
    "Return the channel's member list (members + agents). Use to verify an agent is in a channel before @-mentioning it (mentions of non-members render but don't dispatch tasks).",
  inputSchema: z.object({
    channel_id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/channels/${input.channel_id}/members`);
  },
});

export const channelMarkReadTool = defineTool({
  name: "multica_channel_mark_read",
  title: "Mark channel as read",
  description:
    "Update the read cursor for the active actor up to the given message id. Clears the unread badge.",
  inputSchema: z.object({
    channel_id: z.string().uuid(),
    message_id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(`/api/channels/${input.channel_id}/read`, {
      message_id: input.message_id,
    });
  },
});

export const channelTools = [
  channelListTool,
  channelGetTool,
  channelHistoryTool,
  channelPostTool,
  channelMembersTool,
  channelMarkReadTool,
];
