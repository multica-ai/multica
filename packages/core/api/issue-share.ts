import { z } from "zod";

export const IssueShareTokenSchema = z.string().regex(/^[a-f0-9]{24}$/);

export const IssueShareSchema = z.object({
  token: IssueShareTokenSchema.nullable(),
});

const SharedIssueCommentSchema = z.object({
  id: z.string(),
  author_type: z.enum(["member", "agent"]),
  content: z.string(),
  created_at: z.iso.datetime({ offset: true }),
});

const SharedIssueRunSchema = z.object({
  id: z.string(),
  status: z.string(),
  completed_at: z.iso.datetime({ offset: true }),
});

export const SharedIssueSchema = z.object({
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullish().transform((value) => value ?? ""),
  status: z.string(),
  status_label: z.string().optional(),
  comments: z.array(SharedIssueCommentSchema).default([]),
  runs: z.array(SharedIssueRunSchema).default([]),
});

export type IssueShare = z.infer<typeof IssueShareSchema>;
export type SharedIssue = z.infer<typeof SharedIssueSchema>;
