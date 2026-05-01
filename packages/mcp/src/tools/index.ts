// Aggregated tool registry. Order here is the order the MCP picker
// shows tools to the model — front-load the high-leverage ones (issues,
// channels, agents) and put admin/configuration further down so the
// orchestration surface is the first thing the model sees.

import type { RegisteredTool } from "../tool.js";
import { workspaceTools } from "./workspace.js";
import { issueTools } from "./issues.js";
import { agentTools } from "./agents.js";
import { channelTools } from "./channels.js";
import { projectTools } from "./projects.js";
import { labelTools } from "./labels.js";
import { autopilotTools } from "./autopilots.js";

export const allTools: RegisteredTool[] = [
  ...issueTools,
  ...agentTools,
  ...channelTools,
  ...projectTools,
  ...labelTools,
  ...autopilotTools,
  ...workspaceTools,
];
