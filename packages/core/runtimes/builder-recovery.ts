import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import type { EmptyAgentBuilderSession } from "../api/schemas";
import { runtimeKeys } from "./queries";

export const builderRecoveryKeys = {
  profile: (wsId: string, profileId: string) =>
    ["empty-builder-sessions", wsId, profileId] as const,
};

export function useEmptyAgentBuilderSessions(
  wsId: string,
  profileId: string,
  enabled: boolean,
) {
  return useInfiniteQuery({
    queryKey: builderRecoveryKeys.profile(wsId, profileId),
    queryFn: async ({ pageParam }) => {
      const page = await api.listEmptyAgentBuilderSessions(
        wsId,
        profileId,
        pageParam,
      );
      if (page.nextCursor && page.nextCursor === pageParam)
        throw new Error("Repeated recovery cursor");
      return page;
    },
    initialPageParam: "",
    getNextPageParam: (page) => page.nextCursor || undefined,
    enabled,
    retry: false,
  });
}

export function useDiscardEmptyAgentBuilderSession(
  wsId: string,
  profileId: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (session: EmptyAgentBuilderSession) =>
      api.discardEmptyAgentBuilderSession(
        wsId,
        profileId,
        session.sessionId,
        session.snapshot,
      ),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: builderRecoveryKeys.profile(wsId, profileId),
      });
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
    },
  });
}

export type { EmptyAgentBuilderSession };
