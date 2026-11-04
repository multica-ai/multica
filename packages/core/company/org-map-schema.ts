import { z } from "zod";

// Mirrors GET /api/company/org-map. Unknown fields are ignored; optional fields
// have defaults, so a backend that omits them still parses.
const OrgMapAgentSchema = z.object({
  name: z.string().default(""),
  runtime_status: z.string().default(""),
  archived: z.boolean().default(false),
});

const OrgMapMemberSchema = z.object({
  type: z.string().default("member"),
  id: z.string().default(""),
  name: z.string().default(""),
  role: z.string().default(""),
  is_leader: z.boolean().default(false),
  skills: z.array(z.string()).default([]),
});

export const OrgMapSchema = z.object({
  floors: z
    .array(
      z.object({
        id: z.string(),
        name: z.string().default(""),
        chief: OrgMapAgentSchema.nullable().default(null),
      }),
    )
    .default([]),
  squads: z
    .array(
      z.object({
        id: z.string(),
        name: z.string().default(""),
        floor_id: z.string().nullable().default(null),
        floor_name: z.string().default(""),
        leader: OrgMapAgentSchema.nullable().default(null),
        has_charter: z.boolean().default(false),
        project: z
          .object({ title: z.string().default(""), workflow_name: z.string().default("") })
          .nullable()
          .default(null),
        open_issues: z.number().default(0),
        member_count: z.number().default(0),
        members_truncated: z.boolean().default(false),
        members: z.array(OrgMapMemberSchema).default([]),
      }),
    )
    .default([]),
  squad_limit: z.number().default(200),
  squads_truncated: z.boolean().default(false),
});

export type OrgMap = z.infer<typeof OrgMapSchema>;

export const EMPTY_ORG_MAP: OrgMap = OrgMapSchema.parse({});
