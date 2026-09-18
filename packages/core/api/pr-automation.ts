import { z } from "zod";

export const PRPolicySchema = z
  .object({
    source: z.enum(["manual", "title_branch", "all"]),
    auto_complete: z.boolean(),
    revision: z.number(),
  })
  .transform(({ auto_complete, ...rest }) => ({
    ...rest,
    autoComplete: auto_complete,
  }));
export type PRPolicy = z.infer<typeof PRPolicySchema>;
export const PRPolicyEnvelopeSchema = z.object({
  policy: PRPolicySchema,
  migrated: z.boolean(),
});
export const PRPolicyLinkSchema = z
  .object({
    pr_id: z.string(),
    provider: z.string(),
    title: z.string(),
    url: z
      .string()
      .url()
      .refine((value) => /^https?:\/\//.test(value)),
    state: z.string(),
    source: z.string(),
    connected: z.boolean(),
  })
  .transform(({ pr_id, ...rest }) => ({ ...rest, prId: pr_id }));
export const PRPolicyIssueSchema = z.object({
  id: z.string(),
  identifier: z.string(),
  title: z.string(),
  status: z.string(),
  revision: z.number(),
  terminal: z.boolean(),
  triage: z.boolean(),
  disabled: z.boolean(),
  links: z.array(PRPolicyLinkSchema),
  added: z.array(PRPolicyLinkSchema),
  removed: z.array(PRPolicyLinkSchema),
  excluded: z.array(PRPolicyLinkSchema),
  decision: z.object({
    reason: z.string(),
    complete: z.boolean(),
    waiting: z.array(z.string()),
  }),
});
export const PRPolicyPlanSchema = PRPolicyEnvelopeSchema.extend({
  issues: z.array(PRPolicyIssueSchema),
  pending: z.array(z.string()),
  token: z.string(),
});
export const IssuePRPolicySchema = PRPolicyEnvelopeSchema.extend({
  issue: PRPolicyIssueSchema,
});
export type PRPolicyPlan = z.infer<typeof PRPolicyPlanSchema>;
export type IssuePRPolicy = z.infer<typeof IssuePRPolicySchema>;
export type PRPolicyUpdate =
  | { disabled: boolean }
  | {
      prId?: string;
      url?: string;
      mode: "manual" | "excluded" | "automatic";
    };

export type PRPolicyEnvelope = z.infer<typeof PRPolicyEnvelopeSchema>;
