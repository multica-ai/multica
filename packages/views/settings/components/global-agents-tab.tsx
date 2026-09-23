"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  globalAgentListOptions,
  useDeleteGlobalAgent,
} from "@multica/core/global-agents";
import type { GlobalAgent } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT } from "../../i18n";
import { GlobalAgentFormDialog } from "./global-agent-form-dialog";
import { GlobalAgentWorkspacesDialog } from "./global-agent-workspaces-dialog";
import { SettingsCard, SettingsSection, SettingsTab } from "./settings-layout";

type OpenDialog =
  | { kind: "create" }
  | { kind: "edit"; agent: GlobalAgent }
  | { kind: "workspaces"; agent: GlobalAgent }
  | { kind: "delete"; agent: GlobalAgent }
  | null;

/**
 * Account-level agents (#8775): defined once, enabled per workspace. The list
 * and every mutation are scoped to the signed-in user, not to the workspace
 * the settings page happens to be opened in.
 */
export function GlobalAgentsTab() {
  const { t } = useT("settings");
  const listQuery = useQuery(globalAgentListOptions());
  const deleteMutation = useDeleteGlobalAgent();
  const [dialog, setDialog] = useState<OpenDialog>(null);

  const agents = listQuery.data ?? [];
  // Dialogs read the freshest cached copy so links and fields stay current
  // after an enable/disable or a save made from the dialog itself.
  const current = (agent: GlobalAgent) =>
    agents.find((item) => item.id === agent.id) ?? agent;

  const handleDelete = async (agent: GlobalAgent) => {
    try {
      await deleteMutation.mutateAsync(agent);
      toast.success(t(($) => $.global_agents.delete_dialog.deleted_toast));
      setDialog(null);
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : t(($) => $.global_agents.delete_dialog.delete_failed_toast),
      );
    }
  };

  const createButton = (
    <Button size="sm" onClick={() => setDialog({ kind: "create" })}>
      <Plus className="size-4" aria-hidden="true" />
      {t(($) => $.global_agents.create)}
    </Button>
  );

  return (
    <SettingsTab
      title={t(($) => $.global_agents.title)}
      description={t(($) => $.global_agents.description)}
    >
      <SettingsSection action={agents.length > 0 ? createButton : undefined}>
        {listQuery.isLoading ? (
          <SettingsCard>
            {Array.from({ length: 2 }).map((_, i) => (
              <div key={i} className="flex items-center gap-3 px-4 py-3.5">
                <Skeleton className="size-8 rounded-full" />
                <div className="flex-1 space-y-1.5">
                  <Skeleton className="h-4 w-40" />
                  <Skeleton className="h-3 w-64" />
                </div>
              </div>
            ))}
          </SettingsCard>
        ) : agents.length === 0 ? (
          <Card>
            <CardContent className="flex flex-col items-start gap-3">
              <p className="text-caption text-muted-foreground">
                {listQuery.isError
                  ? t(($) => $.global_agents.load_failed)
                  : t(($) => $.global_agents.empty)}
              </p>
              {listQuery.isError ? null : createButton}
            </CardContent>
          </Card>
        ) : (
          <SettingsCard>
            {agents.map((agent) => (
              <GlobalAgentRow
                key={agent.id}
                agent={agent}
                onWorkspaces={() => setDialog({ kind: "workspaces", agent })}
                onEdit={() => setDialog({ kind: "edit", agent })}
                onDelete={() => setDialog({ kind: "delete", agent })}
              />
            ))}
          </SettingsCard>
        )}
      </SettingsSection>

      {dialog?.kind === "create" ? (
        <GlobalAgentFormDialog onClose={() => setDialog(null)} />
      ) : null}
      {dialog?.kind === "edit" ? (
        <GlobalAgentFormDialog
          agent={current(dialog.agent)}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {dialog?.kind === "workspaces" ? (
        <GlobalAgentWorkspacesDialog
          agent={current(dialog.agent)}
          onClose={() => setDialog(null)}
        />
      ) : null}
      {dialog?.kind === "delete" ? (
        <AlertDialog
          open
          onOpenChange={(open) => {
            if (!open && !deleteMutation.isPending) setDialog(null);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.global_agents.delete_dialog.title, {
                  name: dialog.agent.name,
                })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.global_agents.delete_dialog.description)}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={deleteMutation.isPending}>
                {t(($) => $.global_agents.delete_dialog.cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                variant="destructive"
                disabled={deleteMutation.isPending}
                aria-busy={deleteMutation.isPending || undefined}
                onClick={() => void handleDelete(current(dialog.agent))}
              >
                {t(($) => $.global_agents.delete_dialog.confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      ) : null}
    </SettingsTab>
  );
}

function GlobalAgentRow({
  agent,
  onWorkspaces,
  onEdit,
  onDelete,
}: {
  agent: GlobalAgent;
  onWorkspaces: () => void;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("settings");
  const enabledIn = agent.links
    .filter((link) => link.archived !== true)
    .map((link) => link.workspace_name || link.workspace_slug);

  return (
    <div className="flex items-center gap-3 px-4 py-3.5">
      <ActorAvatar
        name={agent.name}
        initials={agent.name.slice(0, 2).toUpperCase()}
        avatarUrl={agent.avatar_url}
        isAgent
        size="lg"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate text-body font-medium">{agent.name}</div>
        {agent.description ? (
          <div className="truncate text-caption text-muted-foreground">
            {agent.description}
          </div>
        ) : null}
        <div className="truncate text-caption text-muted-foreground">
          {enabledIn.length > 0
            ? t(($) => $.global_agents.enabled_in, {
                workspaces: enabledIn.join(", "),
              })
            : t(($) => $.global_agents.not_enabled)}
        </div>
      </div>
      <Button variant="outline" size="sm" onClick={onWorkspaces}>
        {t(($) => $.global_agents.manage_workspaces)}
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.global_agents.actions_aria, {
                name: agent.name,
              })}
            />
          }
        >
          <MoreHorizontal className="size-4" aria-hidden="true" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuItem onClick={onEdit}>
            <Pencil className="size-3.5" aria-hidden="true" />
            {t(($) => $.global_agents.edit)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            <Trash2 className="size-3.5" aria-hidden="true" />
            {t(($) => $.global_agents.delete)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
