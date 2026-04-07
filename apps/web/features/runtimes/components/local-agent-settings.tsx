"use client";

import { useState, useEffect, useCallback } from "react";
import { RefreshCw, Check, X, Loader2, Terminal, Settings } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/shared/api";
import type { LocalDetectedAgent } from "@/shared/types";

export function LocalAgentSettings() {
  const [agents, setAgents] = useState<LocalDetectedAgent[]>([]);
  const [loading, setLoading] = useState(true);
  const [detecting, setDetecting] = useState(false);
  const [editingPath, setEditingPath] = useState<string | null>(null);
  const [pathInput, setPathInput] = useState("");

  const fetchAgents = useCallback(async () => {
    try {
      const result = await api.listLocalAgents();
      setAgents(result.agents);
    } catch {
      // First time — no agents detected yet.
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  const handleDetect = async () => {
    setDetecting(true);
    try {
      const result = await api.detectLocalAgents();
      setAgents(result.agents);
      toast.success("Agent detection complete");
    } catch {
      toast.error("Detection failed");
    } finally {
      setDetecting(false);
    }
  };

  const handleSetPath = async (provider: string) => {
    try {
      const updated = await api.setLocalAgentPath(provider, pathInput);
      setAgents((prev) =>
        prev.map((a) => (a.provider === provider ? updated : a))
      );
      setEditingPath(null);
      setPathInput("");
      toast.success(`Path updated for ${provider}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to set path");
    }
  };

  const handleHealthCheck = async () => {
    setDetecting(true);
    try {
      const result = await api.healthCheckLocalAgents();
      setAgents(result.agents);
      toast.success("Health check complete");
    } catch {
      toast.error("Health check failed");
    } finally {
      setDetecting(false);
    }
  };

  const providerLabels: Record<string, string> = {
    claude: "Claude Code",
    codex: "Codex",
    opencode: "OpenCode",
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Terminal className="h-5 w-5" />
              Local Agent Runtimes
            </CardTitle>
            <CardDescription>
              Detected agent CLI installations on this machine
            </CardDescription>
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleHealthCheck}
              disabled={detecting}
            >
              {detecting ? (
                <Loader2 className="h-4 w-4 animate-spin mr-1" />
              ) : (
                <RefreshCw className="h-4 w-4 mr-1" />
              )}
              Health Check
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={handleDetect}
              disabled={detecting}
            >
              {detecting ? (
                <Loader2 className="h-4 w-4 animate-spin mr-1" />
              ) : (
                <Settings className="h-4 w-4 mr-1" />
              )}
              Detect
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        {loading ? (
          <div className="flex justify-center py-4">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : agents.length === 0 ? (
          <div className="text-center py-4 text-sm text-muted-foreground">
            No agents detected. Click &quot;Detect&quot; to scan for installed agent CLIs.
          </div>
        ) : (
          agents.map((agent) => (
            <div
              key={agent.provider}
              className="flex items-center justify-between p-3 border rounded-lg"
            >
              <div className="flex items-center gap-3">
                <div>
                  <div className="font-medium text-sm">
                    {providerLabels[agent.provider] ?? agent.provider}
                  </div>
                  {agent.version && (
                    <div className="text-xs text-muted-foreground">
                      v{agent.version}
                    </div>
                  )}
                  {agent.path && (
                    <div className="text-xs text-muted-foreground font-mono">
                      {agent.path}
                      {agent.is_custom_path && (
                        <span className="ml-1 text-blue-500">(custom)</span>
                      )}
                    </div>
                  )}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge
                  variant={agent.status === "available" ? "default" : "destructive"}
                >
                  {agent.status === "available" ? (
                    <Check className="h-3 w-3 mr-1" />
                  ) : (
                    <X className="h-3 w-3 mr-1" />
                  )}
                  {agent.status}
                </Badge>
                {editingPath === agent.provider ? (
                  <div className="flex items-center gap-1">
                    <Input
                      className="h-7 w-48 text-xs"
                      placeholder="/path/to/cli"
                      value={pathInput}
                      onChange={(e) => setPathInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") handleSetPath(agent.provider);
                        if (e.key === "Escape") setEditingPath(null);
                      }}
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 w-7 p-0"
                      onClick={() => handleSetPath(agent.provider)}
                    >
                      <Check className="h-3 w-3" />
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 w-7 p-0"
                      onClick={() => setEditingPath(null)}
                    >
                      <X className="h-3 w-3" />
                    </Button>
                  </div>
                ) : (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-7 text-xs"
                    onClick={() => {
                      setEditingPath(agent.provider);
                      setPathInput(agent.path);
                    }}
                  >
                    Set Path
                  </Button>
                )}
              </div>
            </div>
          ))
        )}
        {agents.some((a) => a.error) && (
          <div className="text-xs text-destructive mt-2">
            {agents
              .filter((a) => a.error)
              .map((a) => (
                <div key={a.provider}>
                  {providerLabels[a.provider] ?? a.provider}: {a.error}
                </div>
              ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
