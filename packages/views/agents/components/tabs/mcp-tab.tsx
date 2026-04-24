"use client";

import { useEffect, useState } from "react";
import {
  Loader2,
  Save,
  Eraser,
  WandSparkles,
  CopyPlus,
  Lock,
  Link2,
} from "lucide-react";
import type { Agent } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@multica/ui/components/ui/card";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { toast } from "sonner";

const STARTER_CONFIG = {
  mcpServers: {
    "mcp-skills": {
      command: "npx",
      args: ["-y", "@koderspa/mcp-skills@latest"],
    },
  },
};

function formatMcpConfig(value: Agent["mcp_config"] | null | undefined): string {
  if (!value || (typeof value === "object" && Object.keys(value).length === 0)) {
    return "";
  }
  return JSON.stringify(value, null, 2);
}

function parseMcpConfig(raw: string): Record<string, unknown> | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;

  const parsed: unknown = JSON.parse(trimmed);
  if (parsed === null) return null;
  if (Array.isArray(parsed) || typeof parsed !== "object") {
    throw new Error("MCP config must be a JSON object");
  }
  return parsed as Record<string, unknown>;
}

export function McpTab({
  agent,
  readOnly = false,
  onSave,
}: {
  agent: Agent;
  readOnly?: boolean;
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const originalValue = formatMcpConfig(agent.mcp_config);
  const [value, setValue] = useState(originalValue);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValue(originalValue);
  }, [originalValue]);

  const dirty = value.trim() !== originalValue.trim();

  const handleAddStarter = () => {
    setValue(JSON.stringify(STARTER_CONFIG, null, 2));
  };

  const handleFormat = () => {
    try {
      const parsed = parseMcpConfig(value);
      setValue(parsed ? JSON.stringify(parsed, null, 2) : "");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Invalid MCP config JSON");
    }
  };

  const handleClear = async () => {
    setSaving(true);
    try {
      await onSave({ mcp_config: null });
      setValue("");
      toast.success("MCP configuration cleared");
    } catch {
      toast.error("Failed to clear MCP configuration");
    } finally {
      setSaving(false);
    }
  };

  const handleSave = async () => {
    let parsed: Record<string, unknown> | null;
    try {
      parsed = parseMcpConfig(value);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Invalid MCP config JSON");
      return;
    }

    setSaving(true);
    try {
      await onSave({ mcp_config: parsed });
      setValue(parsed ? JSON.stringify(parsed, null, 2) : "");
      toast.success(parsed ? "MCP configuration saved" : "MCP configuration cleared");
    } catch {
      toast.error("Failed to save MCP configuration");
    } finally {
      setSaving(false);
    }
  };

  if (readOnly) {
    return (
      <div className="max-w-3xl space-y-4">
        <div>
          <Label className="text-xs text-muted-foreground">MCP Configuration</Label>
          <p className="mt-0.5 text-xs text-muted-foreground">
            MCP server configuration may contain secrets. Only the agent owner or a workspace admin can view and edit it.
          </p>
        </div>
        <Alert>
          <Lock className="h-4 w-4" />
          <AlertTitle>Configuration hidden</AlertTitle>
          <AlertDescription>
            This agent has an MCP configuration, but its contents are redacted for your role.
          </AlertDescription>
        </Alert>
      </div>
    );
  }

  return (
    <div className="max-w-3xl space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <Label className="text-xs text-muted-foreground">MCP Configuration</Label>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Configure Model Context Protocol servers for this agent. The JSON is passed through to runtimes that support explicit MCP config injection.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={handleAddStarter}>
            <CopyPlus className="h-3 w-3" />
            Starter
          </Button>
          <Button type="button" variant="outline" size="sm" className="h-7 gap-1 text-xs" onClick={handleFormat}>
            <WandSparkles className="h-3 w-3" />
            Format
          </Button>
        </div>
      </div>

      <Card size="sm">
        <CardHeader className="border-b">
          <CardTitle className="flex items-center gap-2 text-sm">
            <Link2 className="h-4 w-4" />
            MCP JSON
          </CardTitle>
          <CardDescription>
            Example:
            <code className="ml-1 rounded bg-muted px-1.5 py-0.5 text-[11px]">
              {`{"mcpServers":{"mcp-skills":{"command":"npx","args":["-y","@koderspa/mcp-skills@latest"]}}}`}
            </code>
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 pt-4">
          <Textarea
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={JSON.stringify(STARTER_CONFIG, null, 2)}
            className="min-h-72 font-mono text-xs"
            spellCheck={false}
          />
          <p className="text-xs text-muted-foreground">
            Leave blank to remove MCP config entirely. The config is stored on the agent and reused for future task runs.
          </p>
        </CardContent>
      </Card>

      <div className="flex items-center gap-2">
        <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
          {saving ? (
            <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
          ) : (
            <Save className="mr-1.5 h-3.5 w-3.5" />
          )}
          Save
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleClear}
          disabled={saving || (!agent.mcp_config && !value.trim())}
        >
          <Eraser className="mr-1.5 h-3.5 w-3.5" />
          Clear
        </Button>
      </div>
    </div>
  );
}
