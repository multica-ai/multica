"use client";

import { useEffect, useState } from "react";
import { Bot, Save, Plus, Trash2 } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { useWorkspaceStore } from "@/features/workspace";
import { useAISettingsQuery, useAISettingsMutations } from "@/features/settings/mutations";

export function AITab() {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const workspaceId = workspace?.id ?? null;

  const { data: settings, isLoading } = useAISettingsQuery(workspaceId);
  const { updateAISettings, updating } = useAISettingsMutations(workspaceId);

  const [provider, setProvider] = useState("deepseek");
  const [apiKey, setApiKey] = useState("");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [labelRules, setLabelRules] = useState<string[]>([]);
  const [newRule, setNewRule] = useState("");

  useEffect(() => {
    if (settings) {
      setProvider(settings.provider ?? "deepseek");
      setModel(settings.model ?? "");
      setBaseUrl(settings.base_url ?? "");
      setLabelRules(settings.label_rules ?? []);
    }
  }, [settings]);

  async function handleSave() {
    if (!workspaceId) return;
    try {
      await updateAISettings({
        provider,
        api_key: apiKey || undefined,
        model: model || undefined,
        base_url: baseUrl || undefined,
        label_rules: labelRules,
      });
      setApiKey("");
      toast.success("AI settings saved");
    } catch {
      toast.error("Failed to save AI settings");
    }
  }

  function addRule() {
    const trimmed = newRule.trim();
    if (!trimmed) return;
    setLabelRules((prev) => [...prev, trimmed]);
    setNewRule("");
  }

  function removeRule(idx: number) {
    setLabelRules((prev) => prev.filter((_, i) => i !== idx));
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12 text-muted-foreground text-sm">
        Loading AI settings…
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-base">
            <Bot className="h-4 w-4" />
            AI Provider
          </CardTitle>
          <CardDescription>
            Configure the AI provider used for label suggestions and schedule planning.
            If no API key is set here, the server-side environment variable is used.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid gap-2">
            <Label htmlFor="ai-provider">Provider</Label>
            <Input
              id="ai-provider"
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
              placeholder="deepseek"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ai-apikey">
              API Key
              {(settings?.has_api_key || settings?.api_key_masked) && (
                <Badge variant="secondary" className="ml-2 text-xs font-normal">
                  {settings.masked_api_key ?? settings.api_key_masked ?? "configured"}
                </Badge>
              )}
            </Label>
            <Input
              id="ai-apikey"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={(settings?.has_api_key || settings?.api_key_masked) ? "Leave blank to keep existing" : "sk-…"}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ai-model">Model</Label>
            <Input
              id="ai-model"
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="deepseek-chat"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="ai-baseurl">Base URL</Label>
            <Input
              id="ai-baseurl"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder="https://api.deepseek.com"
            />
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Label Rules</CardTitle>
          <CardDescription>
            Natural language rules for AI label suggestions. Describe when each label
            should be applied (e.g. "Apply 'bug' when the issue describes a malfunction").
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          {labelRules.length === 0 && (
            <p className="text-sm text-muted-foreground">
              No rules yet. Add rules to guide AI label suggestions.
            </p>
          )}
          {labelRules.map((rule, idx) => (
            <div key={idx} className="flex items-start gap-2">
              <Textarea
                value={rule}
                onChange={(e) => {
                  const updated = [...labelRules];
                  updated[idx] = e.target.value;
                  setLabelRules(updated);
                }}
                className="min-h-[60px] resize-none text-sm"
              />
              <Button
                variant="ghost"
                size="icon"
                className="mt-1 shrink-0 text-muted-foreground hover:text-destructive"
                onClick={() => removeRule(idx)}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>
          ))}
          <div className="flex gap-2">
            <Input
              value={newRule}
              onChange={(e) => setNewRule(e.target.value)}
              placeholder="Add a label rule…"
              onKeyDown={(e) => e.key === "Enter" && addRule()}
            />
            <Button variant="outline" onClick={addRule}>
              <Plus className="h-4 w-4" />
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={updating}>
          <Save className="mr-2 h-4 w-4" />
          {updating ? "Saving…" : "Save AI Settings"}
        </Button>
      </div>
    </div>
  );
}
