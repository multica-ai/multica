"use client";
// CEREBRO-PATCH(agent-infisical-secrets): agent settings tab to manage Infisical secret refs.

import { useEffect, useState } from "react";
import { Loader2, Plus, Save, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Agent, AgentInfisicalSecret } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { toast } from "sonner";
import { useT } from "../../../i18n";

let nextInfisicalSecretId = 0;

type SecretEntry = AgentInfisicalSecret & { row_id: number };

function refsToEntries(refs: AgentInfisicalSecret[]): SecretEntry[] {
  return refs.map((ref) => ({ ...ref, row_id: nextInfisicalSecretId++ }));
}

function entriesToRefs(entries: SecretEntry[]): AgentInfisicalSecret[] {
  return entries
    .map((entry) => ({
      env_var_name: entry.env_var_name.trim(),
      secret_name: entry.secret_name.trim(),
      environment: entry.environment.trim(),
      secret_path: entry.secret_path.trim() || "/",
    }))
    .filter((entry) => entry.env_var_name || entry.secret_name || entry.environment);
}

function normalizeRefs(refs: AgentInfisicalSecret[]): AgentInfisicalSecret[] {
  return entriesToRefs(refsToEntries(refs));
}

export function InfisicalSecretsTab({
  agent,
  readOnly = false,
  onDirtyChange,
}: {
  agent: Agent;
  readOnly?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const { data: refs = [], isLoading } = useQuery({
    queryKey: ["agent-infisical-secrets", agent.id],
    queryFn: () => api.listAgentInfisicalSecrets(agent.id),
    enabled: !readOnly,
  });
  const [entries, setEntries] = useState<SecretEntry[]>(
    refsToEntries(agent.infisical_secrets ?? []),
  );
  const [originalRefs, setOriginalRefs] = useState<AgentInfisicalSecret[]>(
    normalizeRefs(agent.infisical_secrets ?? []),
  );
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setEntries(refsToEntries(refs));
    setOriginalRefs(normalizeRefs(refs));
  }, [refs]);

  const currentRefs = entriesToRefs(entries);
  const dirty = JSON.stringify(currentRefs) !== JSON.stringify(originalRefs);

  useEffect(() => {
    onDirtyChange?.(!readOnly && dirty);
  }, [dirty, onDirtyChange, readOnly]);

  const addEntry = () => {
    setEntries([
      ...entries,
      {
        row_id: nextInfisicalSecretId++,
        env_var_name: "",
        secret_name: "",
        environment: "production",
        secret_path: "/",
      },
    ]);
  };

  const updateEntry = (
    index: number,
    field: keyof AgentInfisicalSecret,
    value: string,
  ) => {
    setEntries(
      entries.map((entry, i) =>
        i === index ? { ...entry, [field]: value } : entry,
      ),
    );
  };

  const removeEntry = (index: number) => {
    setEntries(entries.filter((_, i) => i !== index));
  };

  const handleSave = async () => {
    const names = currentRefs.map((entry) => entry.env_var_name.toUpperCase());
    if (new Set(names).size !== names.length) {
      toast.error(t(($) => $.tab_body.infisical.duplicate_env_toast));
      return;
    }
    setSaving(true);
    try {
      const saved = await api.replaceAgentInfisicalSecrets(agent.id, currentRefs);
      setEntries(refsToEntries(saved));
      setOriginalRefs(normalizeRefs(saved));
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

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.infisical.intro)}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEntry}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          {t(($) => $.tab_body.common.add)}
        </Button>
      </div>

      {isLoading ? (
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          {t(($) => $.tab_body.infisical.loading)}
        </div>
      ) : entries.length > 0 ? (
        <div className="space-y-2">
          {entries.map((entry, index) => (
            <div
              key={entry.row_id}
              className="grid gap-2 md:grid-cols-[1fr_1fr_130px_110px_32px] md:items-center"
            >
              <Input
                value={entry.env_var_name}
                onChange={(e) => updateEntry(index, "env_var_name", e.target.value)}
                placeholder="DATABASE_URL"
                className="font-mono text-xs"
              />
              <Input
                value={entry.secret_name}
                onChange={(e) => updateEntry(index, "secret_name", e.target.value)}
                placeholder="DATABASE_URL"
                className="font-mono text-xs"
              />
              <Input
                value={entry.environment}
                onChange={(e) => updateEntry(index, "environment", e.target.value)}
                placeholder="production"
                className="font-mono text-xs"
              />
              <Input
                value={entry.secret_path}
                onChange={(e) => updateEntry(index, "secret_path", e.target.value)}
                placeholder="/"
                className="font-mono text-xs"
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => removeEntry(index)}
                className="text-muted-foreground hover:text-destructive"
                aria-label="Remove Infisical secret"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </div>
          ))}
        </div>
      ) : (
        <p className="text-xs italic text-muted-foreground">
          {t(($) => $.tab_body.infisical.empty)}
        </p>
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
