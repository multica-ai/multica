// Autopilot tools — read-only. Triggering an autopilot run IS exposed
// because that's the high-leverage chat-side workflow ("kick off the
// nightly cleanup"); creating new autopilots / triggers is left for the
// UI where the form-driven wizard makes the contract obvious.

import { z } from "zod";
import { defineTool } from "../tool.js";

export const autopilotListTool = defineTool({
  name: "multica_autopilot_list",
  title: "List autopilots",
  description:
    "Return autopilots in the workspace (id, title, status, agent, source). Use to find an autopilot before triggering or inspecting runs.",
  inputSchema: z.object({}),
  handler: async (_input, ctx) => {
    return ctx.client.get("/api/autopilots");
  },
});

export const autopilotGetTool = defineTool({
  name: "multica_autopilot_get",
  title: "Get autopilot",
  description: "Full autopilot details including triggers and instructions.",
  inputSchema: z.object({
    id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/autopilots/${input.id}`);
  },
});

export const autopilotRunsTool = defineTool({
  name: "multica_autopilot_runs",
  title: "List autopilot runs",
  description: "Recent execution history for an autopilot (status, started, completed, error).",
  inputSchema: z.object({
    id: z.string().uuid(),
    limit: z.number().int().min(1).max(100).optional().describe("Default 20."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.get(`/api/autopilots/${input.id}/runs`, {
      query: { limit: input.limit },
    });
  },
});

export const autopilotTriggerTool = defineTool({
  name: "multica_autopilot_trigger",
  title: "Trigger autopilot run",
  description:
    "Manually start an autopilot run. Optional payload is forwarded to the agent as the run's trigger context.",
  inputSchema: z.object({
    id: z.string().uuid(),
    payload: z
      .record(z.unknown())
      .optional()
      .describe("Free-form JSON payload available to the agent during the run."),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(`/api/autopilots/${input.id}/trigger`, {
      payload: input.payload ?? null,
    });
  },
});

export const autopilotTools = [
  autopilotListTool,
  autopilotGetTool,
  autopilotRunsTool,
  autopilotTriggerTool,
];
