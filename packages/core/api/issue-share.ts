import { z } from "zod";

export const IssueShareTokenSchema = z.string().regex(/^[a-f0-9]{24}$/);

export const IssueShareSchema = z.object({
  token: IssueShareTokenSchema.nullable(),
});

const SharedIssueCommentSchema = z.object({
  id: z.string(),
  author_type: z.enum(["member", "agent"]),
  author_name: z.string().nullish(),
  author_avatar_url: z.string().nullish(),
  content: z.string(),
  created_at: z.iso.datetime({ offset: true }),
  parent_id: z.string().nullish(),
  source_task_id: z.string().nullish(),
});

const SharedIssueRunSchema = z.object({
  id: z.string(),
  status: z.string(),
  completed_at: z.iso.datetime({ offset: true }),
  started_at: z.iso.datetime({ offset: true }).nullish(),
  agent_name: z.string().nullish(),
  agent_avatar_url: z.string().nullish(),
});

const SharedIssueDeliverableSchema = z.object({
  id: z.string(),
  filename: z.string(),
  content_type: z.string(),
  size: z.number().int().nonnegative(),
});

export const SharedIssueMessagesSchema = z.object({
  messages: z.array(z.object({
    seq: z.number().int().nonnegative(),
    type: z.string(),
    content: z.string(),
    created_at: z.iso.datetime({ offset: true }),
  })),
  next_after: z.number().int().nonnegative().nullable(),
});

export const SharedIssueSchema = z.object({
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullish().transform((value) => value ?? ""),
  status: z.string(),
  status_label: z.string().optional(),
  comments: z.array(SharedIssueCommentSchema).default([]),
  runs: z.array(SharedIssueRunSchema).default([]),
  deliverables: z.array(SharedIssueDeliverableSchema).default([]),
});

export type IssueShare = z.infer<typeof IssueShareSchema>;
export type SharedIssue = z.infer<typeof SharedIssueSchema>;
export type SharedIssueComment = z.infer<typeof SharedIssueCommentSchema>;
export type SharedIssueRun = z.infer<typeof SharedIssueRunSchema>;
export type SharedIssueDeliverable = z.infer<typeof SharedIssueDeliverableSchema>;
export type SharedIssueMessages = z.infer<typeof SharedIssueMessagesSchema>;
