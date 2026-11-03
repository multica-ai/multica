import { infiniteQueryOptions, type QueryClient } from "@tanstack/react-query";
import type { MemberRole } from "@multica/core/types";
import { api } from "@/data/api";
import { memberListOptions } from "@/data/queries/members";
import { agentListOptions } from "@/data/queries/agents";
import { aiKeys, type AISessionClient } from "./client";
import { agentReferenceUnavailable, prepareAIMessage, REFERENCE_PAGE_SIZE, type AIReference, type ReferenceKind } from "./references";

export interface ReferenceOption extends AIReference {
  detail: string;
  unavailable?: NonNullable<ReturnType<typeof agentReferenceUnavailable>>;
}
export interface ReferencePage { items: ReferenceOption[]; nextOffset?: number | string }

export async function loadReferencePage(client: AISessionClient, qc: QueryClient, session: string, kind: ReferenceKind, search: string, pageParam: number | string, signal: AbortSignal): Promise<ReferencePage> {
  const scope = client.scope;
  const query = search.trim();
  const needle = query.toLocaleLowerCase();
  const offset = typeof pageParam === "number" ? pageParam : 0;
  if (kind === "issue") {
    // Search exposes no total: one lookahead result determines whether to page.
    const page = query
      ? await api.searchIssues({ q: query, offset, limit: REFERENCE_PAGE_SIZE + 1, include_closed: true }, { signal })
      : await api.listIssues({ workspace_id: scope.workspaceId, offset, limit: REFERENCE_PAGE_SIZE + 1, sort_by: "updated_at", sort_direction: "desc" }, { signal });
    const issues = page.issues.filter((entry) => entry.workspace_id === scope.workspaceId);
    return { items: issues.slice(0, REFERENCE_PAGE_SIZE).map((entry) => ({ kind, id: entry.id, name: `${entry.identifier} · ${entry.title}`, detail: entry.status })),
      nextOffset: issues.length > REFERENCE_PAGE_SIZE ? offset + REFERENCE_PAGE_SIZE : undefined };
  }
  let items: ReferenceOption[];
  if (kind === "session") {
    const page = await client.sessionPage({ search: query, excludeId: session, cursor: typeof pageParam === "string" ? pageParam : undefined, limit: REFERENCE_PAGE_SIZE }, signal);
    items = page.summaries.map((entry) => {
        // Match desktop's internal workdir boundary; generated IDs are not folder labels.
        const automatic = /(?:^|[\\/])local-conversations[\\/](?:[a-f0-9]{64}[\\/]workdirs|workdirs[\\/]\d+)[\\/][^\\/]+[\\/]?$/.test(entry.cwd);
        const directory = automatic ? null : entry.cwd.split(/[/\\]/).filter(Boolean).at(-1);
        return { kind, id: entry.conversation_id, name: entry.title, detail: [entry.provider === "claude" ? "Claude Code" : entry.provider === "codex" ? "Codex" : entry.provider, directory].filter(Boolean).join(" · ") };
      });
    return { items, nextOffset: page.next_cursor ?? undefined };
  } else {
    const [agents, members] = await Promise.all([
      qc.fetchQuery({ ...agentListOptions(scope.workspaceId), staleTime: 30_000 }),
      qc.fetchQuery({ ...memberListOptions(scope.workspaceId), staleTime: 30_000 }),
    ]);
    if (signal.aborted) throw new Error("cancelled");
    const role = members.find((entry) => entry.user_id === scope.userId)?.role ?? null;
    items = agents.filter((entry) => entry.workspace_id === scope.workspaceId && !entry.archived_at && (!needle || entry.name.toLocaleLowerCase().includes(needle)))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((entry) => ({ kind, id: entry.id, name: entry.name, detail: "", unavailable: agentReferenceUnavailable(entry, scope.userId, role) ?? undefined }));
  }
  return { items: items.slice(offset, offset + REFERENCE_PAGE_SIZE), nextOffset: items.length > offset + REFERENCE_PAGE_SIZE ? offset + REFERENCE_PAGE_SIZE : undefined };
}

export const referencePageOptions = (client: AISessionClient, qc: QueryClient, session: string, kind: ReferenceKind, search: string) => infiniteQueryOptions({
  queryKey: [...(kind === "session" ? aiKeys.browse(client.scope) : aiKeys.scope(client.scope)), "references", session, kind, search.trim()],
  initialPageParam: 0 as number | string,
  queryFn: ({ pageParam, signal }) => loadReferencePage(client, qc, session, kind, search, pageParam, signal),
  getNextPageParam: (page) => page.nextOffset,
  retry: false,
});

export function resolveAIMessage(client: AISessionClient, text: string, references: AIReference[]) {
  const { workspaceId, userId } = client.scope;
  return prepareAIMessage(text, references, { workspaceId, userId,
    session: (id) => client.snapshot(id),
    issue: (id) => api.getIssue(id),
    agentCatalog: async () => {
      const [agents, members] = await Promise.all([api.listAgents(), api.listMembers(workspaceId)]);
      const role: MemberRole | null = members.find((entry) => entry.user_id === userId)?.role ?? null;
      return { agents, role };
    },
  });
}
