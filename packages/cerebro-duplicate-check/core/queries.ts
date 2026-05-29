import { useEffect, useState } from "react";
import { useQuery, type UseQueryResult, keepPreviousData } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { useWorkspaceId } from "@multica/core/hooks";
import type { DuplicateMatch, DuplicateMatchVerdict } from "./types";

const MatchSchema = z.object({
  id: z.string(),
  identifier: z.string().default(""),
  title: z.string().default(""),
  status: z.string().default(""),
  url: z.string().default(""),
  // Leave verdict lenient — backend may evolve the enum. The component
  // filters down to the actionable two values before rendering.
  verdict: z.string(),
  reason: z.string().default(""),
});
const ResponseSchema = z.object({
  matches: z.array(MatchSchema).default([]),
});
const EMPTY: { matches: DuplicateMatch[] } = { matches: [] };

function isActionable(v: string): v is DuplicateMatchVerdict {
  return v === "duplicate" || v === "related";
}

// parseSimilarResponse is exported so the test suite can feed malformed
// responses through the same path the hook uses — the upstream rule for
// every new endpoint (see CLAUDE.md "API Response Compatibility").
export function parseSimilarResponse(raw: unknown): DuplicateMatch[] {
  const parsed = parseWithFallback(raw, ResponseSchema, EMPTY, {
    endpoint: "POST /api/cerebro/issues/check-similar",
  });
  return parsed.matches
    .filter((m) => isActionable(m.verdict))
    .map((m) => ({ ...m, verdict: m.verdict as DuplicateMatchVerdict }));
}

async function fetchSimilar(payload: {
  title: string;
  description?: string;
  projectId?: string;
}): Promise<DuplicateMatch[]> {
  const raw = await api.checkSimilarCerebroIssues<unknown>({
    title: payload.title,
    description: payload.description,
    project_id: payload.projectId,
  });
  return parseSimilarResponse(raw);
}

// Title + description are debounced before they reach the query so we
// don't fire a request on every keystroke. 600 ms matches the editor's
// own onUpdate debounce, so the request lands once the user actually paused.
const DEBOUNCE_MS = 600;
const MIN_TITLE_CHARS = 6;

/**
 * useDuplicateCheck wires the create-issue panel to the backend.
 *
 * - Returns `data` as `DuplicateMatch[]` (filtered to actionable verdicts).
 * - Only fires when title.trim() has ≥ MIN_TITLE_CHARS — short titles produce
 *   noise and waste a gateway call.
 * - Honours the consumer's `enabled` switch so the caller can turn the panel
 *   off entirely when the feature flag is false.
 */
export function useDuplicateCheck(args: {
  title: string;
  description?: string;
  projectId?: string;
  enabled?: boolean;
}): UseQueryResult<DuplicateMatch[]> {
  const wsId = useWorkspaceId();
  const debouncedTitle = useDebounced(args.title.trim(), DEBOUNCE_MS);
  const debouncedDesc = useDebounced((args.description ?? "").trim(), DEBOUNCE_MS);
  const enabled =
    (args.enabled ?? true) &&
    Boolean(wsId) &&
    debouncedTitle.length >= MIN_TITLE_CHARS;

  return useQuery({
    queryKey: [
      "cerebro",
      "duplicate-check",
      wsId,
      debouncedTitle,
      debouncedDesc,
      args.projectId ?? "",
    ],
    queryFn: () =>
      fetchSimilar({
        title: debouncedTitle,
        description: debouncedDesc || undefined,
        projectId: args.projectId,
      }),
    enabled,
    // The same draft can be revisited multiple times before the user
    // commits; cache for a minute so reopening the modal is instant.
    staleTime: 60 * 1000,
    placeholderData: keepPreviousData,
  });
}

function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const handle = setTimeout(() => setDebounced(value), ms);
    return () => clearTimeout(handle);
  }, [value, ms]);
  return debounced;
}
