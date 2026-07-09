"use client";

import { useEffect, useMemo, useState } from "react";
import { AlertCircle, Loader2, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

export function AgentDraftDialog({
  agents,
  runtimesById,
  onClose,
}: {
  agents: Agent[];
  runtimesById: Map<string, AgentRuntime>;
  onClose: () => void;
}) {
  const { t } = useT("agents");
  const eligibleAgents = useMemo(
    () => agents.filter((agent) => runtimesById.get(agent.runtime_id)?.status === "online"),
    [agents, runtimesById],
  );
  const [hostAgentId, setHostAgentId] = useState(eligibleAgents[0]?.id ?? "");
  const [prompt, setPrompt] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!hostAgentId && eligibleAgents[0]) setHostAgentId(eligibleAgents[0].id);
  }, [hostAgentId, eligibleAgents]);

  const selectedAgent = eligibleAgents.find((agent) => agent.id === hostAgentId) ?? null;
  const canSubmit = !!selectedAgent && prompt.trim().length > 0 && !loading;

  const submit = async () => {
    if (!canSubmit) return;
    setLoading(true);
    setError("");
    try {
      await api.draftAgentWithAI({
        host_agent_id: selectedAgent.id,
        prompt: prompt.trim(),
      });
      toast.success(t(($) => $.ai_draft.toast_queued));
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t(($) => $.ai_draft.fallback_error));
      setLoading(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[82vh] w-[min(560px,calc(100vw-2rem))] flex-col overflow-hidden p-0">
        <div className="flex items-center gap-3 border-b px-5 py-4">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-brand/10 text-brand">
            <Sparkles className="h-4 w-4" />
          </div>
          <div>
            <DialogTitle className="text-sm font-semibold">
              {t(($) => $.ai_draft.title)}
            </DialogTitle>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {t(($) => $.ai_draft.subtitle)}
            </p>
          </div>
        </div>

        <div className="space-y-4 overflow-y-auto px-5 py-4">
          {eligibleAgents.length === 0 ? (
            <div className="flex items-start gap-2 rounded-md bg-warning/10 px-3 py-2 text-xs text-muted-foreground">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
              <span>{t(($) => $.ai_draft.no_agents)}</span>
            </div>
          ) : (
            <div className="space-y-1.5">
              <Label className="text-xs text-muted-foreground">
                {t(($) => $.ai_draft.host_agent_label)}
              </Label>
              <Select value={hostAgentId} onValueChange={(v) => setHostAgentId(v ?? "")}>
                <SelectTrigger className="w-full">
                  <SelectValue>
                    {() => selectedAgent?.name ?? t(($) => $.ai_draft.host_agent_placeholder)}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent align="start" alignItemWithTrigger={false} className="max-h-72">
                  {eligibleAgents.map((agent) => {
                    const runtime = runtimesById.get(agent.runtime_id);
                    return (
                      <SelectItem key={agent.id} value={agent.id}>
                        <span className="truncate">{agent.name}</span>
                        {runtime?.provider && (
                          <span className="text-xs text-muted-foreground">{runtime.provider}</span>
                        )}
                      </SelectItem>
                    );
                  })}
                </SelectContent>
              </Select>
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="agent-draft-prompt" className="text-xs text-muted-foreground">
              {t(($) => $.ai_draft.prompt_label)}
            </Label>
            <Textarea
              id="agent-draft-prompt"
              value={prompt}
              onChange={(e) => {
                setPrompt(e.target.value);
                setError("");
              }}
              placeholder={t(($) => $.ai_draft.prompt_placeholder)}
              rows={5}
              className="resize-none"
              autoFocus
            />
          </div>

          {error && (
            <div role="alert" className="flex items-start gap-2 rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span>{error}</span>
            </div>
          )}
        </div>

        <div className="flex shrink-0 items-center justify-end gap-2 border-t bg-muted/30 px-5 py-3">
          <Button type="button" variant="ghost" size="sm" onClick={onClose} disabled={loading}>
            {t(($) => $.ai_draft.cancel)}
          </Button>
          <Button type="button" size="sm" onClick={submit} disabled={!canSubmit}>
            {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
            {t(($) => $.ai_draft.submit)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
