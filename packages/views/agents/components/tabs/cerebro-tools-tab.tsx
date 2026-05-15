"use client";

// CEREBRO-PATCH(agent-tools-tab): JEH-1290/1353/1359/1360 W8 — tools tab on agent editor

import { useState } from "react";
import { Wrench, AlertCircle, Loader2, Info } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent } from "@multica/core/types";
import type { AgentTool } from "@multica/cerebro-types";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { Switch } from "@multica/ui/components/ui/switch";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../../i18n";

function agentToolsQueryKey(agentId: string, wsId: string) {
  return ["cerebro", "agent-tools", wsId, agentId] as const;
}

async function fetchAgentTools(agentId: string, wsId: string): Promise<AgentTool[]> {
  return await api.getAgentTools(agentId, { workspace_id: wsId });
}

interface ToolConfigFieldsProps {
  tool: AgentTool;
  onConfigChange: (key: string, value: string) => void;
  disabled: boolean;
}

function ToolConfigFields({ tool, onConfigChange, disabled }: ToolConfigFieldsProps) {
  const config = tool.config ?? {};

  // Tool-specific config fields based on tool name
  if (tool.name === "firtal_bq_query") {
    const rowLimit = String(config.row_limit ?? "1000");
    return (
      <div className="mt-2 pl-10">
        <Label className="text-xs text-muted-foreground">Row limit</Label>
        <Input
          type="number"
          value={rowLimit}
          onChange={(e) => onConfigChange("row_limit", e.target.value)}
          disabled={disabled}
          className="mt-1 h-7 w-32 text-xs"
          min={1}
          max={10000}
        />
      </div>
    );
  }

  if (tool.name === "gogcli_sheets_write") {
    const spreadsheetId = String(config.spreadsheet_id ?? "");
    return (
      <div className="mt-2 pl-10">
        <Label className="text-xs text-muted-foreground">Default Spreadsheet ID</Label>
        <Input
          type="text"
          value={spreadsheetId}
          onChange={(e) => onConfigChange("spreadsheet_id", e.target.value)}
          disabled={disabled}
          className="mt-1 h-7 text-xs"
          placeholder="1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms"
        />
      </div>
    );
  }

  if (tool.name === "web_fetch") {
    const allowlist = String(config.url_allowlist ?? "");
    return (
      <div className="mt-2 pl-10">
        <Label className="text-xs text-muted-foreground">URL allowlist (comma-separated)</Label>
        <Input
          type="text"
          value={allowlist}
          onChange={(e) => onConfigChange("url_allowlist", e.target.value)}
          disabled={disabled}
          className="mt-1 h-7 text-xs"
          placeholder="https://docs.example.com, https://api.example.com"
        />
      </div>
    );
  }

  return null;
}

interface ToolRowProps {
  tool: AgentTool;
  onToggle: (name: string, enabled: boolean) => Promise<void>;
  onConfigSave: (name: string, config: Record<string, unknown>) => Promise<void>;
  toggling: boolean;
}

function ToolRow({ tool, onToggle, onConfigSave, toggling }: ToolRowProps) {
  const [localConfig, setLocalConfig] = useState<Record<string, unknown>>(tool.config ?? {});
  const [savingConfig, setSavingConfig] = useState(false);

  const isExcluded = tool.status === "explicitly_excluded"; // CEREBRO-PATCH(agent-tools-status): excluded tools are visible but cannot be toggled callable.
  const isDirty = JSON.stringify(localConfig) !== JSON.stringify(tool.config ?? {});

  const handleConfigChange = (key: string, value: string) => {
    setLocalConfig((prev) => ({ ...prev, [key]: value }));
  };

  const handleSaveConfig = async () => {
    setSavingConfig(true);
    try {
      await onConfigSave(tool.name, localConfig);
    } finally {
      setSavingConfig(false);
    }
  };

  return (
    <li className="rounded-md border bg-card p-3">
      <div className="flex items-start gap-3">
        <Wrench className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-sm font-medium">{tool.name}</span>
                {isExcluded && (
                  <span className="rounded border border-dashed px-1.5 py-0.5 text-[11px] text-muted-foreground">
                    Excluded
                  </span>
                )}
              </div>
              {tool.description && (
                <p className="mt-0.5 text-xs text-muted-foreground">{tool.description}</p>
              )}
            </div>
            <Switch
              checked={tool.enabled}
              onCheckedChange={(v) => onToggle(tool.name, v)}
              disabled={toggling || isExcluded}
              aria-label={`Toggle ${tool.name}`}
            />
          </div>
          {tool.enabled && (
            <ToolConfigFields
              tool={{ ...tool, config: localConfig }}
              onConfigChange={handleConfigChange}
              disabled={savingConfig}
            />
          )}
          {tool.enabled && isDirty && (
            <div className="mt-2 pl-10">
              <Button
                size="sm"
                variant="outline"
                onClick={handleSaveConfig}
                disabled={savingConfig}
                className="h-6 text-xs"
              >
                {savingConfig ? "Saving…" : "Save config"}
              </Button>
            </div>
          )}
        </div>
      </div>
    </li>
  );
}

export function CerebroToolsTab({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const [toggling, setToggling] = useState(false);
  const isLocalAgent = agent.runtime_mode === "local";

  const { data: tools = [], isLoading, isError } = useQuery({
    queryKey: agentToolsQueryKey(agent.id, wsId),
    queryFn: () => fetchAgentTools(agent.id, wsId),
    enabled: !isLocalAgent,
  });

  const handleToggle = async (toolName: string, enabled: boolean) => {
    setToggling(true);
    try {
      await api.updateAgentTool(agent.id, toolName, { enabled }, { workspace_id: wsId });
      qc.invalidateQueries({ queryKey: agentToolsQueryKey(agent.id, wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.tools.toggle_failed_toast));
    } finally {
      setToggling(false);
    }
  };

  const handleConfigSave = async (toolName: string, config: Record<string, unknown>) => {
    try {
      await api.updateAgentTool(agent.id, toolName, { enabled: true, config }, { workspace_id: wsId });
      qc.invalidateQueries({ queryKey: agentToolsQueryKey(agent.id, wsId) });
      toast.success(t(($) => $.tab_body.tools.config_saved_toast));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.tools.config_save_failed_toast));
    }
  };

  if (isLocalAgent) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.tools.intro)}
        </p>
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <Info className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.tools.local_empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.tools.local_agent_note)}
          </p>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
        <AlertCircle className="h-8 w-8 text-muted-foreground/40" />
        <p className="mt-3 text-sm text-muted-foreground">
          {t(($) => $.tab_body.tools.load_failed)}
        </p>
      </div>
    );
  }

  if (tools.length === 0) {
    return (
      <div className="space-y-4">
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.tools.intro)}
        </p>
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-12">
          <Wrench className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.tools.empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.tools.empty_hint)}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-xs text-muted-foreground">
        {t(($) => $.tab_body.tools.intro)}
      </p>
      <ul className="space-y-2">
        {tools.map((tool) => (
          <ToolRow
            key={tool.name}
            tool={tool}
            onToggle={handleToggle}
            onConfigSave={handleConfigSave}
            toggling={toggling}
          />
        ))}
      </ul>
    </div>
  );
}
