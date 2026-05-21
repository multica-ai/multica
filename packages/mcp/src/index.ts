#!/usr/bin/env node

/**
 * Multica MCP Server
 *
 * Exposes Multica issue management as MCP tools so any AI agent
 * (Claude Code, Copilot CLI, Cursor) can file and manage work items.
 *
 * Configuration via environment variables:
 *   MULTICA_URL   — Multica server URL (e.g. http://multica:8080)
 *   MULTICA_TOKEN — Personal Access Token (mul_...)
 */

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { MulticaClient } from "./client.js";

const MULTICA_URL = process.env.MULTICA_URL || process.env.MULTICA_SERVER_URL || "http://localhost:8080";
const MULTICA_TOKEN = process.env.MULTICA_TOKEN || "";

if (!MULTICA_TOKEN) {
  console.error("MULTICA_TOKEN is required. Generate one at your Multica instance under Settings → Tokens.");
  process.exit(1);
}

const client = new MulticaClient(MULTICA_URL, MULTICA_TOKEN);

let workspaceReady = false;
async function ensureWorkspace() {
  if (workspaceReady) return;
  await client.autoSelectWorkspace();
  workspaceReady = true;
}

function text(t: string) {
  return { content: [{ type: "text" as const, text: t }] };
}

const server = new Server(
  { name: "multica", version: "0.1.0" },
  { capabilities: { tools: {} } }
);

// ─── List Tools ───

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: "create_issue",
      description: "Create a new work item. Optionally assign to an agent (Copilot, Codex) or member by name.",
      inputSchema: {
        type: "object" as const,
        properties: {
          title: { type: "string", description: "Issue title" },
          description: { type: "string", description: "Detailed description (markdown)" },
          priority: { type: "string", description: "urgent, high, medium, low, or none" },
          status: { type: "string", description: "backlog, todo, in_progress, in_review, done, cancelled" },
          assignee: { type: "string", description: "Agent or member name to assign (e.g. 'Copilot')" },
        },
        required: ["title"],
      },
    },
    {
      name: "list_issues",
      description: "List work items, optionally filtered.",
      inputSchema: {
        type: "object" as const,
        properties: {
          status: { type: "string", description: "Filter by status" },
          priority: { type: "string", description: "Filter by priority" },
          open_only: { type: "boolean", description: "Only open issues" },
        },
      },
    },
    {
      name: "get_issue",
      description: "Get full details of an issue including comments.",
      inputSchema: {
        type: "object" as const,
        properties: {
          issue_id: { type: "string", description: "Issue ID (UUID)" },
        },
        required: ["issue_id"],
      },
    },
    {
      name: "update_issue",
      description: "Update an issue's status, priority, title, or description.",
      inputSchema: {
        type: "object" as const,
        properties: {
          issue_id: { type: "string", description: "Issue ID" },
          status: { type: "string" },
          priority: { type: "string" },
          title: { type: "string" },
          description: { type: "string" },
        },
        required: ["issue_id"],
      },
    },
    {
      name: "assign_issue",
      description: "Assign an issue to an agent or member by name. Triggers the agent to start working.",
      inputSchema: {
        type: "object" as const,
        properties: {
          issue_id: { type: "string", description: "Issue ID" },
          assignee: { type: "string", description: "Agent or member name" },
        },
        required: ["issue_id", "assignee"],
      },
    },
    {
      name: "add_comment",
      description: "Add a comment to an issue.",
      inputSchema: {
        type: "object" as const,
        properties: {
          issue_id: { type: "string", description: "Issue ID" },
          content: { type: "string", description: "Comment text (markdown)" },
        },
        required: ["issue_id", "content"],
      },
    },
    {
      name: "list_agents",
      description: "List all AI agents with their status and provider.",
      inputSchema: { type: "object" as const, properties: {} },
    },
  ],
}));

