import { z } from "zod";

export const IssueShareTokenSchema = z.string().regex(/^[a-f0-9]{24}$/);

export const IssueShareSchema = z.object({
  token: IssueShareTokenSchema.nullable(),
});

export const SharedIssueSchema = z.object({
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullish().transform((value) => value ?? ""),
  status: z.string(),
  status_label: z.string().optional(),
});

export type IssueShare = z.infer<typeof IssueShareSchema>;
export type SharedIssue = z.infer<typeof SharedIssueSchema>;
