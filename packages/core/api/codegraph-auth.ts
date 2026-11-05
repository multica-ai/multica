import { z } from "zod";
export const GraphCredentialSchema = z.object({
  id: z.string().min(1),
  name: z.string(),
  service_url: z.string(),
  repository_count: z.number().int().nonnegative(),
  updated_at: z.string(),
});
export const GraphCredentialsSchema = z.object({
  items: z.array(GraphCredentialSchema),
  configured: z.boolean(),
});
export const GraphSavedSchema = z.object({ id: z.string().min(1) });
export const GraphReadableSchema = z.object({ readable: z.literal(true) });
export const GraphAuthSavedSchema = z.object({ saved: z.literal(true) });
export type GraphCredential = z.infer<typeof GraphCredentialSchema>;
export type GraphAuthInput = {
  auth_mode: "token" | "anonymous";
  credential_id?: string;
  repo_url: string;
  branch: string;
};
