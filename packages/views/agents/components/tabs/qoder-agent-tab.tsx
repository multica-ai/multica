"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { qoderAgentId, qoderDefaultRepository } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { QoderAgentSelect } from "../qoder-agent-select";
import { useT } from "../../../i18n";

export function QoderAgentTab({
  agent,
  onSave,
  onDirtyChange,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
  onSave: (updates: {
    runtime_config: Record<string, unknown>;
  }) => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("runtimes");
  const savedAgentId = qoderAgentId(agent.runtime_config);
  const savedRepo = qoderDefaultRepository(agent.runtime_config);
  const [draft, setDraft] = useState<string | null>(null);
  const [repoUrl, setRepoUrl] = useState<string | null>(null);
  const [repoRef, setRepoRef] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const value = draft ?? savedAgentId;
  const url = repoUrl ?? savedRepo.url;
  const ref = repoRef ?? savedRepo.ref;
  const dirty =
    value !== savedAgentId ||
    url.trim() !== savedRepo.url ||
    ref.trim() !== savedRepo.ref;
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);
  const disabled = saving || !canEdit;
  return (
    <form
      className="space-y-5"
      onSubmit={async (event) => {
        event.preventDefault();
        if (!dirty || !value || disabled) return;
        setSaving(true);
        try {
          const { repository: _previous, ...rest } = agent.runtime_config;
          await onSave({
            runtime_config: {
              ...rest,
              qoder_agent_id: value,
              ...(url.trim()
                ? { repository: { url: url.trim(), ref: ref.trim() } }
                : {}),
            },
          });
          setDraft(null);
          setRepoUrl(null);
          setRepoRef(null);
          toast.success(t(($) => $.qoder.agent_saved));
        } catch (error) {
          toast.error(
            error instanceof Error
              ? error.message
              : t(($) => $.qoder.agent_save_failed),
          );
        } finally {
          setSaving(false);
        }
      }}
    >
      <QoderAgentSelect value={value} onChange={setDraft} disabled={disabled} />
      <fieldset className="space-y-3">
        <legend className="text-caption font-medium">
          {t(($) => $.qoder.default_repository)}
        </legend>
        <div className="grid gap-3 sm:grid-cols-[1fr_10rem]">
          <Input
            type="url"
            aria-label={t(($) => $.qoder.github_url)}
            placeholder="https://github.com/owner/repo"
            value={url}
            disabled={disabled}
            autoComplete="off"
            onChange={(event) => setRepoUrl(event.target.value)}
          />
          <Input
            aria-label={t(($) => $.qoder.default_branch)}
            placeholder={t(($) => $.qoder.default_branch_placeholder)}
            value={ref}
            disabled={disabled || !url.trim()}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => setRepoRef(event.target.value)}
          />
        </div>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.qoder.default_repository_hint)}
        </p>
      </fieldset>
      <Button type="submit" disabled={!dirty || !value || disabled}>
        {saving ? t(($) => $.qoder.working) : t(($) => $.qoder.save_agent)}
      </Button>
    </form>
  );
}
