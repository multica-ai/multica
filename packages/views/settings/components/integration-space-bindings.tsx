"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ListIntegrationBindingsResponse } from "@multica/core/types";
import { SpaceMultiPicker } from "../../spaces";
import { useT } from "../../i18n";

const bindingKey = (workspaceId: string) =>
  ["integration-bindings", workspaceId] as const;

export function IntegrationSpaceBindings() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: bindingKey(wsId),
    queryFn: () => api.listIntegrationBindings(),
    enabled: !!wsId,
  });
  const replace = useMutation({
    mutationFn: ({
      provider,
      connectionId,
      spaceIds,
    }: {
      provider: string;
      connectionId: string;
      spaceIds: string[];
    }) => api.replaceIntegrationBindings(provider, connectionId, spaceIds),
    onSuccess: (next) => {
      queryClient.setQueryData<ListIntegrationBindingsResponse>(
        bindingKey(wsId),
        next,
      );
      toast.success(t(($) => $.integration_bindings.toast_saved));
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.integration_bindings.toast_failed),
      );
    },
  });

  if (!data || data.connections.length === 0) return null;

  return (
    <section className="space-y-4 rounded-lg border p-4">
      <div className="flex items-start gap-3">
        <span className="rounded-md bg-muted p-2">
          <Link2 className="size-4 text-muted-foreground" />
        </span>
        <div>
          <h2 className="text-sm font-semibold">
            {t(($) => $.integration_bindings.title)}
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            {t(($) => $.integration_bindings.description)}
          </p>
        </div>
      </div>
      <div className="divide-y rounded-md border">
        {data.connections.map((connection) => (
          <div
            key={`${connection.provider}:${connection.connection_id}`}
            className="flex items-center gap-3 p-3"
          >
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">
                {connection.display_name}
              </p>
              <p className="text-xs capitalize text-muted-foreground">
                {connection.provider}
              </p>
            </div>
            <SpaceMultiPicker
              spaceIds={connection.space_ids}
              disabled={!data.can_manage || replace.isPending}
              onChange={(spaceIds) =>
                replace.mutate({
                  provider: connection.provider,
                  connectionId: connection.connection_id,
                  spaceIds,
                })
              }
            />
          </div>
        ))}
      </div>
    </section>
  );
}
