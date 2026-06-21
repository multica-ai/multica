"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createChapter, listChapters, startFresh, updateChapter } from "./api";
import type { ChapterHandoffInput } from "./types";

const key = (issueId: string) => ["cerebro-chapters", issueId] as const;

export function useChapters(issueId: string) {
  return useQuery({
    queryKey: key(issueId),
    queryFn: () => listChapters(issueId),
  });
}

export function useStartFresh(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ChapterHandoffInput) => startFresh(issueId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(issueId) }),
  });
}

export function useCreateChapter(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: Parameters<typeof createChapter>[1]) => createChapter(issueId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(issueId) }),
  });
}

export function useUpdateChapter(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ chapterId, input }: { chapterId: string; input: Parameters<typeof updateChapter>[2] }) =>
      updateChapter(issueId, chapterId, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: key(issueId) }),
  });
}
