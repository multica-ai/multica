import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  AgentTask,
  Comment,
  CreateIssueRequest,
  Issue,
  IssueReaction,
  IssueSubscriber,
  ListIssuesResponse,
  Reaction,
  TimelineEntry,
  UpdateIssueRequest,
} from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

function updateIssueInList(list: Issue[], issueId: string, updates: Partial<Issue>): Issue[] {
  return list.map((issue) => (issue.id === issueId ? { ...issue, ...updates } : issue));
}

function removeIssueFromList(list: Issue[], issueId: string): Issue[] {
  return list.filter((issue) => issue.id !== issueId);
}

function patchIssueLists(
  queryClient: ReturnType<typeof useQueryClient>,
  workspaceId: string | null,
  updater: (issues: Issue[]) => Issue[],
) {
  if (!workspaceId) return;

  queryClient.setQueriesData<ListIssuesResponse>(
    {
      predicate: (query) =>
        query.queryKey[0] === "issues" &&
        query.queryKey[1] === "lists" &&
        query.queryKey.includes(workspaceId),
    },
    (existing) => {
      if (!existing) return existing;
      return {
        ...existing,
        issues: updater(existing.issues),
      };
    },
  );
}

function patchIssueDetail(
  queryClient: ReturnType<typeof useQueryClient>,
  issueId: string,
  updater: (issue: Issue | null) => Issue | null,
) {
  queryClient.setQueryData<Issue | null>(queryKeys.issues.detail(issueId), (existing) =>
    updater(existing ?? null),
  );
}

