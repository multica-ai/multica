// MCP server wiring. Builds the `Server` instance, registers every tool
// from `tools/index.ts`, and dispatches CallTool requests by name. Pure
// orchestration — the actual API logic lives in each tool module.

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";

import type { MulticaClient } from "./client.js";
import type { RegisteredTool, ToolContext } from "./tool.js";
import { allTools } from "./tools/index.js";

export interface CreateServerOptions {
  client: MulticaClient;
  /** Override the tool registry — useful for tests that want to inject one fake tool. */
  tools?: RegisteredTool[];
}

const SERVER_NAME = "multica";
// Hard-coded version string. Bumped manually with each release; keep in
// sync with package.json.
const SERVER_VERSION = "0.1.0";

export function createServer(opts: CreateServerOptions): {
  server: Server;
  tools: RegisteredTool[];
} {
  const tools = opts.tools ?? allTools;
  // Quick duplicate-name guard. A typo here would let later tools shadow
  // earlier ones silently and we'd ship a broken server.
  const seen = new Set<string>();
  for (const t of tools) {
    if (seen.has(t.name)) {
      throw new Error(`Duplicate MCP tool name: ${t.name}`);
    }
    seen.add(t.name);
  }

  const ctx: ToolContext = { client: opts.client };
  const server = new Server(
    { name: SERVER_NAME, version: SERVER_VERSION },
    { capabilities: { tools: {} } },
  );

  // List: stable order matches `tools` so the picker UI doesn't shuffle
  // between sessions. Each tool's input schema is converted from zod to
  // JSON schema via `zodToJsonSchema` shimmed below — the SDK accepts a
  // raw JSON schema object.
  server.setRequestHandler(ListToolsRequestSchema, async () => {
    return {
      tools: tools.map((t) => ({
        name: t.name,
        title: t.title ?? t.name,
        description: t.description,
        inputSchema: zodToJsonSchema(t.inputSchema),
      })),
    };
  });

  // Call: lookup → validate input → run handler → serialize result.
  // Errors thrown by handlers (network, ApiError, validation) are
  // converted to an MCP error content block so the model can recover
  // instead of crashing the whole session.
  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    const tool = tools.find((t) => t.name === req.params.name);
    if (!tool) {
      return {
        isError: true,
        content: [{ type: "text", text: `Unknown tool: ${req.params.name}` }],
      };
    }

    let parsed: unknown;
    try {
      parsed = tool.inputSchema.parse(req.params.arguments ?? {});
    } catch (err) {
      const detail = err instanceof z.ZodError ? formatZodIssues(err) : String(err);
      return {
        isError: true,
        content: [{ type: "text", text: `Invalid input for ${tool.name}: ${detail}` }],
      };
    }

    try {
      const result = await tool.handler(parsed, ctx);
      return {
        content: [{ type: "text", text: serialize(result) }],
      };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return {
        isError: true,
        content: [{ type: "text", text: message }],
      };
    }
  });

  return { server, tools };
}

function serialize(value: unknown): string {
  if (typeof value === "string") return value;
  // Pretty-print so the model can read structure when it inspects raw
  // tool results in long conversation transcripts.
  return JSON.stringify(value, null, 2);
}

function formatZodIssues(err: z.ZodError): string {
  return err.issues
    .map((i) => `${i.path.length ? i.path.join(".") + ": " : ""}${i.message}`)
    .join("; ");
}

// The MCP SDK accepts JSON Schema objects directly. Zod 3 ships a
// `toJSONSchema`-style helper as a separate package, but to avoid the
// extra dependency we walk a minimal subset of zod types we actually
// use (object / string / number / boolean / array / enum / optional /
// nullable). For everything else we fall through to `{ type: "object" }`
// so the SDK doesn't choke; the handler still validates with the real
// zod schema before running.
function zodToJsonSchema(schema: z.ZodTypeAny): Record<string, unknown> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const def = (schema as any)._def;
  if (!def) return { type: "object" };

  const typeName = def.typeName as string;
  switch (typeName) {
    case "ZodObject": {
      const shape = def.shape() as Record<string, z.ZodTypeAny>;
      const properties: Record<string, unknown> = {};
      const required: string[] = [];
      for (const [key, child] of Object.entries(shape)) {
        properties[key] = zodToJsonSchema(child);
        if (!isOptional(child)) required.push(key);
      }
      const out: Record<string, unknown> = {
        type: "object",
        properties,
      };
      if (required.length > 0) out.required = required;
      const description = (schema as { description?: string }).description;
      if (description) out.description = description;
      return out;
    }
    case "ZodString": {
      const out: Record<string, unknown> = { type: "string" };
      const description = (schema as { description?: string }).description;
      if (description) out.description = description;
      return out;
    }
    case "ZodNumber":
      return { type: "number" };
    case "ZodBoolean":
      return { type: "boolean" };
    case "ZodArray":
      return { type: "array", items: zodToJsonSchema(def.type) };
    case "ZodEnum":
      return { type: "string", enum: def.values };
    case "ZodLiteral":
      return { const: def.value };
    case "ZodOptional":
    case "ZodNullable":
    case "ZodDefault":
      return zodToJsonSchema(def.innerType);
    case "ZodUnion": {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const options = def.options as any[];
      return { anyOf: options.map((o: z.ZodTypeAny) => zodToJsonSchema(o)) };
    }
    case "ZodRecord":
      return { type: "object", additionalProperties: zodToJsonSchema(def.valueType) };
    default:
      return { type: "object" };
  }
}

function isOptional(schema: z.ZodTypeAny): boolean {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const typeName = (schema as any)._def?.typeName;
  return typeName === "ZodOptional" || typeName === "ZodDefault";
}
