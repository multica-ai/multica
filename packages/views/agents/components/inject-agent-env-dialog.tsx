"use client";

import { useEffect, useState } from "react";
import { Loader2, Plus } from "lucide-react";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { useMergeAgentsEnv } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import {
  createEnvEntry,
  EnvKeyValueEditor,
  envEntriesToMap,
  findDuplicateEnvKey,
  type EnvEntryDraft,
} from "./env-key-value-editor";

/**
 * Quick environment-variable injection for one agent or many (MUL-5758).
 *
 * Add-or-overwrite only: every key the user does not type is left untouched on
 * every targeted agent. Deletion stays on the detail page's env tab, where the
 * user is looking at one agent's full map — a bulk delete of secrets is not
 * recoverable from this UI.
 *
 * This dialog never reveals an existing value. It does not call
 * `GET /api/agents/{id}/env`, so opening it writes no `agent_env_revealed`
 * audit row and pulls no plaintext secret into the browser — which is exactly
 * why bulk injection needed its own merge endpoint instead of a
 * read-modify-write against the wholesale-replace one.
 *
 * The consequence is that "will this key be added or overwritten?" cannot be
 * answered before the write. The result toast reports the split afterwards.
 */
export function InjectAgentEnvDialog({
  open,
  onOpenChange,
  agents,
  wsId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The agents to inject into. One entry is the single-agent case. */
  agents: Agent[];
  wsId: string;
}) {
  const { t } = useT("agents");
  const merge = useMergeAgentsEnv(wsId);
  const [entries, setEntries] = useState<EnvEntryDraft[]>([]);

  // One blank row on open, and never carry a previous session's draft (it
  // holds secret values) into the next one.
  useEffect(() => {
    if (!open) return;
    setEntries([createEnvEntry()]);
  }, [open]);

  const isBulk = agents.length > 1;
  const envMap = envEntriesToMap(entries);
  const duplicateKey = findDuplicateEnvKey(entries);
  const canSubmit =
    Object.keys(envMap).length > 0 && !duplicateKey && !merge.isPending;

  const handleSubmit = async () => {
    if (duplicateKey) {
      toast.error(t(($) => $.tab_body.env.duplicate_keys_toast));
      return;
    }
    try {
      const result = await merge.mutateAsync({
        agentIds: agents.map((a) => a.id),
        set: envMap,
      });
      onOpenChange(false);
      const added = result.results.reduce((n, r) => n + r.added_keys.length, 0);
      const overwritten = result.results.reduce(
        (n, r) => n + r.overwritten_keys.length,
        0,
      );
      toast.success(
        t(($) => $.inject_env_dialog.success_toast, {
          agents: result.results.length,
          added,
          overwritten,
        }),
      );
      if (result.skipped.length > 0) {
        toast.warning(
          t(($) => $.inject_env_dialog.skipped_toast, {
            count: result.skipped.length,
          }),
        );
      }
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.inject_env_dialog.failed_toast),
      );
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isBulk
              ? t(($) => $.inject_env_dialog.title_bulk, {
                  count: agents.length,
                })
              : t(($) => $.inject_env_dialog.title_single, {
                  name: agents[0]?.name ?? "",
                })}
          </DialogTitle>
          <DialogDescription>
            {t(($) => $.inject_env_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          <div className="flex items-center justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={merge.isPending}
              onClick={() => setEntries((prev) => [...prev, createEnvEntry()])}
            >
              <Plus className="h-3 w-3" />
              {t(($) => $.tab_body.common.add)}
            </Button>
          </div>

          <EnvKeyValueEditor
            entries={entries}
            onChange={setEntries}
            disabled={merge.isPending}
          />

          {duplicateKey && (
            <p className="text-caption text-destructive" aria-live="polite">
              {t(($) => $.tab_body.env.duplicate_keys_toast)}
            </p>
          )}

          {isBulk && (
            <div className="max-h-40 space-y-1 overflow-y-auto rounded-lg border p-2">
              {agents.map((agent) => (
                <div key={agent.id} className="flex items-center gap-2 px-1.5 py-1">
                  <ActorAvatar actorType="agent" actorId={agent.id} size="sm" />
                  <span className="min-w-0 flex-1 truncate text-body">
                    {agent.name}
                  </span>
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {t(($) => $.inject_env_dialog.current_key_count, {
                      count: agent.custom_env_key_count ?? 0,
                    })}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={merge.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.migrate_dialog.cancel)}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={!canSubmit}
            onClick={handleSubmit}
          >
            {merge.isPending ? (
              <Loader2 className="mr-1 size-3.5 animate-spin" />
            ) : null}
            {t(($) => $.inject_env_dialog.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
