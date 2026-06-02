"use client";

import { useCallback, useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Globe, Minus, Plus, Terminal } from "lucide-react";
import type { McpServerEntry, McpServers } from "@multica/core/agents/mcp-validate";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Separator } from "@multica/ui/components/ui/separator";
import { useT } from "../../i18n";

// ── Types ────────────────────────────────────────────────────────────────────

export interface McpServerEditorProps {
  /** Current config value (null = no config). */
  value: McpServers | null;
  /** Called when the user modifies the config. Receives null to clear. */
  onChange: (servers: McpServers | null) => void;
}

interface ServerRow {
  key: string;
  entry: McpServerEntry;
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function emptyStdio(): McpServerEntry {
  return { command: "", args: [] };
}

function emptyHttp(): McpServerEntry {
  return { type: "sse", url: "", headers: {} };
}

/** Make an object key that doesn't collide with existing keys. */
function uniqueKey(existing: Set<string>, base: string): string {
  let candidate = base;
  let n = 1;
  while (existing.has(candidate)) {
    n++;
    candidate = `${base}-${n}`;
  }
  return candidate;
}

// ── ServerCard ───────────────────────────────────────────────────────────────

function ServerCard({
  name,
  entry,
  onNameChange,
  onEntryChange,
  onRemove,
  existingNames,
}: {
  name: string;
  entry: McpServerEntry;
  onNameChange: (newName: string) => void;
  onEntryChange: (entry: McpServerEntry) => void;
  onRemove: () => void;
  existingNames: Set<string>;
}) {
  const { t } = useT("agents");
  const [expanded, setExpanded] = useState(true);

  const isStdio = "command" in entry;
  const transportLabel = isStdio ? "stdio" : (entry.type ?? "sse");

  return (
    <div className="rounded-md border">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex items-center text-muted-foreground hover:text-foreground"
        >
          {expanded ? (
            <ChevronDown className="h-3.5 w-3.5" />
          ) : (
            <ChevronRight className="h-3.5 w-3.5" />
          )}
        </button>

        {isStdio ? (
          <Terminal className="h-3.5 w-3.5 text-muted-foreground" />
        ) : (
          <Globe className="h-3.5 w-3.5 text-muted-foreground" />
        )}

        <Input
          value={name}
          onChange={(e) => onNameChange(e.target.value)}
          className="h-7 max-w-[180px] text-sm font-medium"
          placeholder={t(($) => $.tab_body.mcp_config.server_name_placeholder)}
          aria-label={t(($) => $.tab_body.mcp_config.server_name_aria)}
        />

        <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
          {transportLabel}
        </span>

        <div className="flex-1" />

        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onRemove}
          className="h-7 px-2 text-destructive hover:text-destructive"
        >
          <Minus className="h-3 w-3" />
        </Button>
      </div>

      {/* Body */}
      {expanded && (
        <div className="space-y-3 border-t px-3 pb-3 pt-2">
          {/* Transport selector */}
          <div className="flex items-center gap-2">
            <Label className="w-20 text-xs">
              {t(($) => $.tab_body.mcp_config.transport_label)}
            </Label>
            <Select
              value={isStdio ? "stdio" : "http"}
              onValueChange={(v) => {
                onEntryChange(v === "stdio" ? emptyStdio() : emptyHttp());
              }}
            >
              <SelectTrigger className="h-7 w-[140px] text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="stdio">stdio</SelectItem>
                <SelectItem value="http">HTTP / SSE</SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Stdio fields */}
          {isStdio && (
            <>
              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs">
                  {t(($) => $.tab_body.mcp_config.command_label)}
                </Label>
                <Input
                  value={entry.command ?? ""}
                  onChange={(e) =>
                    onEntryChange({ ...entry, command: e.target.value })
                  }
                  className="h-7 flex-1 font-mono text-xs"
                  placeholder="npx"
                />
              </div>

              <div className="flex items-start gap-2">
                <Label className="w-20 pt-1.5 text-xs">
                  {t(($) => $.tab_body.mcp_config.args_label)}
                </Label>
                <Input
                  value={(entry.args ?? []).join(" ")}
                  onChange={(e) =>
                    onEntryChange({
                      ...entry,
                      args: e.target.value
                        .split(/\s+/)
                        .filter(Boolean),
                    })
                  }
                  className="h-7 flex-1 font-mono text-xs"
                  placeholder="-y @modelcontextprotocol/server-filesystem /tmp"
                />
              </div>

              <EnvEditor
                label={t(($) => $.tab_body.mcp_config.env_label)}
                values={entry.env ?? {}}
                onChange={(env) => onEntryChange({ ...entry, env })}
              />
            </>
          )}

          {/* HTTP fields */}
          {!isStdio && (
            <>
              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs">
                  {t(($) => $.tab_body.mcp_config.http_type_label)}
                </Label>
                <Select
                  value={entry.type ?? "sse"}
                  onValueChange={(v) =>
                    onEntryChange({
                      ...entry,
                      type: v as "sse" | "streamable-http",
                    })
                  }
                >
                  <SelectTrigger className="h-7 w-[180px] text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="sse">SSE</SelectItem>
                    <SelectItem value="streamable-http">Streamable HTTP</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-2">
                <Label className="w-20 text-xs">
                  {t(($) => $.tab_body.mcp_config.url_label)}
                </Label>
                <Input
                  value={entry.url ?? ""}
                  onChange={(e) =>
                    onEntryChange({ ...entry, url: e.target.value })
                  }
                  className="h-7 flex-1 font-mono text-xs"
                  placeholder="https://example.com/mcp"
                />
              </div>

              <EnvEditor
                label={t(($) => $.tab_body.mcp_config.headers_label)}
                values={entry.headers ?? {}}
                onChange={(headers) => onEntryChange({ ...entry, headers })}
              />
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ── EnvEditor (reused for env + headers) ─────────────────────────────────────

function EnvEditor({
  label,
  values,
  onChange,
}: {
  label: string;
  values: Record<string, string>;
  onChange: (values: Record<string, string>) => void;
}) {
  const entries = useMemo(() => Object.entries(values), [values]);
  const { t } = useT("agents");

  const set = useCallback(
    (key: string, value: string) => {
      onChange({ ...values, [key]: value });
    },
    [values, onChange],
  );

  const remove = useCallback(
    (key: string) => {
      const next = { ...values };
      delete next[key];
      onChange(next);
    },
    [values, onChange],
  );

  return (
    <div className="flex items-start gap-2">
      <Label className="w-20 pt-1.5 text-xs">{label}</Label>
      <div className="flex-1 space-y-1">
        {entries.map(([k, v]) => (
          <div key={k} className="flex items-center gap-1">
            <Input
              value={k}
              className="h-6 w-[120px] font-mono text-xs"
              placeholder="KEY"
              readOnly
            />
            <Input
              value={v}
              onChange={(e) => set(k, e.target.value)}
              className="h-6 flex-1 font-mono text-xs"
              placeholder="value"
            />
            <button
              type="button"
              onClick={() => remove(k)}
              className="text-muted-foreground hover:text-destructive"
            >
              <Minus className="h-3 w-3" />
            </button>
          </div>
        ))}
        <AddKeyValueRow
          onAdd={(key) => onChange({ ...values, [key]: "" })}
          existingKeys={new Set(entries.map(([k]) => k))}
          placeholder={t(($) => $.tab_body.mcp_config.env_key_placeholder)}
        />
      </div>
    </div>
  );
}

function AddKeyValueRow({
  onAdd,
  existingKeys,
  placeholder,
}: {
  onAdd: (key: string) => void;
  existingKeys: Set<string>;
  placeholder: string;
}) {
  const [draft, setDraft] = useState("");

  const handleAdd = () => {
    const trimmed = draft.trim();
    if (!trimmed || existingKeys.has(trimmed)) return;
    onAdd(trimmed);
    setDraft("");
  };

  return (
    <div className="flex items-center gap-1">
      <Input
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            handleAdd();
          }
        }}
        className="h-6 w-[120px] font-mono text-xs"
        placeholder={placeholder}
      />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={handleAdd}
        disabled={!draft.trim() || existingKeys.has(draft.trim())}
        className="h-6 px-2 text-xs"
      >
        <Plus className="h-3 w-3" />
      </Button>
    </div>
  );
}

// ── McpServerEditor ─────────────────────────────────────────────────────────

export function McpServerEditor({ value, onChange }: McpServerEditorProps) {
  const { t } = useT("agents");

  const rows: ServerRow[] = useMemo(() => {
    if (!value) return [];
    return Object.entries(value).map(([key, entry]) => ({ key, entry }));
  }, [value]);

  const existingNames = useMemo(
    () => new Set(rows.map((r) => r.key)),
    [rows],
  );

  const handleAddStdio = useCallback(() => {
    const next: McpServers = { ...(value ?? {}) };
    const name = uniqueKey(new Set(Object.keys(next)), "server");
    next[name] = emptyStdio();
    onChange(next);
  }, [value, onChange]);

  const handleAddHttp = useCallback(() => {
    const next: McpServers = { ...(value ?? {}) };
    const name = uniqueKey(new Set(Object.keys(next)), "http-server");
    next[name] = emptyHttp();
    onChange(next);
  }, [value, onChange]);

  const handleNameChange = useCallback(
    (oldName: string, newName: string) => {
      if (!value || oldName === newName) return;
      if (newName.trim() === "" || newName in value) return;
      const next: McpServers = {};
      for (const [k, v] of Object.entries(value)) {
        next[k === oldName ? newName : k] = v;
      }
      onChange(next);
    },
    [value, onChange],
  );

  const handleEntryChange = useCallback(
    (name: string, entry: McpServerEntry) => {
      if (!value) return;
      onChange({ ...value, [name]: entry });
    },
    [value, onChange],
  );

  const handleRemove = useCallback(
    (name: string) => {
      if (!value) return;
      const next = { ...value };
      delete next[name];
      onChange(Object.keys(next).length === 0 ? null : next);
    },
    [value, onChange],
  );

  const handleClearAll = useCallback(() => onChange(null), [onChange]);

  return (
    <div className="space-y-2">
      {rows.length === 0 && (
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.mcp_config.no_servers)}
        </p>
      )}

      {rows.map((row) => (
        <ServerCard
          key={row.key}
          name={row.key}
          entry={row.entry}
          onNameChange={(n) => handleNameChange(row.key, n)}
          onEntryChange={(e) => handleEntryChange(row.key, e)}
          onRemove={() => handleRemove(row.key)}
          existingNames={existingNames}
        />
      ))}

      <Separator />

      <div className="flex items-center gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleAddStdio}
          className="h-7 text-xs"
        >
          <Plus className="mr-1 h-3 w-3" />
          {t(($) => $.tab_body.mcp_config.add_stdio_server)}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleAddHttp}
          className="h-7 text-xs"
        >
          <Plus className="mr-1 h-3 w-3" />
          {t(($) => $.tab_body.mcp_config.add_http_server)}
        </Button>
        {rows.length > 0 && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={handleClearAll}
            className="ml-auto h-7 text-xs text-destructive hover:text-destructive"
          >
            {t(($) => $.tab_body.mcp_config.clear_action)}
          </Button>
        )}
      </div>
    </div>
  );
}
