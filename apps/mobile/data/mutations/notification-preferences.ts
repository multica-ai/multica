/**
 * Mobile notification-preferences mutation. Mirrors the optimistic pattern of
 * packages/core/notification-preferences/mutations.ts — written here per the
 * "Mobile-owned updaters" rule (don't import web mutations; key shape is
 * independent and may drift).
 *
 * Optimistic policy: patch cache → fire PATCH → rollback on error → invalidate
 * on settle (mirrors mobile inbox mutations + CLAUDE.md "Mutations are
 * optimistic by default"). Toggle latency on cellular is real — the Switch
 * snapping back if the request hangs would look broken.
 */
import {
  useMutation,
  useQueryClient,
  type MutateOptions,
} from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import type {
  NotificationPreferenceResponse,
  NotificationPreferences,
} from "@multica/core/types";
import {
  applyNotificationPreferencePatch,
  deriveNotificationPreferencePatch,
  rollbackNotificationPreferencePatch,
} from "@multica/core/notification-preferences/patch";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import { notificationPreferenceKeys } from "@/data/queries/notification-preferences";

interface NotificationPreferenceMutationVariables {
  preferences: NotificationPreferences;
  patch: NotificationPreferences;
  workspaceId: string | null;
  workspaceSlug: string | null;
}

interface NotificationPreferenceMutationContext {
  previous: NotificationPreferenceResponse | undefined;
  key: readonly unknown[];
}

type ExternalMutationOptions = MutateOptions<
  NotificationPreferenceResponse,
  Error,
  NotificationPreferences,
  NotificationPreferenceMutationContext
>;

type InternalMutationOptions = MutateOptions<
  NotificationPreferenceResponse,
  Error,
  NotificationPreferenceMutationVariables,
  NotificationPreferenceMutationContext
>;

function mapMutationOptions(
  preferences: NotificationPreferences,
  options: ExternalMutationOptions | undefined,
): InternalMutationOptions | undefined {
  if (!options) return undefined;

  return {
    onSuccess: options.onSuccess
      ? (data, _variables, result, context) =>
          options.onSuccess?.(data, preferences, result, context)
      : undefined,
    onError: options.onError
      ? (error, _variables, result, context) =>
          options.onError?.(error, preferences, result, context)
      : undefined,
    onSettled: options.onSettled
      ? (data, error, _variables, result, context) =>
          options.onSettled?.(data, error, preferences, result, context)
      : undefined,
  };
}

export function useUpdateNotificationPreferences() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const workspaceSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const key = notificationPreferenceKeys.all(wsId);
  const renderedPreferences =
    qc.getQueryData<NotificationPreferenceResponse>(key)?.preferences ?? {};
  const renderedPreferencesRef = useRef(renderedPreferences);
  renderedPreferencesRef.current = renderedPreferences;

  // Match Core's concurrency contract: serialize writes per workspace and
  // capture the slug so queued work cannot follow a later workspace switch.
  const mutation = useMutation<
    NotificationPreferenceResponse,
    Error,
    NotificationPreferenceMutationVariables,
    NotificationPreferenceMutationContext
  >({
    mutationKey: key,
    scope: { id: `notification-preferences:${wsId ?? "unscoped"}` },
    mutationFn: ({ patch, workspaceSlug: targetWorkspaceSlug }) => {
      if (!targetWorkspaceSlug) {
        throw new Error(
          "Workspace context is required to update notifications",
        );
      }
      return api.updateNotificationPreferences(patch, targetWorkspaceSlug);
    },
    onMutate: async ({ patch, workspaceId }) => {
      const targetKey = notificationPreferenceKeys.all(workspaceId);
      await qc.cancelQueries({ queryKey: targetKey });
      const previous =
        qc.getQueryData<NotificationPreferenceResponse>(targetKey);
      qc.setQueryData<NotificationPreferenceResponse>(targetKey, (old) => ({
        ...(old ?? { workspace_id: workspaceId ?? "" }),
        preferences: applyNotificationPreferencePatch(
          old?.preferences ?? {},
          patch,
        ),
      }));
      return { previous, key: targetKey };
    },
    onError: (_error, { patch, workspaceId }, context) => {
      const targetKey =
        context?.key ?? notificationPreferenceKeys.all(workspaceId);
      qc.setQueryData<NotificationPreferenceResponse>(targetKey, (old) => ({
        ...(old ?? { workspace_id: workspaceId ?? "" }),
        preferences: rollbackNotificationPreferencePatch(
          old?.preferences ?? {},
          patch,
          context?.previous?.preferences ?? {},
        ),
      }));
    },
    onSettled: (_data, _error, { workspaceId }, context) => {
      const mutationKey = notificationPreferenceKeys.all(workspaceId);
      // The settling mutation is still counted; only the final queued write
      // should trigger an authoritative refetch.
      if (qc.isMutating({ mutationKey }) > 1) return;
      qc.invalidateQueries({ queryKey: context?.key ?? mutationKey });
    },
  });

  const mutate = useCallback(
    (
      preferences: NotificationPreferences,
      options?: ExternalMutationOptions,
    ) => {
      const patch = deriveNotificationPreferencePatch(
        renderedPreferencesRef.current,
        preferences,
      );
      mutation.mutate(
        { preferences, patch, workspaceId: wsId, workspaceSlug },
        mapMutationOptions(preferences, options),
      );
    },
    [mutation, workspaceSlug, wsId],
  );

  const mutateAsync = useCallback(
    (
      preferences: NotificationPreferences,
      options?: ExternalMutationOptions,
    ) => {
      const patch = deriveNotificationPreferencePatch(
        renderedPreferencesRef.current,
        preferences,
      );
      return mutation.mutateAsync(
        { preferences, patch, workspaceId: wsId, workspaceSlug },
        mapMutationOptions(preferences, options),
      );
    },
    [mutation, workspaceSlug, wsId],
  );

  return {
    ...mutation,
    variables: mutation.variables?.preferences,
    mutate,
    mutateAsync,
  };
}
