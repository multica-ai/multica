import { z } from "zod";
import { jsonSchema, type Json } from "../contracts/index.js";
import type { Environments } from "../setup/environments.js";
import { problem, VerificationError } from "../host/problem.js";

export const stateSourceSchema = z.enum([
  "daemon",
  "main",
  "renderer",
  "backend",
  "page",
]);
export type StateSource = z.infer<typeof stateSourceSchema>;
export const eventSourceSchema = z.enum(["daemon", "main", "page"]);
export type EventSource = z.infer<typeof eventSourceSchema>;

export const stateReadInput = z
  .object({
    environmentId: z.uuid(),
    source: stateSourceSchema,
    subjectRef: z.string().max(400).default(""),
    beforeTurnIndex: z.number().int().nonnegative().optional(),
  })
  .strict();
export const eventsReadInput = z
  .object({
    environmentId: z.uuid(),
    source: eventSourceSchema,
    subjectRef: z.string().max(400).default(""),
    after: z.number().int().nonnegative().default(0),
  })
  .strict();

/** Reads application state and events as the sources report them. */
export class State {
  constructor(private readonly environments: Environments) {}

  async read(input: z.input<typeof stateReadInput>) {
    const { environmentId, source, subjectRef, beforeTurnIndex } =
      stateReadInput.parse(input);
    await this.environments.get(environmentId);
    const observedAt = new Date().toISOString();
    let payload: unknown;
    if (source === "daemon") {
      if (!subjectRef)
        throw new VerificationError(
          problem("invalidInput", "Daemon state requires a conversation id"),
        );
      const ref = await this.environments.connection(environmentId, "daemon");
      const path = `/local/v2/sessions/${encodeURIComponent(subjectRef)}`;
      payload = await this.environments.bindings.request(
        ref,
        beforeTurnIndex === undefined
          ? `${path}/snapshot`
          : `${path}/history?before_index=${beforeTurnIndex}`,
      );
    } else if (source === "backend") {
      const match = /^(issue|project):(.+)$/.exec(subjectRef);
      if (!match)
        throw new VerificationError(
          problem(
            "invalidInput",
            "Backend state requires an issue:<id> or project:<id> reference",
          ),
        );
      const ref = await this.environments.connection(environmentId, "backend");
      payload = await this.environments.bindings.request(
        ref,
        `/api/${match[1]}s/${encodeURIComponent(match[2]!)}`,
      );
    } else {
      payload = await this.environments.desktopRequest(
        environmentId,
        `/snapshot?source=${source}&subjectRef=${encodeURIComponent(subjectRef)}`,
      );
    }
    return {
      environmentId,
      source,
      subjectRef,
      observedAt,
      payload: json(payload),
    };
  }

  async events(input: z.input<typeof eventsReadInput>) {
    const { environmentId, source, subjectRef, after } =
      eventsReadInput.parse(input);
    await this.environments.get(environmentId);
    let payload: unknown;
    if (source === "daemon") {
      if (!subjectRef)
        throw new VerificationError(
          problem("invalidInput", "Daemon events require a conversation id"),
        );
      const ref = await this.environments.connection(environmentId, "daemon");
      payload = await this.environments.bindings.request(
        ref,
        `/local/v2/verification/events?conversationId=${encodeURIComponent(subjectRef)}&after=${after}`,
      );
    } else {
      payload = await this.environments.desktopRequest(
        environmentId,
        `/events?after=${after}`,
      );
    }
    return {
      environmentId,
      source,
      subjectRef,
      after,
      observedAt: new Date().toISOString(),
      payload: json(payload),
    };
  }
}

function json(value: unknown): Json {
  return jsonSchema.parse(value);
}
