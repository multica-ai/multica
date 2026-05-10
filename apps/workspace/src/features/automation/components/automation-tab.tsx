"use client";

import { useState } from "react";
import { Zap, Play, Clock } from "lucide-react";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { useAutomationTemplates } from "../queries";
import { useAutomationMutations } from "../mutations";
import type { AutomationTemplate, StandupSummaryResult } from "@/shared/types";

/** Renders the toggle + optional "Run Now" for a single automation template. */
function TemplateCard({
  template,
  onToggle,
  onRun,
  isTogglingId,
  isRunningId,
}: {
  template: AutomationTemplate;
  onToggle: (id: string, enabled: boolean) => void;
  onRun: (id: string) => void;
  isTogglingId: string | null;
  isRunningId: string | null;
}) {
  const isToggling = isTogglingId === template.id;
  const isRunning = isRunningId === template.id;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-start justify-between gap-4">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-xl shrink-0">{template.icon}</span>
            <div className="min-w-0">
              <CardTitle className="text-sm font-medium">{template.name}</CardTitle>
              <CardDescription className="mt-0.5 text-xs">{template.description}</CardDescription>
            </div>
          </div>
          <Switch
            checked={template.enabled}
            disabled={isToggling}
            onCheckedChange={(checked) => onToggle(template.id, checked)}
          />
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="flex items-center gap-2 flex-wrap">
          {template.trigger_type === "scheduled" ? (
            <Badge variant="secondary" className="gap-1 text-xs font-normal">
              <Clock className="h-3 w-3" />
              {template.schedule ?? "Scheduled"}
            </Badge>
          ) : (
            <Badge variant="secondary" className="gap-1 text-xs font-normal">
              <Play className="h-3 w-3" />
              Manual trigger
            </Badge>
          )}
          {template.trigger_type === "manual" && (
            <Button
              size="sm"
              variant="outline"
              className="h-7 text-xs"
              disabled={isRunning}
              onClick={() => onRun(template.id)}
            >
              {isRunning ? "Running…" : "Run Now"}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

/** Displays the standup summary result after a manual run. */
function StandupResult({ result }: { result: StandupSummaryResult }) {
  return (
    <Card className="border-dashed">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium">
          Standup Summary — {result.date}
        </CardTitle>
        <CardDescription className="text-xs">
          {result.member_count} member{result.member_count !== 1 ? "s" : ""}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <pre className="whitespace-pre-wrap text-xs text-muted-foreground leading-relaxed font-sans">
          {result.content}
        </pre>
      </CardContent>
    </Card>
  );
}

/** Automation tab content for the Settings page. */
export function AutomationTab() {
  const { data: templates = [], isLoading } = useAutomationTemplates();
  const { enableAutomation, disableAutomation, runAutomation, runResult } = useAutomationMutations();

  const [togglingId, setTogglingId] = useState<string | null>(null);
  const [runningId, setRunningId] = useState<string | null>(null);

  async function handleToggle(templateId: string, enabled: boolean) {
    setTogglingId(templateId);
    try {
      if (enabled) {
        await enableAutomation(templateId);
        toast.success("Automation enabled");
      } else {
        await disableAutomation(templateId);
        toast.success("Automation disabled");
      }
    } catch {
      toast.error("Failed to update automation");
    } finally {
      setTogglingId(null);
    }
  }

  async function handleRun(templateId: string) {
    setRunningId(templateId);
    try {
      await runAutomation(templateId);
      toast.success("Automation ran successfully");
    } catch {
      toast.error("Failed to run automation");
    } finally {
      setRunningId(null);
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12 text-muted-foreground text-sm">
        Loading automation templates…
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <div className="flex items-center gap-2 mb-1">
          <Zap className="h-4 w-4" />
          <h2 className="text-base font-semibold">Automation</h2>
        </div>
        <p className="text-sm text-muted-foreground">
          Enable built-in automations to keep your team informed and on track. Scheduled automations
          run automatically; manual automations can be triggered on demand.
        </p>
      </div>

      {templates.length === 0 ? (
        <p className="text-sm text-muted-foreground py-4">No automation templates available.</p>
      ) : (
        <div className="space-y-3">
          {templates.map((template) => (
            <TemplateCard
              key={template.id}
              template={template}
              onToggle={handleToggle}
              onRun={handleRun}
              isTogglingId={togglingId}
              isRunningId={runningId}
            />
          ))}
        </div>
      )}

      {runResult && <StandupResult result={runResult} />}
    </div>
  );
}
