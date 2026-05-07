// Agent tools — read-only surface for picking the right agent to assign
// or @-mention. Mutating operations (create / archive / set skills) are
// intentionally not exposed: agent configuration changes ripple into
// task dispatch and shouldn't happen from a chat-side LLM by default.

import { z } from "zod";
import { defineTool } from "../tool.js";

export const agentListTool = defineTool({
  name: "multica_agent_list",
  title: "List agents",
  description:
    "Return agents in the workspace (id, name, description, runtime status, archived). Use to find the right agent before creating or assigning an issue.",
  inputSchema: z.object({
    include_archived: z
      .boolean()
      .optional()
      .describe("Include archived agents. Default false."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get("/api/agents", {
      query: { include_archived: input.include_archived },
    });
  },
});

export const agentGetTool = defineTool({
  name: "multica_agent_get",
  title: "Get agent",
  description:
    "Fetch full details for one agent (instructions, skills, runtime, custom env). Use to verify an agent is reachable before assigning to it.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/agents/${input.id}`);
  },
});

export const agentTasksTool = defineTool({
  name: "multica_agent_tasks",
  title: "List agent's recent tasks",
  description:
    "Return the most recent tasks dispatched to an agent (status, issue, started_at, completed_at). Useful to check whether the agent is currently busy.",
  inputSchema: z.object({
    id: z.string().uuid(),
    limit: z.number().int().min(1).max(100).optional().describe("Default 20."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/agents/${input.id}/tasks`, {
      query: { limit: input.limit },
    });
  },
});

export const agentTools = [agentListTool, agentGetTool, agentTasksTool];