export function useIssueMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  const createIssueMutation = useMutation({
    mutationFn: async (data: CreateIssueRequest) => api.createIssue(data),
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => {
        if (issues.some((item) => item.id === issue.id)) return issues;
        return [...issues, issue];
      });
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
    },
  });

  const updateIssueMutation = useMutation({
    mutationFn: async ({ issueId, updates }: { issueId: string; updates: Partial<UpdateIssueRequest> }) =>
      api.updateIssue(issueId, updates),
    onMutate: async ({ issueId, updates }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });

      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });
      const previousDetail = queryClient.getQueryData<Issue | null>(queryKeys.issues.detail(issueId)) ?? null;

      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issueId, updates));
      patchIssueDetail(queryClient, issueId, (issue) => (issue ? { ...issue, ...updates } : issue));

      return { previousLists, previousDetail };
    },
    onError: (_error, variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
      queryClient.setQueryData(queryKeys.issues.detail(variables.issueId), context?.previousDetail ?? null);
    },
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issue.id, issue));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
    },
  });

  const addIssueLabelMutation = useMutation({
    mutationFn: async ({ issueId, labelId, name, color }: { issueId: string; labelId?: string; name?: string; color?: string }) =>
      api.addIssueLabel(issueId, {
        ...(labelId ? { label_id: labelId } : {}),
        ...(name ? { name } : {}),
        ...(color ? { color } : {}),
      }),
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issue.id, issue));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
      if (workspaceId) {
        queryClient.invalidateQueries({ queryKey: queryKeys.workspace.labels(workspaceId) });
      }
    },
  });

  const removeIssueLabelMutation = useMutation({
    mutationFn: async ({ issueId, labelId }: { issueId: string; labelId: string }) =>
      api.removeIssueLabel(issueId, labelId),
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issue.id, issue));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
    },
  });

  const addIssueDependencyMutation = useMutation({
    mutationFn: async ({ issueId, dependencyIssueId, type }: { issueId: string; dependencyIssueId: string; type: string }) =>
      api.addIssueDependency(issueId, { issue_id: dependencyIssueId, type }),
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issue.id, issue));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
    },
  });

  const removeIssueDependencyMutation = useMutation({
    mutationFn: async ({ issueId, dependencyId }: { issueId: string; dependencyId: string }) =>
      api.removeIssueDependency(issueId, dependencyId),
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => updateIssueInList(issues, issue.id, issue));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
    },
  });

  const deleteIssueMutation = useMutation({
    mutationFn: async ({ issueId }: { issueId: string }) => {
      await api.deleteIssue(issueId);
      return issueId;
    },
    onMutate: async ({ issueId }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });
      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });
      const previousDetail = queryClient.getQueryData<Issue | null>(queryKeys.issues.detail(issueId)) ?? null;

      patchIssueLists(queryClient, workspaceId, (issues) => removeIssueFromList(issues, issueId));
      queryClient.removeQueries({ queryKey: queryKeys.issues.detail(issueId) });
      return { previousLists, previousDetail };
    },
    onError: (_error, variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
      queryClient.setQueryData(queryKeys.issues.detail(variables.issueId), context?.previousDetail ?? null);
    },
  });

  const archiveIssueMutation = useMutation({
    mutationFn: async ({ issueId }: { issueId: string }) => api.archiveIssue(issueId),
    onMutate: async ({ issueId }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });
      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });
      const previousDetail = queryClient.getQueryData<Issue | null>(queryKeys.issues.detail(issueId)) ?? null;

      patchIssueLists(queryClient, workspaceId, (issues) => removeIssueFromList(issues, issueId));
      patchIssueDetail(queryClient, issueId, (issue) => (issue ? { ...issue, archived_at: new Date().toISOString() } : issue));
      return { previousLists, previousDetail };
    },
    onError: (_error, variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
      queryClient.setQueryData(queryKeys.issues.detail(variables.issueId), context?.previousDetail ?? null);
    },
    onSuccess: (issue) => {
      patchIssueLists(queryClient, workspaceId, (issues) => removeIssueFromList(issues, issue.id));
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.all() });
      if (workspaceId) void queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  const restoreIssueMutation = useMutation({
    mutationFn: async ({ issueId }: { issueId: string }) => api.restoreIssue(issueId),
    onSuccess: (issue) => {
      queryClient.setQueryData(queryKeys.issues.detail(issue.id), issue);
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.all() });
    },
  });

  const batchUpdateMutation = useMutation({
    mutationFn: async ({ issueIds, updates }: { issueIds: string[]; updates: Partial<UpdateIssueRequest> }) => {
      await api.batchUpdateIssues(issueIds, updates);
      return { issueIds, updates };
    },
    onMutate: async ({ issueIds, updates }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });
      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });

      patchIssueLists(queryClient, workspaceId, (issues) =>
        issues.map((issue) => (issueIds.includes(issue.id) ? { ...issue, ...updates } : issue)),
      );

      issueIds.forEach((issueId) => {
        patchIssueDetail(queryClient, issueId, (issue) => (issue ? { ...issue, ...updates } : issue));
      });

      return { previousLists };
    },
    onError: (_error, _variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
    },
  });

  const batchDeleteMutation = useMutation({
    mutationFn: async ({ issueIds }: { issueIds: string[] }) => {
      await api.batchDeleteIssues(issueIds);
      return issueIds;
    },
    onMutate: async ({ issueIds }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });
      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });

      patchIssueLists(queryClient, workspaceId, (issues) =>
        issues.filter((issue) => !issueIds.includes(issue.id)),
      );

      issueIds.forEach((issueId) => {
        queryClient.removeQueries({ queryKey: queryKeys.issues.detail(issueId) });
      });

      return { previousLists };
    },
    onError: (_error, _variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
    },
  });

  const batchArchiveMutation = useMutation({
    mutationFn: async ({ issueIds }: { issueIds: string[] }) => {
      await api.batchArchiveIssues(issueIds);
      return issueIds;
    },
    onMutate: async ({ issueIds }) => {
      await queryClient.cancelQueries({ queryKey: queryKeys.issues.all() });
      const previousLists = queryClient.getQueriesData<ListIssuesResponse>({
        predicate: (query) => query.queryKey[0] === "issues" && query.queryKey[1] === "lists",
      });

      patchIssueLists(queryClient, workspaceId, (issues) =>
        issues.filter((issue) => !issueIds.includes(issue.id)),
      );

      issueIds.forEach((issueId) => {
        patchIssueDetail(queryClient, issueId, (issue) => (issue ? { ...issue, archived_at: new Date().toISOString() } : issue));
      });

      return { previousLists };
    },
    onError: (_error, _variables, context) => {
      context?.previousLists.forEach(([key, value]) => {
        queryClient.setQueryData(key, value);
      });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues.all() });
      if (workspaceId) void queryClient.invalidateQueries({ queryKey: queryKeys.inbox.all(workspaceId) });
    },
  });

  return {
    createIssue: (data: CreateIssueRequest) => createIssueMutation.mutateAsync(data),
    updateIssue: (issueId: string, updates: Partial<UpdateIssueRequest>) =>
      updateIssueMutation.mutateAsync({ issueId, updates }),
    deleteIssue: (issueId: string) => deleteIssueMutation.mutateAsync({ issueId }),
    archiveIssue: (issueId: string) => archiveIssueMutation.mutateAsync({ issueId }),
    restoreIssue: (issueId: string) => restoreIssueMutation.mutateAsync({ issueId }),
    batchUpdateIssues: (issueIds: string[], updates: Partial<UpdateIssueRequest>) =>
      batchUpdateMutation.mutateAsync({ issueIds, updates }),
    batchDeleteIssues: (issueIds: string[]) => batchDeleteMutation.mutateAsync({ issueIds }),
    batchArchiveIssues: (issueIds: string[]) => batchArchiveMutation.mutateAsync({ issueIds }),
    addIssueLabel: (issueId: string, data: { labelId?: string; name?: string; color?: string }) =>
      addIssueLabelMutation.mutateAsync({ issueId, ...data }),
    removeIssueLabel: (issueId: string, labelId: string) =>
      removeIssueLabelMutation.mutateAsync({ issueId, labelId }),
    addIssueDependency: (issueId: string, dependencyIssueId: string, type: string) =>
      addIssueDependencyMutation.mutateAsync({ issueId, dependencyIssueId, type }),
    removeIssueDependency: (issueId: string, dependencyId: string) =>
      removeIssueDependencyMutation.mutateAsync({ issueId, dependencyId }),
    isMutating:
      createIssueMutation.isPending ||
      updateIssueMutation.isPending ||
      deleteIssueMutation.isPending ||
      archiveIssueMutation.isPending ||
      restoreIssueMutation.isPending ||
      batchUpdateMutation.isPending ||
      batchDeleteMutation.isPending ||
      batchArchiveMutation.isPending ||
      addIssueLabelMutation.isPending ||
      removeIssueLabelMutation.isPending ||
      addIssueDependencyMutation.isPending ||
      removeIssueDependencyMutation.isPending,
  };
}

