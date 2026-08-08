"use client";

import { useCallback, useEffect, useState } from "react";
import { Eye, Loader2, Lock, Plus, Save } from "lucide-react";
import { api } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { useT } from "../../../i18n";
import {
  createEnvEntry,
  EnvKeyValueEditor,
  envEntriesToMap,
  findDuplicateEnvKey,
  type EnvEntryDraft,
} from "../env-key-value-editor";

// Env values never reach this component until the user clicks
// "Reveal & edit" — the agent resource feed no longer carries
// custom_env at all after MUL-2600. Until then we display only the
// configured-key count from `agent.custom_env_key_count`, which is
// safe because it's not the values themselves.

// Values arrive masked: this tab reveals a whole agent's env at once, so
// every row starts hidden and the user unmasks the one they care about.
function envMapToEntries(env: Record<string, string>): EnvEntryDraft[] {
  return Object.entries(env).map(([key, value]) =>
    createEnvEntry({ key, value, visible: false }),
  );
}

export function EnvTab({
  agent,
  onDirtyChange,
  onSaved,
}: {
  agent: Agent;
  onDirtyChange?: (dirty: boolean) => void;
  // Notifier so the parent page can refresh its agent cache after a
  // successful PUT — the parent owns the `Agent` object the rest of
  // the page reads (name, has_custom_env, etc.). Optional so call
  // sites without invalidation logic stay simple.
  onSaved?: () => void;
}) {
  const { t } = useT("agents");

  // revealed === null means "haven't fetched yet"; revealed === [] is
  // a legitimate empty map after a successful reveal. We do NOT
  // pre-populate from `agent` here because the agent resource shape
  // no longer carries values — only the dedicated `/env` endpoint
  // does, and that endpoint writes an audit row per call so we never
  // fetch implicitly on mount.
  const [revealed, setRevealed] = useState<EnvEntryDraft[] | null>(null);
  const [originalMap, setOriginalMap] = useState<Record<string, string>>({});
  const [revealing, setRevealing] = useState(false);
  const [saving, setSaving] = useState(false);

  const keyCount = agent.custom_env_key_count ?? 0;

  const currentEnvMap = revealed ? envEntriesToMap(revealed) : originalMap;
  const dirty =
    revealed !== null &&
    JSON.stringify(currentEnvMap) !== JSON.stringify(originalMap);

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  const handleReveal = useCallback(async () => {
    setRevealing(true);
    try {
      const resp = await api.getAgentEnv(agent.id);
      const env = resp.custom_env ?? {};
      setOriginalMap(env);
      setRevealed(envMapToEntries(env));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.env.reveal_failed_toast),
      );
    } finally {
      setRevealing(false);
    }
  }, [agent.id, t]);

  const addEnvEntry = () => {
    setRevealed((prev) => [...(prev ?? []), createEnvEntry()]);
  };

  const handleSave = async () => {
    if (revealed === null) return;
    if (findDuplicateEnvKey(revealed) !== null) {
      toast.error(t(($) => $.tab_body.env.duplicate_keys_toast));
      return;
    }

    setSaving(true);
    try {
      const resp = await api.updateAgentEnv(agent.id, {
        custom_env: currentEnvMap,
      });
      const env = resp.custom_env ?? {};
      setOriginalMap(env);
      setRevealed(envMapToEntries(env));
      toast.success(t(($) => $.tab_body.env.saved_toast));
      onSaved?.();
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.tab_body.env.save_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  // Pre-reveal state: show count + Reveal button. We never auto-fetch
  // on mount so a member just navigating between tabs doesn't trigger
  // an audit-log entry; the reveal must be intentional.
  if (revealed === null) {
    return (
      <div className="space-y-4">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <p className="flex items-center gap-2 text-body font-medium">
              <Lock className="h-3.5 w-3.5 text-muted-foreground" />
              {keyCount > 0
                ? t(($) => $.tab_body.env.not_revealed_title, {
                    count: keyCount,
                  })
                : t(($) => $.tab_body.env.not_revealed_empty)}
            </p>
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.env.not_revealed_hint)}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={revealing}
            onClick={handleReveal}
            className="shrink-0"
          >
            {revealing ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Eye className="h-3.5 w-3.5" />
            )}
            {revealing
              ? t(($) => $.tab_body.env.revealing)
              : t(($) => $.tab_body.env.reveal_action)}
          </Button>
        </div>
      </div>
    );
  }

  // Editable state — only entered after a successful reveal.
  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-3">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.tab_body.env.intro_prefix)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-micro">
            {"ANTHROPIC_API_KEY"}
          </code>
          {t(($) => $.tab_body.env.intro_separator)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-micro">
            {"ANTHROPIC_BASE_URL"}
          </code>
          {t(($) => $.tab_body.env.intro_suffix)}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={addEnvEntry}
          className="shrink-0"
        >
          <Plus className="h-3 w-3" />
          {t(($) => $.tab_body.common.add)}
        </Button>
      </div>

      {revealed.length > 0 ? (
        <EnvKeyValueEditor entries={revealed} onChange={setRevealed} />
      ) : (
        <p className="text-caption italic text-muted-foreground">
          {t(($) => $.tab_body.env.empty_editable)}
        </p>
      )}

      <div className="flex items-center justify-end gap-3">
        {dirty && (
          <span className="text-caption text-muted-foreground">{t(($) => $.tab_body.common.unsaved_changes)}</span>
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
