import { z } from "zod";
import { parseWithFallback } from "../api/schema";

/**
 * An agent's instruction history (spec 4.6). Every change to the instructions
 * writes a revision: `human` for an edit, `proposal` for an approved organisation
 * change, `rollback` for a restore. A restore is itself a new revision.
 */
export const INSTRUCTION_REVISION_SOURCES = ["human", "proposal", "rollback"] as const;
export type InstructionRevisionSource = (typeof INSTRUCTION_REVISION_SOURCES)[number] | "unknown";

export interface InstructionRevision {
  id: string;
  agent_id: string;
  revision: number;
  instructions: string;
  source: InstructionRevisionSource;
  proposal_id: string | null;
  actor_type: string;
  actor_id: string | null;
  created_at: string;
}

const sourceSchema = z
  .string()
  .default("unknown")
  .transform((s): InstructionRevisionSource =>
    (INSTRUCTION_REVISION_SOURCES as readonly string[]).includes(s)
      ? (s as InstructionRevisionSource)
      : "unknown",
  );

export const InstructionRevisionSchema = z
  .object({
    id: z.string().default(""),
    agent_id: z.string().default(""),
    revision: z.number().default(1),
    instructions: z.string().default(""),
    source: sourceSchema,
    proposal_id: z.string().nullable().default(null),
    actor_type: z.string().default(""),
    actor_id: z.string().nullable().default(null),
    created_at: z.string().default(""),
  })
  .loose();

const InstructionRevisionListResponseSchema = z
  .object({ revisions: z.array(InstructionRevisionSchema).default([]) })
  .loose()
  .transform((body) => body.revisions);

export const EMPTY_INSTRUCTION_REVISIONS: InstructionRevision[] = [];

/** The history list, newest first. A response that cannot be read is an empty history. */
export function parseInstructionRevisions(raw: unknown): InstructionRevision[] {
  return parseWithFallback<InstructionRevision[]>(raw, InstructionRevisionListResponseSchema, EMPTY_INSTRUCTION_REVISIONS, {
    endpoint: "GET /api/agents/{id}/instruction-revisions",
  });
}