function commentToTimelineEntry(comment: Comment): TimelineEntry {
  return {
    type: "comment",
    id: comment.id,
    actor_type: comment.author_type,
    actor_id: comment.author_id,
    content: comment.content,
    parent_id: comment.parent_id,
    created_at: comment.created_at,
    updated_at: comment.updated_at,
    comment_type: comment.type,
    reactions: comment.reactions ?? [],
  };
}

export function useIssueTimelineMutations(issueId: string) {
  const queryClient = useQueryClient();

  const createCommentMutation = useMutation({
    mutationFn: async ({ content, type, parentId, attachmentIds }: { content: string; type?: string; parentId?: string; attachmentIds?: string[] }) =>
      api.createComment(issueId, content, type, parentId, attachmentIds),
    onSuccess: (comment) => {
      queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) => {
        if (existing.some((entry) => entry.id === comment.id)) return existing;
        return [...existing, commentToTimelineEntry(comment)];
      });
    },
  });

  const updateCommentMutation = useMutation({
    mutationFn: async ({ commentId, content }: { commentId: string; content: string }) =>
      api.updateComment(commentId, content),
    onSuccess: (comment) => {
      queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) =>
        existing.map((entry) => (entry.id === comment.id ? commentToTimelineEntry(comment) : entry)),
      );
    },
  });

  const deleteCommentMutation = useMutation({
    mutationFn: async ({ commentId }: { commentId: string }) => {
      await api.deleteComment(commentId);
      return commentId;
    },
    onSuccess: (commentId) => {
      queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) => {
        const idsToRemove = new Set<string>([commentId]);
        let added = true;
        while (added) {
          added = false;
          for (const entry of existing) {
            if (entry.parent_id && idsToRemove.has(entry.parent_id) && !idsToRemove.has(entry.id)) {
              idsToRemove.add(entry.id);
              added = true;
            }
          }
        }
        return existing.filter((entry) => !idsToRemove.has(entry.id));
      });
    },
  });

  const toggleCommentReactionMutation = useMutation({
    mutationFn: async ({
      commentId,
      emoji,
      existingReactionId,
    }: {
      commentId: string;
      emoji: string;
      existingReactionId?: string;
    }) => {
      if (existingReactionId) {
        await api.removeReaction(commentId, emoji);
        return { commentId, emoji, reaction: null as Reaction | null, existingReactionId };
      }

      const reaction = await api.addReaction(commentId, emoji);
      return { commentId, emoji, reaction, existingReactionId: null as string | null };
    },
    onSuccess: ({ commentId, emoji, reaction, existingReactionId }) => {
      queryClient.setQueryData<TimelineEntry[]>(queryKeys.issues.timeline(issueId), (existing = []) =>
        existing.map((entry) => {
          if (entry.id !== commentId) return entry;

          const reactions = entry.reactions ?? [];
          if (!reaction) {
            return {
              ...entry,
              reactions: reactions.filter((item) => item.id !== existingReactionId),
            };
          }

          if (reactions.some((item) => item.id === reaction.id)) return entry;
          return { ...entry, reactions: [...reactions, reaction] };
        }),
      );
    },
  });

  return {
    submitComment: (content: string, attachmentIds?: string[]) =>
      createCommentMutation.mutateAsync({ content, attachmentIds }),
    submitReply: (parentId: string, content: string, attachmentIds?: string[]) =>
      createCommentMutation.mutateAsync({ content, type: "comment", parentId, attachmentIds }),
    editComment: (commentId: string, content: string) =>
      updateCommentMutation.mutateAsync({ commentId, content }),
    deleteComment: (commentId: string) => deleteCommentMutation.mutateAsync({ commentId }),
    toggleCommentReaction: (commentId: string, emoji: string, existingReactionId?: string) =>
      toggleCommentReactionMutation.mutateAsync({ commentId, emoji, existingReactionId }),
    submitting: createCommentMutation.isPending,
  };
}

