"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import {
  Cloud,
  Monitor,
  Loader2,
  Save,
  Globe,
  Lock,
  Camera,
  ChevronDown,
  FileText,
} from "lucide-react";
import type {
  Agent,
  AgentVisibility,
  AgentRuntimeConfig,
  RuntimeDevice,
} from "@multica/core/types";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { ActorAvatar } from "../../../common/actor-avatar";

export function SettingsTab({
  agent,
  runtimes,
  onSave,
}: {
  agent: Agent;
  runtimes: RuntimeDevice[];
  onSave: (updates: Partial<Agent>) => Promise<void>;
}) {
  const [name, setName] = useState(agent.name);
  const [description, setDescription] = useState(agent.description ?? "");
  const [visibility, setVisibility] = useState<AgentVisibility>(agent.visibility);
  const [maxTasks, setMaxTasks] = useState(agent.max_concurrent_tasks);
  const [selectedRuntimeId, setSelectedRuntimeId] = useState(agent.runtime_id);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const { upload, uploading } = useFileUpload(api);
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Config mode state
  const [configMode, setConfigMode] = useState<"global" | "project">(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.config_mode ?? "global";
  });
  const [claudeSettingsJson, setClaudeSettingsJson] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.claude_settings_json ?? "";
  });
  const [codexConfigToml, setCodexConfigToml] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.codex_config_toml ?? "";
  });
  const [opencodeConfigJson, setOpencodeConfigJson] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    return rc?.opencode_config_json ?? "";
  });
  const [envVarsStr, setEnvVarsStr] = useState(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    const ev = rc?.env_vars;
    if (!ev || Object.keys(ev).length === 0) return "";
    return Object.entries(ev)
      .map(([k, v]) => `${k}=${v}`)
      .join("\n");
  });

  // Reference dialog state
  const [claudeDocsOpen, setClaudeDocsOpen] = useState(false);
  const [codexDocsOpen, setCodexDocsOpen] = useState(false);
  const [opencodeDocsOpen, setOpencodeDocsOpen] = useState(false);

  // Textarea auto-resize
  const claudeConfigRef = useCallback((el: HTMLTextAreaElement | null) => {
    if (el) {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, []);
  const codexConfigRef = useCallback((el: HTMLTextAreaElement | null) => {
    if (el) {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, []);
  const opencodeConfigRef = useCallback((el: HTMLTextAreaElement | null) => {
    if (el) {
      el.style.height = "auto";
      el.style.height = el.scrollHeight + "px";
    }
  }, []);

  // Sync state when agent changes (e.g. after save)
  useEffect(() => {
    const rc = agent.runtime_config as AgentRuntimeConfig | undefined;
    setConfigMode(rc?.config_mode ?? "global");
    setClaudeSettingsJson(rc?.claude_settings_json ?? "");
    setCodexConfigToml(rc?.codex_config_toml ?? "");
    setOpencodeConfigJson(rc?.opencode_config_json ?? "");
    const ev = rc?.env_vars;
    setEnvVarsStr(
      !ev || Object.keys(ev).length === 0
        ? ""
        : Object.entries(ev)
            .map(([k, v]) => `${k}=${v}`)
            .join("\n"),
    );
  }, [agent]);

  const selectedRuntime = runtimes.find((d) => d.id === selectedRuntimeId) ?? null;
  const runtimeProvider = selectedRuntime?.provider ?? "";

  const handleAvatarUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    e.target.value = "";
    try {
      const result = await upload(file);
      if (!result) return;
      await onSave({ avatar_url: result.link });
      toast.success("Avatar updated");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to upload avatar");
    }
  };

  const parseEnvVars = (str: string): Record<string, string> | undefined => {
    if (!str.trim()) return undefined;
    const vars: Record<string, string> = {};
    for (const line of str.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed || trimmed.startsWith("#")) continue;
      const eqIdx = trimmed.indexOf("=");
      if (eqIdx === -1) continue;
      const key = trimmed.slice(0, eqIdx).trim();
      const value = trimmed.slice(eqIdx + 1).trim();
      if (key) vars[key] = value;
    }
    return Object.keys(vars).length > 0 ? vars : undefined;
  };

  const buildRuntimeConfig = (): Record<string, unknown> | undefined => {
    const rc: AgentRuntimeConfig = { config_mode: configMode };
    if (configMode === "project") {
      rc.env_vars = parseEnvVars(envVarsStr);
      if (runtimeProvider === "claude" && claudeSettingsJson.trim()) {
        rc.claude_settings_json = claudeSettingsJson;
      }
      if (runtimeProvider === "codex" && codexConfigToml.trim()) {
        rc.codex_config_toml = codexConfigToml;
      }
      if (runtimeProvider === "opencode" && opencodeConfigJson.trim()) {
        rc.opencode_config_json = opencodeConfigJson;
      }
    }
    return rc;
  };

  const dirty =
    name !== agent.name ||
    description !== (agent.description ?? "") ||
    visibility !== agent.visibility ||
    maxTasks !== agent.max_concurrent_tasks ||
    selectedRuntimeId !== agent.runtime_id ||
    JSON.stringify(buildRuntimeConfig()) !== JSON.stringify(agent.runtime_config ?? {});

  const handleSave = async () => {
    if (!name.trim()) {
      toast.error("Name is required");
      return;
    }
    setSaving(true);
    try {
      await onSave({
        name: name.trim(),
        description,
        visibility,
        max_concurrent_tasks: maxTasks,
        runtime_id: selectedRuntimeId,
        runtime_config: buildRuntimeConfig(),
      });
      toast.success("Settings saved");
    } catch {
      toast.error("Failed to save settings");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-lg space-y-6">
      <div>
        <Label className="text-xs text-muted-foreground">Avatar</Label>
        <div className="mt-1.5 flex items-center gap-4">
          <button
            type="button"
            className="group relative h-16 w-16 shrink-0 rounded-full bg-muted overflow-hidden focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploading}
          >
            <ActorAvatar actorType="agent" actorId={agent.id} size={64} className="rounded-none" />
            <div className="absolute inset-0 flex items-center justify-center bg-black/40 opacity-0 transition-opacity group-hover:opacity-100">
              {uploading ? (
                <Loader2 className="h-5 w-5 animate-spin text-white" />
              ) : (
                <Camera className="h-5 w-5 text-white" />
              )}
            </div>
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={handleAvatarUpload}
          />
          <div className="text-xs text-muted-foreground">
            Click to upload avatar
          </div>
        </div>
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Name</Label>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Description</Label>
        <Input
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What does this agent do?"
          className="mt-1"
        />
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Visibility</Label>
        <div className="mt-1.5 flex gap-2">
          <button
            type="button"
            onClick={() => setVisibility("workspace")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              visibility === "workspace"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="text-left">
              <div className="font-medium">Workspace</div>
              <div className="text-xs text-muted-foreground">All members can assign</div>
            </div>
          </button>
          <button
            type="button"
            onClick={() => setVisibility("private")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              visibility === "private"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
            <div className="text-left">
              <div className="font-medium">Private</div>
              <div className="text-xs text-muted-foreground">Only you can assign</div>
            </div>
          </button>
        </div>
      </div>

      <div>
        <Label className="text-xs text-muted-foreground">Max Concurrent Tasks</Label>
        <Input
          type="number"
          min={1}
          max={50}
          value={maxTasks}
          onChange={(e) => setMaxTasks(Number(e.target.value))}
          className="mt-1 w-24"
        />
      </div>

      {/* Config Mode */}
      <div>
        <Label className="text-xs text-muted-foreground">Config Mode</Label>
        <div className="mt-1.5 flex gap-2">
          <button
            type="button"
            onClick={() => setConfigMode("global")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              configMode === "global"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <div className="text-left">
              <div className="font-medium">Global</div>
              <div className="text-xs text-muted-foreground">Use default config</div>
            </div>
          </button>
          <button
            type="button"
            onClick={() => setConfigMode("project")}
            className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
              configMode === "project"
                ? "border-primary bg-primary/5"
                : "border-border hover:bg-muted"
            }`}
          >
            <div className="text-left">
              <div className="font-medium">Project</div>
              <div className="text-xs text-muted-foreground">Per-task config override</div>
            </div>
          </button>
        </div>
      </div>

      {/* Environment Variables (project mode) */}
      {configMode === "project" && (
        <div>
          <Label className="text-xs text-muted-foreground">Environment Variables</Label>
          <p className="text-xs text-muted-foreground mt-0.5">
            One per line, <code className="rounded bg-muted px-1 py-0.5 text-xs">KEY=VALUE</code> format. Lines starting with <code className="rounded bg-muted px-1 py-0.5 text-xs">#</code> are ignored.
          </p>
          <div className="mt-1.5">
            <Textarea
              value={envVarsStr}
              onChange={(e) => setEnvVarsStr(e.target.value)}
              placeholder={"# Environment variables\nMY_VAR=my_value\nANOTHER_VAR=another"}
              className="min-h-[80px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
              spellCheck={false}
            />
          </div>
        </div>
      )}

      {/* Claude settings.json (project mode) */}
      {runtimeProvider === "claude" && configMode === "project" && (
        <div>
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-xs text-muted-foreground">settings.json</Label>
              <p className="text-xs text-muted-foreground mt-0.5">
                Written to <code className="rounded bg-muted px-1 py-0.5 text-xs">~/.claude/settings.json</code> for each task.
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setClaudeDocsOpen(true)}
            >
              <FileText className="h-3.5 w-3.5 mr-1" />
              Config Reference
            </Button>
          </div>
          <div className="mt-2">
            <Textarea
              ref={claudeConfigRef}
              value={claudeSettingsJson}
              onChange={(e) => setClaudeSettingsJson(e.target.value)}
              placeholder={'{\n  "permissions": {\n    "allow": ["Bash(git log*)"],\n    "deny": ["Bash(rm -rf*)"]\n  },\n  "env": {\n    "CLAUDE_CODE_USE_BEDROCK": "1"\n  }\n}'}
              className="min-h-[200px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
              spellCheck={false}
            />
          </div>

          <Dialog open={claudeDocsOpen} onOpenChange={setClaudeDocsOpen}>
            <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
              <DialogHeader>
                <DialogTitle>settings.json Reference</DialogTitle>
                <DialogDescription>
                  Configuration for Claude Code. This file is written to the agent&apos;s settings for each task.
                </DialogDescription>
              </DialogHeader>
              <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
                <section>
                  <h4 className="font-medium mb-1.5">Example Configuration</h4>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`{
  "permissions": {
    "allow": [
      "Bash(git log*)",
      "Bash(git diff*)",
      "Read",
      "Write"
    ],
    "deny": [
      "Bash(rm -rf*)",
      "Bash(curl*|*)"
    ]
  },
  "env": {
    "CLAUDE_CODE_USE_BEDROCK": "1",
    "ANTHROPIC_MODEL": "claude-sonnet-4-5"
  }
}`}</pre>
                </section>
                <section>
                  <h4 className="font-medium mb-1.5">Permissions</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Control which tools the agent can use. Supports glob patterns.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"permissions": {
  "allow": ["Read", "Write", "Bash(git*)"],
  "deny": ["Bash(rm*)"]
}`}</pre>
                </section>
                <section>
                  <h4 className="font-medium mb-1.5">Environment Variables</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Set environment variables for the Claude Code process.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"env": {
  "CLAUDE_CODE_USE_BEDROCK": "1",
  "ANTHROPIC_MODEL": "claude-sonnet-4-5"
}`}</pre>
                </section>
                <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                  <strong className="text-foreground">Tip:</strong> Changes take effect on the next task execution. Config is written per-task to an isolated settings directory.
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* Codex config.toml (project mode) */}
      {runtimeProvider === "codex" && configMode === "project" && (
        <div>
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-xs text-muted-foreground">config.toml</Label>
              <p className="text-xs text-muted-foreground mt-0.5">
                Written to an isolated <code className="rounded bg-muted px-1 py-0.5 text-xs">CODEX_HOME</code> directory for each task.
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setCodexDocsOpen(true)}
            >
              <FileText className="h-3.5 w-3.5 mr-1" />
              Config Reference
            </Button>
          </div>
          <div className="mt-2">
            <Textarea
              ref={codexConfigRef}
              value={codexConfigToml}
              onChange={(e) => setCodexConfigToml(e.target.value)}
              placeholder={'# Codex configuration\nmodel = "o3"\nsandbox_mode = "workspace-write"\n\n[profiles.default]\nmodel = "o3"\nsandbox_mode = "workspace-write"'}
              className="min-h-[200px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
              spellCheck={false}
            />
          </div>

          <Dialog open={codexDocsOpen} onOpenChange={setCodexDocsOpen}>
            <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
              <DialogHeader>
                <DialogTitle>config.toml Reference</DialogTitle>
                <DialogDescription>
                  Configuration for Codex. This file is written to an isolated CODEX_HOME directory for each task.
                </DialogDescription>
              </DialogHeader>
              <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
                <section>
                  <h4 className="font-medium mb-1.5">Example Configuration</h4>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`# Default model
model = "o3"

# Sandbox mode: "workspace-write", "read-only", "full-access"
sandbox_mode = "workspace-write"

# Approval policy: "suggest", "auto-edit", "full-auto"
approval_policy = "suggest"

# Chat mode (interactive conversation)
# chat_mode = true

[profiles.production]
model = "o3"
sandbox_mode = "read-only"
approval_policy = "always"`}</pre>
                </section>
                <section>
                  <h4 className="font-medium mb-1.5">Model Configuration</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Specify which model Codex should use.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`model = "o3"               # Default model
sandbox_mode = "workspace-write"  # Sandbox mode
approval_policy = "suggest"  # Approval policy`}</pre>
                </section>
                <section>
                  <h4 className="font-medium mb-1.5">Profiles</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Define named configuration profiles for different scenarios.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`[profiles.production]
model = "o3"
sandbox_mode = "read-only"
approval_policy = "always"`}</pre>
                </section>
                <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                  <strong className="text-foreground">Tip:</strong> Changes take effect on the next task execution. Config is written per-task to an isolated CODEX_HOME directory.
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      )}

      {/* OpenCode opencode.json (project mode) */}
      {runtimeProvider === "opencode" && configMode === "project" && (
        <div>
          <div className="flex items-center justify-between">
            <div>
              <Label className="text-xs text-muted-foreground">opencode.json</Label>
              <p className="text-xs text-muted-foreground mt-0.5">
                Written to <code className="rounded bg-muted px-1 py-0.5 text-xs">opencode.json</code> in each task&apos;s workdir.
              </p>
            </div>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setOpencodeDocsOpen(true)}
            >
              <FileText className="h-3.5 w-3.5 mr-1" />
              Config Reference
            </Button>
          </div>
          <div className="mt-2">
            <Textarea
              ref={opencodeConfigRef}
              value={opencodeConfigJson}
              onChange={(e) => setOpencodeConfigJson(e.target.value)}
              placeholder={'{\n  "$schema": "https://opencode.ai/config.json",\n  "model": "provider/model-id",\n  "provider": {\n    "myprovider": {\n      "npm": "@ai-sdk/openai-compatible",\n      "name": "My Provider",\n      "options": {\n        "baseURL": "https://api.example.com/v1",\n        "apiKey": "sk-xxx"\n      },\n      "models": {\n        "model-id": {\n          "name": "Model Name"\n        }\n      }\n    }\n  }\n}'}
              className="min-h-[200px] font-mono text-xs resize-none overflow-hidden rounded-lg bg-muted/30 border-dashed"
              spellCheck={false}
            />
          </div>

          <Dialog open={opencodeDocsOpen} onOpenChange={setOpencodeDocsOpen}>
            <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
              <DialogHeader>
                <DialogTitle>opencode.json Reference</DialogTitle>
                <DialogDescription>
                  Configuration for OpenCode. This file is written to <code className="rounded bg-muted px-1 py-0.5 text-xs">opencode.json</code> in the task&apos;s workdir. Project config overrides global defaults.
                </DialogDescription>
              </DialogHeader>
              <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-sm">
                <section>
                  <h4 className="font-medium mb-1.5">Example Configuration</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    A complete example showing model, provider, and permission settings.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`{
  "$schema": "https://opencode.ai/config.json",
  "model": "anthropic/claude-sonnet-4-5",
  "small_model": "anthropic/claude-haiku-4-5",
  "provider": {
    "openai": {
      "options": {
        "baseURL": "http://localhost:8080/v1",
        "apiKey": "sk-xxx"
      }
    }
  },
  "permission": {
    "edit": {
      "src/generated/*": "deny"
    }
  }
}`}</pre>
                </section>

                <section>
                  <h4 className="font-medium mb-1.5">Model Configuration</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Specify which models OpenCode should use.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"model": "anthropic/claude-sonnet-4-5",       // Main model
"small_model": "anthropic/claude-haiku-4-5"  // Lightweight tasks`}</pre>
                </section>

                <section>
                  <h4 className="font-medium mb-1.5">Provider Configuration</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Configure providers with custom endpoints, timeouts, and API keys. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">npm</code> to register third-party AI SDK packages.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"provider": {
  "anthropic": {
    "options": {
      "timeout": 600000,
      "chunkTimeout": 30000
    }
  },
  "openai": {
    "options": {
      "baseURL": "http://localhost:8080/v1",
      "apiKey": "sk-xxx"
    }
  },
  "custom": {
    "npm": "@ai-sdk/openai-compatible",
    "name": "My Provider",
    "options": {
      "baseURL": "https://api.example.com/v1",
      "apiKey": "sk-xxx"
    },
    "models": {
      "my-model": {
        "name": "My Model"
      }
    }
  }
}`}</pre>
                </section>

                <section>
                  <h4 className="font-medium mb-1.5">Permissions</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Control which files can be edited. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">"deny"</code> to block specific paths.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"permission": {
  "edit": {
    "packages/opencode/migration/*": "deny",
    "src/generated/*": "deny"
  }
}`}</pre>
                </section>

                <section>
                  <h4 className="font-medium mb-1.5">Tools & MCP</h4>
                  <p className="text-xs text-muted-foreground mb-2">
                    Enable or disable specific tools, or add MCP servers.
                  </p>
                  <pre className="rounded-md border bg-muted/40 px-3 py-2 text-xs font-mono leading-relaxed text-muted-foreground overflow-x-auto">{`"tools": {
  "github-triage": false,
  "github-pr-search": false
},
"mcp": {}`}</pre>
                </section>

                <div className="rounded-md border border-border/50 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                  <strong className="text-foreground">Tip:</strong> Config is written per-task to an isolated workdir. Use <code className="rounded bg-muted px-1 py-0.5 text-xs">$schema</code> for editor auto-completion.
                </div>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      )}

      <div>
        <Label className="text-xs text-muted-foreground">Runtime</Label>
        <Popover open={runtimeOpen} onOpenChange={setRuntimeOpen}>
          <PopoverTrigger
            disabled={runtimes.length === 0}
            className="flex w-full items-center gap-3 rounded-lg border border-border bg-background px-3 py-2.5 mt-1.5 text-left text-sm transition-colors hover:bg-muted disabled:pointer-events-none disabled:opacity-50"
          >
            {selectedRuntime?.runtime_mode === "cloud" ? (
              <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
            ) : (
              <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
            )}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="truncate font-medium">
                  {selectedRuntime?.name ?? "No runtime available"}
                </span>
                {selectedRuntime?.runtime_mode === "cloud" && (
                  <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                    Cloud
                  </span>
                )}
              </div>
              <div className="truncate text-xs text-muted-foreground">
                {selectedRuntime?.device_info ?? "Select a runtime"}
              </div>
            </div>
            <ChevronDown className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${runtimeOpen ? "rotate-180" : ""}`} />
          </PopoverTrigger>
          <PopoverContent align="start" className="w-[var(--anchor-width)] p-1 max-h-60 overflow-y-auto">
            {runtimes.map((device) => (
              <button
                key={device.id}
                onClick={() => {
                  setSelectedRuntimeId(device.id);
                  setRuntimeOpen(false);
                }}
                className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left text-sm transition-colors ${
                  device.id === selectedRuntimeId ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                {device.runtime_mode === "cloud" ? (
                  <Cloud className="h-4 w-4 shrink-0 text-muted-foreground" />
                ) : (
                  <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{device.name}</span>
                    {device.runtime_mode === "cloud" && (
                      <span className="shrink-0 rounded bg-info/10 px-1.5 py-0.5 text-xs font-medium text-info">
                        Cloud
                      </span>
                    )}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">{device.device_info}</div>
                </div>
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    device.status === "online" ? "bg-success" : "bg-muted-foreground/40"
                  }`}
                />
              </button>
            ))}
          </PopoverContent>
        </Popover>
      </div>

      <Button onClick={handleSave} disabled={!dirty || saving} size="sm">
        {saving ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <Save className="h-3.5 w-3.5 mr-1.5" />}
        Save Changes
      </Button>
    </div>
  );
}
