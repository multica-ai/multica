"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { spaceKeys, spaceListOptions } from "@multica/core/spaces/queries";
import { memberListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { AppLink } from "../../navigation";
import { SpaceIcon } from "../../spaces/components/space-icon";
import { useT } from "../../i18n";

export function WorkspaceSpacesTab() {
  const { t } = useT("settings");
  const { t: tSpaces } = useT("spaces");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const paths = useWorkspacePaths();
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const queryClient = useQueryClient();
  const { data: spaces = [], isLoading } = useQuery(spaceListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [savingSpaceId, setSavingSpaceId] = useState<string | null>(null);

  const currentMember = members.find((member) => member.user_id === userId);
  const canManage = currentMember?.role === "owner" || currentMember?.role === "admin";
  const activeSpaces = spaces.filter((space) => !space.archived_at);
  const archivedSpaces = spaces.filter((space) => !!space.archived_at);

  const setDefaultSpace = async (spaceId: string) => {
    if (!workspace || !canManage) return;
    setSavingSpaceId(spaceId);
    try {
      const updated = await api.updateWorkspace(workspace.id, {
        default_space_id: spaceId,
      });
      queryClient.setQueryData<Workspace[]>(
        workspaceKeys.list(),
        (old) => old?.map((item) => (item.id === updated.id ? updated : item)),
      );
      await queryClient.invalidateQueries({ queryKey: spaceKeys.all(wsId) });
      toast.success(t(($) => $.spaces.toast_default_updated));
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t(($) => $.spaces.toast_default_failed),
      );
    } finally {
      setSavingSpaceId(null);
    }
  };

  return (
    <div className="space-y-8">
      <section className="space-y-4">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <h2 className="text-sm font-semibold">{t(($) => $.spaces.title)}</h2>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.spaces.description)}
            </p>
          </div>
          {canManage && (
            <Button
              size="sm"
              render={<AppLink href={paths.spaceNew()} />}
              nativeButton={false}
            >
              {tSpaces(($) => $.page.new_space)}
            </Button>
          )}
        </div>

        {isLoading ? (
          <p className="text-sm text-muted-foreground">
            {tSpaces(($) => $.page.loading)}
          </p>
        ) : (
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {activeSpaces.map((space, index) => (
              <div
                key={space.id}
                className={`flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center ${
                  index > 0 ? "border-t border-border/50" : ""
                }`}
              >
                <div className="flex min-w-0 flex-1 items-center gap-3">
                  <SpaceIcon space={space} className="size-4 shrink-0" />
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <AppLink
                        href={paths.spaceSettings(space.key)}
                        className="truncate text-sm font-medium hover:underline"
                      >
                        {space.name}
                      </AppLink>
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {space.key}
                      </Badge>
                      <Badge variant="secondary" className="text-[10px]">
                        {space.visibility === "private"
                          ? tSpaces(($) => $.form.visibility_private)
                          : tSpaces(($) => $.form.visibility_open)}
                      </Badge>
                      {space.is_default && (
                        <Badge className="text-[10px]">
                          {tSpaces(($) => $.state.default)}
                        </Badge>
                      )}
                    </div>
                    <p className="mt-0.5 text-xs text-muted-foreground">
                      {space.is_member
                        ? t(($) => $.spaces.joined)
                        : t(($) => $.spaces.not_joined)}
                    </p>
                  </div>
                </div>
                {canManage && !space.is_default && space.visibility === "open" && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={savingSpaceId !== null}
                    onClick={() => void setDefaultSpace(space.id)}
                  >
                    {savingSpaceId === space.id
                      ? t(($) => $.spaces.setting_default)
                      : t(($) => $.spaces.set_default)}
                  </Button>
                )}
              </div>
            ))}
          </div>
        )}
      </section>

      {archivedSpaces.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-sm font-semibold">
            {t(($) => $.spaces.archived_title, { count: archivedSpaces.length })}
          </h2>
          <div className="overflow-hidden rounded-xl ring-1 ring-foreground/10">
            {archivedSpaces.map((space, index) => (
              <div
                key={space.id}
                className={`flex items-center gap-3 px-4 py-3 ${
                  index > 0 ? "border-t border-border/50" : ""
                }`}
              >
                <SpaceIcon space={space} className="size-4 shrink-0 opacity-60" />
                <AppLink
                  href={paths.spaceSettings(space.key)}
                  className="min-w-0 flex-1 truncate text-sm text-muted-foreground hover:underline"
                >
                  {space.name}
                </AppLink>
                <Badge variant="outline">{tSpaces(($) => $.state.archived)}</Badge>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}