export function useIssueSubscribersMutations(issueId: string) {
  const queryClient = useQueryClient();

  const mutateSubscriber = useMutation({
    mutationFn: async ({ userId, userType, subscribe }: { userId: string; userType: "member" | "agent"; subscribe: boolean }) => {
      if (subscribe) {
        await api.subscribeToIssue(issueId, userId, userType);
      } else {
        await api.unsubscribeFromIssue(issueId, userId, userType);
      }

      return { userId, userType, subscribe };
    },
    onSuccess: ({ userId, userType, subscribe }) => {
      queryClient.setQueryData<IssueSubscriber[]>(queryKeys.issues.subscribers(issueId), (existing = []) => {
        if (subscribe) {
          if (existing.some((subscriber) => subscriber.user_id === userId && subscriber.user_type === userType)) {
            return existing;
          }
          return [
            ...existing,
            {
              issue_id: issueId,
              user_id: userId,
              user_type: userType,
              reason: "manual",
              created_at: new Date().toISOString(),
            },
          ];
        }

        return existing.filter(
          (subscriber) => !(subscriber.user_id === userId && subscriber.user_type === userType),
        );
      });
    },
  });

  return {
    toggleSubscriber: (userId: string, userType: "member" | "agent", subscribe: boolean) =>
      mutateSubscriber.mutateAsync({ userId, userType, subscribe }),
    updating: mutateSubscriber.isPending,
  };
}

