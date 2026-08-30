import { useEffect, useState } from "react";
import type {
  AgentToolPolicy,
  AgentToolPolicyRule,
  ReplaceAgentToolPolicyRequest,
} from "@multica/core/operational-controls";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";

const EMPTY_RULE: AgentToolPolicyRule = {
  transport_kind: "managed_mcp",
  server_key: "",
  tool_name: "",
  schema_digest: "",
  effect: "allow",
};

export function ToolPolicyEditor({
  policy,
  canEdit,
  onSave,
}: {
  policy: AgentToolPolicy;
  canEdit: boolean;
  onSave: (request: ReplaceAgentToolPolicyRequest) => Promise<void>;
}) {
  const { t } = useT("operations");
  const [rules, setRules] = useState(policy.rules);
  const [saving, setSaving] = useState(false);

  useEffect(() => setRules(policy.rules), [policy]);

  const updateRule = (index: number, patch: Partial<AgentToolPolicyRule>) => {
    setRules((current) => current.map((rule, i) => i === index ? { ...rule, ...patch } : rule));
  };

  const save = async () => {
    setSaving(true);
    try {
      await onSave({ expected_revision: policy.revision ?? 0, rules });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3">
        <p className="text-body font-medium">{t(($) => $.policy.default_deny)}</p>
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.policy.metadata_warning)}
        </p>
      </div>

      {rules.map((rule, index) => (
        <div key={index} className="grid gap-3 rounded-lg border p-4 md:grid-cols-2">
          <label className="space-y-1 text-caption">
            <span>{t(($) => $.policy.transport)}</span>
            <select
              className="h-9 w-full rounded-md border bg-background px-3"
              value={rule.transport_kind}
              disabled={!canEdit}
              onChange={(event) => updateRule(index, { transport_kind: event.target.value as AgentToolPolicyRule["transport_kind"] })}
            >
              <option value="managed_mcp">{t(($) => $.policy.managed_mcp)}</option>
              <option value="managed_native">{t(($) => $.policy.managed_native)}</option>
            </select>
          </label>
          <PolicyInput label={t(($) => $.policy.server_key)} value={rule.server_key} disabled={!canEdit} onChange={(server_key) => updateRule(index, { server_key })} />
          <PolicyInput label={t(($) => $.policy.tool_name)} value={rule.tool_name} disabled={!canEdit} onChange={(tool_name) => updateRule(index, { tool_name })} />
          <PolicyInput label={t(($) => $.policy.schema_digest)} value={rule.schema_digest} disabled={!canEdit} onChange={(schema_digest) => updateRule(index, { schema_digest })} />
          <label className="space-y-1 text-caption">
            <span>{t(($) => $.policy.effect)}</span>
            <select
              className="h-9 w-full rounded-md border bg-background px-3"
              value={rule.effect}
              disabled={!canEdit}
              onChange={(event) => updateRule(index, { effect: event.target.value as AgentToolPolicyRule["effect"] })}
            >
              <option value="allow">{t(($) => $.policy.allow)}</option>
              <option value="require_approval">{t(($) => $.policy.require_approval)}</option>
            </select>
          </label>
          {canEdit && (
            <div className="flex items-end">
              <Button variant="ghost" size="sm" onClick={() => setRules((current) => current.filter((_, i) => i !== index))}>
                {t(($) => $.policy.remove_rule)}
              </Button>
            </div>
          )}
        </div>
      ))}

      {canEdit && (
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => setRules((current) => [...current, { ...EMPTY_RULE }])}>
            {t(($) => $.policy.add_rule)}
          </Button>
          <Button size="sm" disabled={saving} onClick={() => void save()}>
            {saving ? t(($) => $.policy.saving) : t(($) => $.policy.save)}
          </Button>
        </div>
      )}
    </div>
  );
}

function PolicyInput({ label, value, disabled, onChange }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="space-y-1 text-caption">
      <span>{label}</span>
      <input className="h-9 w-full rounded-md border bg-background px-3 font-mono text-caption" aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}
