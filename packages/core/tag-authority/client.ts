import type { QueryClient } from "@tanstack/react-query";
import { z } from "zod";

const roleSchema = z.enum(["owner", "admin", "member"]);
const capabilitiesSchema = z.object({
  collaborate: z.boolean(),
  viewMembers: z.boolean(),
  leaveWorkspace: z.boolean(),
  manageWorkspace: z.boolean(),
  manageOwners: z.boolean(),
  manageAdmins: z.boolean(),
  manageMembers: z.boolean(),
  inviteMembers: z.boolean(),
});
const workspaceSchema = z.object({
  id: z.string(),
  slug: z.string(),
  name: z.string(),
  role: roleSchema,
  authorityVersion: z.number().int().positive(),
  membershipGeneration: z.number().int().positive(),
  capabilities: capabilitiesSchema,
});
const memberSchema = z.object({
  userId: z.string(),
  name: z.string(),
  role: roleSchema,
  membershipGeneration: z.number().int().positive(),
});
const invitationSchema = z.object({
  id: z.string(),
  targetEmail: z.string(),
  role: z.enum(["admin", "member"]),
  status: z.enum(["pending", "accepted", "declined", "revoked", "expired"]),
  expiresAt: z.coerce.date(),
});
const commandSchema = z.object({
  ok: z.literal(true),
  replayed: z.boolean().optional(),
  workspaceId: z.string(),
  authorityVersion: z.number().int().positive(),
  membershipGeneration: z.number().int().positive(),
});
const switchSchema = z.object({
  ok: z.literal(true),
  workspace: z.object({ id: z.string(), slug: z.string() }),
  handoff: z.object({
    code: z.string(),
    expiresAt: z.coerce.date().optional(),
  }),
});
const invitationActionSchema = z.object({
  ok: z.literal(true),
  replayed: z.boolean().optional(),
  invitation: invitationSchema.optional(),
  status: z.string().optional(),
  workspaceId: z.string().optional(),
  authorityVersion: z.number().int().positive().optional(),
  membershipGeneration: z.number().int().positive().optional(),
});
const joinLinkSchema = z.object({
  ok: z.literal(true),
  token: z.string(),
  joinLink: z.object({
    id: z.string(),
    offeredRole: z.literal("member"),
    status: z.enum(["active", "revoked", "expired", "exhausted"]),
    maxClaims: z.number().int().positive(),
    claimCount: z.number().int().nonnegative(),
    expiresAt: z.coerce.date(),
  }),
});
const joinClaimSchema = z.object({
  ok: z.literal(true),
  workspaceId: z.string(),
  authorityVersion: z.number().int().positive(),
  membershipGeneration: z.number().int().positive(),
});

export type AuthorityWorkspace = z.infer<typeof workspaceSchema>;
export type AuthorityMember = z.infer<typeof memberSchema>;
export type AuthorityInvitation = z.infer<typeof invitationSchema>;
export type AuthorityRole = z.infer<typeof roleSchema>;

export class AuthorityClientError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
  ) {
    super(code);
    this.name = "AuthorityClientError";
  }
}

type Fetcher = (url: string, init?: RequestInit) => Promise<Response>;

async function readJson(response: Response) {
  if (response.status === 204) return undefined;
  try {
    return (await response.json()) as unknown;
  } catch {
    throw new AuthorityClientError("invalid_response", response.status);
  }
}

function errorCode(value: unknown) {
  const parsed = z
    .union([
      z.object({ error: z.string() }),
      z.object({ error: z.object({ code: z.string() }) }),
    ])
    .safeParse(value);
  if (!parsed.success) return "request_failed";
  return typeof parsed.data.error === "string"
    ? parsed.data.error
    : parsed.data.error.code;
}

