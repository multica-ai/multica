"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { api, clientErrorMessage } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  isRuntimeUsableForUser,
  runtimeDisplayLabel,
  runtimeListOptions,
} from "@multica/core/runtimes";
import {
  workspaceImportPreviewOptions,
  workspaceKeys,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import { CollectionPageHeaderAction } from "../layout/collection-page";
import { useT } from "../i18n";

export function ImportFromWorkspaceAction() {
  const { t } = useT("workspace");
  const current = useCurrentWorkspace();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const [open, setOpen] = useState(false);
  const others = workspaces.filter((w) => w.id !== current?.id);
  if (!current || others.length === 0) return null;
  return (
    <>
      <CollectionPageHeaderAction
        icon={Download}
        variant="outline"
        label={t(($) => $.import.action)}
        onClick={() => setOpen(true)}
      />
      <ImportFromWorkspaceDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

export function ImportFromWorkspaceDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workspace");
  const current = useCurrentWorkspace();
  const currentUser = useAuthStore((s) => s.user);
  const queryClient = useQueryClient();
  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const { data: runtimes = [] } = useQuery({
    ...runtimeListOptions(current?.id ?? ""),
    enabled: !!current?.id && open,
  });
  const others = workspaces.filter((w) => w.id !== current?.id);
  const usableRuntimes = runtimes.filter((rt) =>
    isRuntimeUsableForUser(rt, currentUser?.id ?? null),
  );

  const [sourceId, setSourceId] = useState("");
  const [runtimeId, setRuntimeId] = useState("");
  const [selectedAgents, setSelectedAgents] = useState<Set<string>>(new Set());
  const [selectedSquads, setSelectedSquads] = useState<Set<string>>(new Set());

  const previewQuery = useQuery({
    ...workspaceImportPreviewOptions(current?.id ?? "", sourceId),
    enabled: open && !!current?.id && !!sourceId,
  });
  const preview = previewQuery.data;

  useEffect(() => {
    if (!open) {
      setSourceId("");
      setRuntimeId("");
      setSelectedAgents(new Set());
      setSelectedSquads(new Set());
    }
  }, [open]);

  useEffect(() => {
    if (!preview) return;
    setSelectedAgents(new Set(preview.agents.map((a) => a.id)));
    setSelectedSquads(new Set(preview.squads.map((s) => s.id)));
  }, [preview]);

  const selectedCount = selectedAgents.size + selectedSquads.size;
  const canSubmit =
    !!current &&
    !!sourceId &&
    !!runtimeId &&
    selectedCount > 0 &&
    !previewQuery.isLoading;

  const mutation = useMutation({
    mutationFn: () =>
      api.importFromWorkspace(current!.id, {
        source_workspace_id: sourceId,
        runtime_id: runtimeId,
        agent_ids: [...selectedAgents],
        squad_ids: [...selectedSquads],
      }),
    onSuccess: (result) => {
      const agents = result.agents.filter((i) => i.status === "created").length;
      const squads = result.squads.filter((i) => i.status === "created").length;
      const skipped =
        result.agents.filter((i) => i.status === "skipped").length +
        result.squads.filter((i) => i.status === "skipped").length;
      toast.success(
        skipped > 0
          ? t(($) => $.import.success_partial, { agents, squads, skipped })
          : t(($) => $.import.success, { agents, squads }),
      );
      queryClient.invalidateQueries({ queryKey: workspaceKeys.agents(current!.id) });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(current!.id) });
      onOpenChange(false);
    },
    onError: (error) => {
      toast.error(clientErrorMessage(error) ?? t(($) => $.import.failed));
    },
  });

  const sourceOptions = useMemo(
    () => others.map((w) => ({ id: w.id, name: w.name })),
    [others],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t(($) => $.import.title)}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="import-source">
              {t(($) => $.import.source_label)}
            </Label>
            <select
              id="import-source"
              className="h-8 w-full rounded-md border border-input bg-background px-2 text-body"
              value={sourceId}
              onChange={(e) => setSourceId(e.target.value)}
            >
              <option value="">
                {t(($) => $.import.source_placeholder)}
              </option>
              {sourceOptions.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="import-runtime">
              {t(($) => $.import.runtime_label)}
            </Label>
            {usableRuntimes.length === 0 ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.import.runtime_empty)}
              </p>
            ) : (
              <select
                id="import-runtime"
                className="h-8 w-full rounded-md border border-input bg-background px-2 text-body"
                value={runtimeId}
                onChange={(e) => setRuntimeId(e.target.value)}
              >
                <option value="">
                  {t(($) => $.import.runtime_placeholder)}
                </option>
                {usableRuntimes.map((rt) => (
                  <option key={rt.id} value={rt.id}>
                    {runtimeDisplayLabel(rt)}
                  </option>
                ))}
              </select>
            )}
          </div>

          {sourceId && previewQuery.isLoading ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.import.loading)}
            </p>
          ) : null}

          {preview && preview.agents.length === 0 && preview.squads.length === 0 ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.import.empty_source)}
            </p>
          ) : null}

          {preview && preview.agents.length > 0 ? (
            <fieldset className="space-y-2">
              <legend className="text-caption font-medium text-muted-foreground">
                {t(($) => $.import.agents_section)}
              </legend>
              <div className="max-h-40 space-y-1 overflow-y-auto">
                {preview.agents.map((agent) => (
                  <label
                    key={agent.id}
                    className="flex cursor-pointer items-center gap-2 text-body"
                  >
                    <Checkbox
                      checked={selectedAgents.has(agent.id)}
                      onCheckedChange={() => {
                        setSelectedAgents((current) => {
                          const next = new Set(current);
                          if (next.has(agent.id)) next.delete(agent.id);
                          else next.add(agent.id);
                          return next;
                        });
                      }}
                    />
                    <span className="min-w-0 truncate">{agent.name}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          ) : null}

          {preview && preview.squads.length > 0 ? (
            <fieldset className="space-y-2">
              <legend className="text-caption font-medium text-muted-foreground">
                {t(($) => $.import.squads_section)}
              </legend>
              <div className="max-h-40 space-y-1 overflow-y-auto">
                {preview.squads.map((squad) => (
                  <label
                    key={squad.id}
                    className="flex cursor-pointer items-center gap-2 text-body"
                  >
                    <Checkbox
                      checked={selectedSquads.has(squad.id)}
                      onCheckedChange={() => {
                        setSelectedSquads((current) => {
                          const next = new Set(current);
                          if (next.has(squad.id)) next.delete(squad.id);
                          else next.add(squad.id);
                          return next;
                        });
                      }}
                    />
                    <span className="min-w-0 truncate">{squad.name}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          ) : null}

          <p className="text-caption text-muted-foreground">
            {t(($) => $.import.hint)}
          </p>
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.import.cancel)}
          </Button>
          <Button
            type="button"
            disabled={!canSubmit || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending
              ? t(($) => $.import.submitting)
              : t(($) => $.import.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
