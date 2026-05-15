// CEREBRO-PATCH(agent-sandbox-tab): JEH-1088 — persona sandbox dropdown wired
// to GET /api/persona/sandboxes + PATCH /api/agents/{id}.persona_sandbox.
// Non-admin members see a disabled control with an explanation; admins/owners
// can re-assign. Runtime-cap badge + cap-baseret disable lives in JEH-1156.
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, Lock } from "lucide-react";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import type { Agent } from "@multica/core/types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { useT } from "../../../i18n";

interface PersonaSandbox {
  name: string;
  description: string;
  system_owned: boolean;
}

export function SandboxTab({
  agent,
  onSave,
}: {
  agent: Agent;
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  const isAdmin = myRole === "owner" || myRole === "admin";

  const {
    data: sandboxes = [],
    isLoading: sandboxesLoading,
  } = useQuery<PersonaSandbox[]>({
    queryKey: ["persona", "sandboxes"],
    queryFn: () => api.listPersonaSandboxes(),
  });

  const [saving, setSaving] = useState(false);
  const current = agent.persona_sandbox ?? "";

  // Silent-hidden when persona is not configured server-side AND the agent has
  // no value set. An agent that *does* have a stale sandbox value still needs
  // to show it so the operator can clear it.
  if (!sandboxesLoading && sandboxes.length === 0 && current === "") {
    return (
      <div className="space-y-3">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.sandbox.intro)}
        </p>
        <div className="rounded-md border border-dashed bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          {t(($) => $.tab_body.sandbox.persona_not_configured)}
        </div>
      </div>
    );
  }

  const handleChange = async (value: string) => {
    if (value === current) return;
    setSaving(true);
    try {
      await onSave({ persona_sandbox: value });
      toast.success(t(($) => $.tab_body.sandbox.saved_toast));
    } catch {
      toast.error(t(($) => $.tab_body.sandbox.save_failed_toast));
    } finally {
      setSaving(false);
    }
  };

  const disabled = !isAdmin || saving || sandboxesLoading;
  const selected = sandboxes.find((sb) => sb.name === current) ?? null;
  const adminOnlyHint = t(($) => $.tab_body.sandbox.admin_only_tooltip);

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.sandbox.intro)}
      </p>

      <div className="space-y-2">
        <label
          htmlFor="persona-sandbox"
          className="block text-xs font-medium"
        >
          {t(($) => $.tab_body.sandbox.label)}
        </label>
        <div className="flex items-center gap-2">
          <NativeSelect
            id="persona-sandbox"
            aria-label={t(($) => $.tab_body.sandbox.label)}
            value={current}
            onChange={(e) => handleChange(e.target.value)}
            disabled={disabled}
            size="sm"
            className="w-full max-w-sm"
            title={!isAdmin ? adminOnlyHint : undefined}
          >
            <NativeSelectOption value="">
              {t(($) => $.tab_body.sandbox.no_gating_option)}
            </NativeSelectOption>
            {sandboxes.map((sb) => (
              <NativeSelectOption key={sb.name} value={sb.name}>
                {sb.name}
              </NativeSelectOption>
            ))}
            {/* Keep the current value selectable even if the server-side list
                no longer returns it (e.g. sandbox was renamed in persona). */}
            {current !== "" && !sandboxes.some((sb) => sb.name === current) && (
              <NativeSelectOption key={current} value={current}>
                {current}
              </NativeSelectOption>
            )}
          </NativeSelect>
          {saving && (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          )}
          {!isAdmin && (
            <Lock
              className="h-3.5 w-3.5 text-muted-foreground"
              aria-label={adminOnlyHint}
            />
          )}
        </div>
        {selected?.description && (
          <p className="text-[11px] text-muted-foreground">
            {selected.description}
          </p>
        )}
      </div>
    </div>
  );
}
