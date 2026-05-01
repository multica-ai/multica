// Label tools — list + attach/detach on issues. Create/update isn't
// exposed: label taxonomy is a deliberate workspace decision and chat-
// driven creation tends to fragment categories.

import { z } from "zod";
import { defineTool } from "../tool.js";

export const labelListTool = defineTool({
  name: "multica_label_list",
  title: "List labels",
  description: "Return all labels in the active workspace (id, name, color).",
  inputSchema: z.object({}),
  handler: async (_input, ctx) => {
    return ctx.client.get("/api/labels");
  },
});

export const labelAttachTool = defineTool({
  name: "multica_label_attach",
  title: "Attach a label to an issue",
  description: "Attach an existing label (by id) to an issue. No-op if already attached.",
  inputSchema: z.object({
    issue_id: z.string().min(1),
    label_id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.post(
      `/api/issues/${encodeURIComponent(input.issue_id)}/labels`,
      { label_id: input.label_id },
    );
  },
});

export const labelDetachTool = defineTool({
  name: "multica_label_detach",
  title: "Detach a label from an issue",
  description: "Remove a label from an issue. No-op if it wasn't attached.",
  inputSchema: z.object({
    issue_id: z.string().min(1),
    label_id: z.string().uuid(),
  }),
  handler: async (input, ctx) => {
    return ctx.client.delete(
      `/api/issues/${encodeURIComponent(input.issue_id)}/labels/${input.label_id}`,
    );
  },
});

export const labelTools = [labelListTool, labelAttachTool, labelDetachTool];
