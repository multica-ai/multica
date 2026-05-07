// Internal tool registration shape. Each `tools/*.ts` module exports an
// array of `RegisteredTool` objects via `defineTool({…})`; the server
// collects them all and dispatches CallTool requests by name.
//
// The shape is split into two layers:
//
//   - `ToolDefinition<TInput>` — the *authored* shape, generic over the
//     input zod schema so handlers get statically-typed `input` from
//     `z.infer<TInput>`. Used inside `defineTool`.
//   - `RegisteredTool` — the *registry* shape, with the schema typed as
//     plain `ZodTypeAny` and the handler as `(unknown, ctx) => …`. This
//     is what the server iterates so an array of tools-with-different-
//     schemas unifies cleanly into one collection.
//
// `defineTool` is the bridge: callers write strongly-typed handlers; the
// returned value is erased to `RegisteredTool` so the registry can hold
// a heterogeneous list without type-variance complaints. The server
// re-validates input against the schema before invoking the handler, so
// erasing the type here is safe at runtime.

import type { z } from "zod";
import type { MulticaClient } from "./client.js";

export interface ToolContext {
  client: MulticaClient;
}

export interface ToolDefinition<TInput extends z.ZodTypeAny> {
  /** Tool name as exposed to the model. Convention: `multica_<resource>_<verb>`. */
  name: string;
  /** Short description shown in the tool picker. ~1–2 sentences max. */
  title?: string;
  /** Long-form description used by the model to decide when to call this tool. */
  description: string;
  /** Zod schema for input validation. The MCP SDK converts this to JSON schema. */
  inputSchema: TInput;
  /** Handler — return a JSON-serializable value. The server wraps the response into MCP `content`. */
  handler: (input: z.infer<TInput>, ctx: ToolContext) => Promise<unknown>;
}

export interface RegisteredTool {
  name: string;
  title?: string;
  description: string;
  inputSchema: z.ZodTypeAny;
  handler: (input: unknown, ctx: ToolContext) => Promise<unknown>;
}

/**
 * Promotes a strongly-typed `ToolDefinition` into the registry shape.
 * The cast is sound because the server validates input against the same
 * `inputSchema` before calling `handler` — by the time the handler runs,
 * `input` matches `z.infer<TInput>` even though we lost the type.
 */
export function defineTool<TInput extends z.ZodTypeAny>(
  def: ToolDefinition<TInput>,
): RegisteredTool {
  return def as unknown as RegisteredTool;
}