export function useIssueReactionMutations(issueId: string) {
  const queryClient = useQueryClient();

  const toggleIssueReactionMutation = useMutation({
    mutationFn: async ({ emoji, existingReactionId }: { emoji: string; existingReactionId?: string }) => {
      if (existingReactionId) {
        await api.removeIssueReaction(issueId, emoji);
        return { emoji, reaction: null as IssueReaction | null, existingReactionId };
      }

      const reaction = await api.addIssueReaction(issueId, emoji);
      return { emoji, reaction, existingReactionId: null as string | null };
    },
    onSuccess: ({ emoji, reaction, existingReactionId }) => {
      queryClient.setQueryData<IssueReaction[]>(queryKeys.issues.reactions(issueId), (existing = []) => {
        if (!reaction) {
          return existing.filter((item) => item.id !== existingReactionId);
        }

        if (existing.some((item) => item.id === reaction.id)) return existing;
        return [...existing, reaction];
      });
    },
  });

  return {
    toggleIssueReaction: (emoji: string, existingReactionId?: string) =>
      toggleIssueReactionMutation.mutateAsync({ emoji, existingReactionId }),
    updating: toggleIssueReactionMutation.isPending,
  };
}

export function useIssueTaskMutations(issueId: string) {
  const queryClient = useQueryClient();

  const cancelTaskMutation = useMutation({
    mutationFn: ({ taskId }: { taskId: string }) => api.cancelTask(issueId, taskId),
    onSuccess: (task) => {
      queryClient.setQueryData<AgentTask | null>(queryKeys.tasks.activeByIssue(issueId), null);
      queryClient.setQueryData<AgentTask[]>(queryKeys.tasks.byIssue(issueId), (existing = []) => {
        const hasTask = existing.some((item) => item.id === task.id);
        if (!hasTask) return [task, ...existing];
        return existing.map((item) => (item.id === task.id ? task : item));
      });
    },
  });

  return {
    cancelTask: (taskId: string) => cancelTaskMutation.mutateAsync({ taskId }),
    cancelling: cancelTaskMutation.isPending,
  };
}

export function useSuggestLabelsMutation() {
  return useMutation({
    mutationFn: ({
      workspaceId,
      issueIds,
    }: {
      workspaceId: string;
      issueIds: string[];
    }) => api.suggestLabels(workspaceId, issueIds),
  });
}

export function useSuggestScheduleMutation() {
  return useMutation({
    mutationFn: ({
      workspaceId,
      issueIds,
    }: {
      workspaceId: string;
      issueIds: string[];
    }) => api.suggestSchedule(workspaceId, issueIds),
  });
}

export function useWorkspaceLabelMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((s) => s.workspace?.id ?? "");

  function invalidateLabels() {
    queryClient.invalidateQueries({ queryKey: queryKeys.workspace.labels(workspaceId) });
  }

  const createMutation = useMutation({
    mutationFn: (data: { name: string; color: string }) => api.createLabel(data),
    onSuccess: invalidateLabels,
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, ...data }: { id: string; name: string; color: string }) =>
      api.updateLabel(id, data),
    onSuccess: invalidateLabels,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => api.deleteLabel(id),
    onSuccess: invalidateLabels,
  });

  return {
    createLabel: (data: { name: string; color: string }) => createMutation.mutateAsync(data),
    updateLabel: (id: string, data: { name: string; color: string }) =>
      updateMutation.mutateAsync({ id, ...data }),
    deleteLabel: (id: string) => deleteMutation.mutateAsync(id),
    creating: createMutation.isPending,
    updating: updateMutation.isPending,
    deleting: deleteMutation.isPending,
  };
}