// ─── Call Tool ───

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  const { name, arguments: args = {} } = request.params;
  await ensureWorkspace();

  switch (name) {
    case "create_issue": {
      const { title, description, priority, status, assignee } = args as Record<string, string>;
      const data: Record<string, unknown> = { title };
      if (description) data.description = description;
      if (priority) data.priority = priority;
      if (status) data.status = status;

      if (assignee) {
        const resolved = await client.resolveAssignee(assignee);
        if (resolved) {
          data.assignee_type = resolved.type;
          data.assignee_id = resolved.id;
        }
      }

      const issue = await client.createIssue(data as Parameters<typeof client.createIssue>[0]);
      const info = assignee
        ? (data.assignee_id ? `Assigned to ${assignee}` : `⚠️ Could not find "${assignee}"`)
        : "Unassigned";
      return text(`✅ Created issue #${issue.sequence_number}: ${issue.title}\nStatus: ${issue.status} | Priority: ${issue.priority} | ${info}\nID: ${issue.id}`);
    }

    case "list_issues": {
      const { status, priority, open_only } = args as Record<string, string | boolean>;
      const result = await client.listIssues({
        status: status as string | undefined,
        priority: priority as string | undefined,
        open_only: open_only === true ? true : undefined,
      });
      const issues = result.issues || [];
      if (issues.length === 0) return text("No issues found.");

      const lines = issues.map(
        (i) => `#${i.sequence_number} [${i.status}] ${i.priority} — ${i.title}${i.assignee_name ? ` (→ ${i.assignee_name})` : ""} (${i.id})`
      );
      return text(`Found ${issues.length} issue(s):\n\n${lines.join("\n")}`);
    }

    case "get_issue": {
      const { issue_id } = args as { issue_id: string };
      const issue = await client.getIssue(issue_id);
      const comments = await client.listComments(issue_id).catch(() => []);
      const commentText = comments.length > 0
        ? `\n\n--- Comments (${comments.length}) ---\n${comments.map((c) => `[${c.author_type}] ${c.content}`).join("\n\n")}`
        : "";
      return text(`#${issue.sequence_number}: ${issue.title}\nStatus: ${issue.status} | Priority: ${issue.priority}\nAssignee: ${issue.assignee_name || "unassigned"}\n\n${issue.description || "(no description)"}${commentText}`);
    }

    case "update_issue": {
      const { issue_id, ...updates } = args as Record<string, string>;
      const filtered = Object.fromEntries(Object.entries(updates).filter(([, v]) => v !== undefined));
      const issue = await client.updateIssue(issue_id, filtered);
      return text(`✅ Updated #${issue.sequence_number}: ${issue.title}\nStatus: ${issue.status} | Priority: ${issue.priority}`);
    }

    case "assign_issue": {
      const { issue_id, assignee } = args as { issue_id: string; assignee: string };
      const resolved = await client.resolveAssignee(assignee);
      if (!resolved) {
        const agents = await client.listAgents();
        return text(`❌ Could not find "${assignee}". Available agents: ${agents.map((a) => a.name).join(", ") || "none"}`);
      }
      const issue = await client.updateIssue(issue_id, { assignee_type: resolved.type, assignee_id: resolved.id });
      return text(`✅ Assigned #${issue.sequence_number} to ${resolved.name} (${resolved.type})`);
    }

    case "add_comment": {
      const { issue_id, content } = args as { issue_id: string; content: string };
      await client.createComment(issue_id, content);
      return text("✅ Comment added.");
    }

    case "list_agents": {
      const agents = await client.listAgents();
      if (agents.length === 0) return text("No agents configured.");
      const lines = agents.map((a) => `${a.name} — ${a.status} (${a.runtime_mode}, ${a.provider || "?"}) [${a.id}]`);
      return text(`Agents (${agents.length}):\n\n${lines.join("\n")}`);
    }

    default:
      return text(`Unknown tool: ${name}`);
  }
});

// ─── Start ───

async function main() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP server failed:", err);
  process.exit(1);
});