export function createTagAuthorityClient({
  fetcher = fetch,
}: {
  fetcher?: Fetcher;
} = {}) {
  async function request<T>(input: {
    path: string;
    method: "DELETE" | "GET" | "PATCH" | "POST";
    body?: unknown;
    schema: z.ZodType<T>;
  }) {
    const headers: Record<string, string> = { accept: "application/json" };
    if (input.body !== undefined) headers["content-type"] = "application/json";
    const response = await fetcher(input.path, {
      credentials: "same-origin",
      headers,
      method: input.method,
      ...(input.body === undefined ? {} : { body: JSON.stringify(input.body) }),
    });
    const value = await readJson(response);
    if (!response.ok) {
      throw new AuthorityClientError(errorCode(value), response.status);
    }
    const parsed = input.schema.safeParse(value);
    if (!parsed.success) {
      throw new AuthorityClientError("invalid_response", response.status);
    }
    return parsed.data;
  }

  return {
    async listWorkspaces() {
      const result = await request({
        path: "/api/tag-authority/workspaces",
        method: "GET",
        schema: z.object({
          ok: z.literal(true),
          workspaces: z.array(workspaceSchema),
        }),
      });
      return result.workspaces;
    },

    async createWorkspace(input: {
      name: string;
      slug: string;
      idempotencyKey: string;
    }) {
      return await request({
        path: "/api/tag-authority/workspaces",
        method: "POST",
        body: input,
        schema: commandSchema.extend({ projectionReady: z.literal(false) }),
      });
    },

    async switchWorkspace(workspaceId: string) {
      return await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/switch`,
        method: "POST",
        schema: switchSchema,
      });
    },

    async listMembers(workspaceId: string) {
      const result = await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/members`,
        method: "GET",
        schema: z.object({
          ok: z.literal(true),
          members: z.array(memberSchema),
        }),
      });
      return result.members;
    },

    async changeMember(
      workspaceId: string,
      input: {
        targetUserId: string;
        role: AuthorityRole;
        status: "active" | "removed";
        expectedAuthorityVersion: number;
        idempotencyKey: string;
      },
    ) {
      return await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/members`,
        method: "PATCH",
        body: input,
        schema: commandSchema,
      });
    },

    async listInvitations(workspaceId: string) {
      const result = await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/invitations`,
        method: "GET",
        schema: z.object({ invitations: z.array(invitationSchema) }),
      });
      return result.invitations;
    },

    async issueInvitation(
      workspaceId: string,
      input: { targetEmail: string; role: "admin" | "member" },
    ) {
      return await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/invitations`,
        method: "POST",
        body: input,
        schema: invitationActionSchema,
      });
    },

    async actOnInvitation(invitationId: string, action: "resend" | "revoke") {
      return await request({
        path: `/api/tag-authority/invitations/${encodeURIComponent(invitationId)}`,
        method: "POST",
        body: { action },
        schema: invitationActionSchema,
      });
    },

    async acceptInvitation(token: string) {
      return await request({
        path: "/api/tag-authority/invitations/accept",
        method: "POST",
        body: { token },
        schema: invitationActionSchema,
      });
    },

    async declineInvitation(token: string) {
      return await request({
        path: "/api/tag-authority/invitations/decline",
        method: "POST",
        body: { token },
        schema: invitationActionSchema,
      });
    },

    async createJoinLink(
      workspaceId: string,
      input: { maxClaims: number; expiresAt: string },
    ) {
      return await request({
        path: `/api/tag-authority/workspaces/${encodeURIComponent(workspaceId)}/join-links`,
        method: "POST",
        body: input,
        schema: joinLinkSchema,
      });
    },

    async revokeJoinLink(joinLinkId: string) {
      return await request({
        path: `/api/tag-authority/join-links/${encodeURIComponent(joinLinkId)}`,
        method: "DELETE",
        schema: z.object({
          ok: z.literal(true),
          status: z.enum(["revoked", "expired"]),
        }),
      });
    },

    async claimJoinLink(token: string) {
      return await request({
        path: "/api/tag-authority/join-links/claim",
        method: "POST",
        body: { token },
        schema: joinClaimSchema,
      });
    },

    async exchangeHandoff(input: { code: string; workspaceSlug: string }) {
      await request({
        path: "/api/tag/api/auth/vibes-handoff",
        method: "POST",
        body: {
          code: input.code,
          audience: "vibes-tag-local",
          workspaceSlug: input.workspaceSlug,
        },
        schema: z.undefined(),
      });
    },
  };
}

export type TagAuthorityClient = ReturnType<typeof createTagAuthorityClient>;
export const tagAuthorityClient = createTagAuthorityClient();

export const authorityKeys = {
  all: ["tag-authority"] as const,
  workspaces: () => ["tag-authority", "workspaces"] as const,
  members: (workspaceId: string) =>
    ["tag-authority", "workspaces", workspaceId, "members"] as const,
  invitations: (workspaceId: string) =>
    ["tag-authority", "workspaces", workspaceId, "invitations"] as const,
};

export async function waitForAuthorityWorkspace(
  client: Pick<ReturnType<typeof createTagAuthorityClient>, "listWorkspaces">,
  workspaceId: string,
  options: {
    timeoutMs?: number;
    intervalMs?: number;
    delay?: (milliseconds: number) => Promise<void>;
  } = {},
) {
  const timeoutMs = options.timeoutMs ?? 30_000;
  const intervalMs = options.intervalMs ?? 750;
  const delay =
    options.delay ??
    ((milliseconds: number) =>
      new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds)));
  const deadline = Date.now() + timeoutMs;

  do {
    const workspace = (await client.listWorkspaces()).find(
      (candidate) => candidate.id === workspaceId,
    );
    if (workspace) return workspace;
    if (Date.now() >= deadline) break;
    await delay(intervalMs);
  } while (Date.now() <= deadline);

  throw new AuthorityClientError("projection_pending", 202);
}

export async function completeAuthorityWorkspaceSwitch(input: {
  client: Pick<
    ReturnType<typeof createTagAuthorityClient>,
    "switchWorkspace" | "exchangeHandoff"
  >;
  workspaceId: string;
  destination: string;
  queryClient: QueryClient;
  disconnectRealtime: () => void;
  clearClientSelection: () => void;
  navigate: (href: string) => void;
}) {
  const switched = await input.client.switchWorkspace(input.workspaceId);
  input.disconnectRealtime();
  await input.queryClient.cancelQueries();
  input.queryClient.clear();
  input.clearClientSelection();
  await input.client.exchangeHandoff({
    code: switched.handoff.code,
    workspaceSlug: switched.workspace.slug,
  });
  input.navigate(input.destination);
}
