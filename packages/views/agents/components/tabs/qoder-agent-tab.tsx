"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import { qoderAgentId } from "@multica/core/agents";
import { Button } from "@multica/ui/components/ui/button";
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
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const value = draft ?? qoderAgentId(agent.runtime_config);
  const dirty = value !== qoderAgentId(agent.runtime_config);
  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);
  return (
    <div className="space-y-4">
      <QoderAgentSelect
        value={value}
        onChange={setDraft}
        disabled={saving || !canEdit}
      />
      <Button
        disabled={!dirty || !value || saving || !canEdit}
        onClick={async () => {
          setSaving(true);
          try {
            await onSave({
              runtime_config: {
                ...agent.runtime_config,
                qoder_agent_id: value,
              },
            });
            setDraft(null);
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
        {saving ? t(($) => $.qoder.working) : t(($) => $.qoder.save_agent)}
      </Button>
    </div>
  );
}
