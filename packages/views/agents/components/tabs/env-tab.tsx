"use client";

import { useCallback, useEffect, useState } from "react";
import { Eye, Loader2, Lock } from "lucide-react";
import { api } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { EnvEditor, entriesToEnvMap, envMapToEntries, type EnvEditorEntry } from "../../../common/env-editor";
import { useT } from "../../../i18n";

// Env values never reach this component until the user clicks
// "Reveal & edit" — the agent resource feed no longer carries
// custom_env at all after MUL-2600. Until then we display only the
// configured-key count from `agent.custom_env_key_count`, which is
// safe because it's not the values themselves.

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
  const [revealed, setRevealed] = useState<EnvEditorEntry[] | null>(null);
  const [originalMap, setOriginalMap] = useState<Record<string, string>>({});
  const [revealing, setRevealing] = useState(false);
  const [saving, setSaving] = useState(false);

  const keyCount = agent.custom_env_key_count ?? 0;

  const currentEnvMap = revealed ? entriesToEnvMap(revealed) : originalMap;
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

  const handleSave = async () => {
    if (revealed === null) return;
    const keys = revealed.filter((e) => e.key.trim()).map((e) => e.key.trim());
    const uniqueKeys = new Set(keys);
    if (uniqueKeys.size < keys.length) {
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
            <p className="flex items-center gap-2 text-sm font-medium">
              <Lock className="h-3.5 w-3.5 text-muted-foreground" />
              {keyCount > 0
                ? t(($) => $.tab_body.env.not_revealed_title, {
                    count: keyCount,
                  })
                : t(($) => $.tab_body.env.not_revealed_empty)}
            </p>
            <p className="text-xs text-muted-foreground">
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
    <EnvEditor
      entries={revealed}
      onChange={setRevealed}
      onSave={handleSave}
      saving={saving}
      dirty={dirty}
      intro={
        <>
          {t(($) => $.tab_body.env.intro_prefix)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">{"ANTHROPIC_API_KEY"}</code>
          {t(($) => $.tab_body.env.intro_separator)}
          <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">{"ANTHROPIC_BASE_URL"}</code>
          {t(($) => $.tab_body.env.intro_suffix)}
        </>
      }
      addLabel={t(($) => $.tab_body.common.add)}
      saveLabel={t(($) => $.tab_body.common.save)}
      unsavedLabel={t(($) => $.tab_body.common.unsaved_changes)}
      keyPlaceholder={t(($) => $.tab_body.env.key_placeholder)}
      valuePlaceholder={t(($) => $.tab_body.env.value_placeholder)}
      showValueLabel={t(($) => $.tab_body.env.show_value_aria)}
      hideValueLabel={t(($) => $.tab_body.env.hide_value_aria)}
      removeLabel={t(($) => $.tab_body.env.remove_aria)}
      emptyLabel={t(($) => $.tab_body.env.empty_editable)}
    />
  );
}
