"use client";
// CEREBRO-PATCH(agent-infisical-secrets): agent settings tab to manage Infisical folder grants.
// CEREBRO-PATCH(user-infisical-folders): grants are constrained to the folders the
// agent owner has been allowed by an admin — the tab is a picker over that allow-list,
// never free-text, so a folder outside the list can't even be selected.

import { useEffect, useMemo, useState } from "react";
import { Loader2, Save } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Agent, AgentInfisicalFolder } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { toast } from "sonner";
import { useT } from "../../../i18n";

function folderKey(folder: AgentInfisicalFolder): string {
  return `${folder.environment} ${folder.secret_path || "/"}`;
}

export function InfisicalFoldersTab({
  agent,
  readOnly = false,
  onDirtyChange,
}: {
  agent: Agent;
  readOnly?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");

  // Folders the owner is allowed to grant — the universe of selectable rows.
  const { data: allowed, isLoading: loadingAllowed } = useQuery({
    queryKey: ["agent-infisical-allowed-folders", agent.id],
    queryFn: () => api.listAgentAllowedInfisicalFolders(agent.id),
    enabled: !readOnly,
  });
  // Folders currently granted to this agent.
  const { data: granted, isLoading: loadingGranted } = useQuery({
    queryKey: ["agent-infisical-folders", agent.id],
    queryFn: () => api.listAgentInfisicalFolders(agent.id),
    enabled: !readOnly,
  });

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [original, setOriginal] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!granted) return;
    const keys = new Set(granted.map(folderKey));
    setSelected(keys);
    setOriginal(keys);
  }, [granted]);

  const allowedFolders = useMemo(() => allowed ?? [], [allowed]);

  const dirty = useMemo(() => {
    if (selected.size !== original.size) return true;
    for (const k of selected) if (!original.has(k)) return true;
    return false;
  }, [selected, original]);

  useEffect(() => {
    onDirtyChange?.(!readOnly && dirty);
  }, [dirty, onDirtyChange, readOnly]);

  const toggle = (folder: AgentInfisicalFolder) => {
    const key = folderKey(folder);
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const handleSave = async () => {
    const folders = allowedFolders.filter((f) => selected.has(folderKey(f)));
    setSaving(true);
    try {
      const saved = await api.replaceAgentInfisicalFolders(agent.id, folders);
      const keys = new Set(saved.map(folderKey));
      setSelected(keys);
      setOriginal(keys);
      toast.success(t(($) => $.tab_body.infisical.saved_toast));
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.tab_body.infisical.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  if (readOnly) {
    return (
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.infisical.readonly)}
      </p>
    );
  }

  const isLoading = loadingAllowed || loadingGranted;

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.infisical.intro)}
      </p>

      {isLoading ? (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          {t(($) => $.tab_body.infisical.loading)}
        </div>
      ) : allowedFolders.length === 0 ? (
        <p className="text-xs italic text-muted-foreground">
          {t(($) => $.tab_body.infisical.no_allowed)}
        </p>
      ) : (
        <div className="space-y-2">
          {allowedFolders.map((folder) => {
            const key = folderKey(folder);
            return (
              <label
                key={key}
                className="flex cursor-pointer items-center gap-3 rounded-md border border-border px-3 py-2 hover:bg-muted/50"
              >
                <Checkbox
                  checked={selected.has(key)}
                  onCheckedChange={() => toggle(folder)}
                />
                <span className="font-mono text-xs">
                  {folder.environment}
                  <span className="text-muted-foreground">
                    {folder.secret_path || "/"}
                  </span>
                </span>
              </label>
            );
          })}
        </div>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.common.unsaved_changes)}
          </span>
        )}
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="h-3.5 w-3.5" />
          )}
          {t(($) => $.tab_body.common.save)}
        </Button>
      </div>
    </div>
  );
}
