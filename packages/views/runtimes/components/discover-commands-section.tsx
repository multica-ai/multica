import { useState } from "react";
import { Loader2, Search, CheckCircle2, XCircle } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { toast } from "sonner";

type SlashCommand = { command: string; label: string; prompt: string };

function extractCommandsJSON(text: string): SlashCommand[] | null {
  // Try direct parse
  try {
    const parsed = JSON.parse(text.trim());
    if (Array.isArray(parsed)) return parsed;
  } catch {}

  // Try all code fence blocks — pick the largest valid JSON array
  const fenceRegex = /```(?:json)?\s*([\s\S]*?)```/g;
  let best: SlashCommand[] | null = null;
  let match: RegExpExecArray | null;
  while ((match = fenceRegex.exec(text)) !== null) {
    try {
      const parsed = JSON.parse(match[1]!);
      if (Array.isArray(parsed) && (!best || parsed.length > best.length)) {
        best = parsed;
      }
    } catch {}
  }
  if (best) return best;

  // Try all [ ... ] blocks — for each [, try each ] rightward until parse succeeds
  let searchStart = 0;
  while (searchStart < text.length) {
    const start = text.indexOf("[", searchStart);
    if (start < 0) break;
    let endPos = start;
    while (endPos < text.length) {
      endPos = text.indexOf("]", endPos + 1);
      if (endPos < 0) break;
      try {
        const parsed = JSON.parse(text.slice(start, endPos + 1));
        if (Array.isArray(parsed) && (!best || parsed.length > best.length)) {
          best = parsed;
        }
      } catch {}
    }
    searchStart = start + 1;
  }
  return best;
}

export function DiscoverCommandsSection({
  runtimeId,
}: {
  runtimeId: string;
}) {
  const [discovering, setDiscovering] = useState(false);
  const [result, setResult] = useState<{
    status: "success" | "error";
    message: string;
  } | null>(null);

  const handleDiscover = async () => {
    setDiscovering(true);
    setResult(null);

    let sessionId: string | null = null;
    let taskId: string | null = null;

    try {
      // 1. Find an agent using this runtime
      const agents = await api.listAgents();
      const agent = agents.find(
        (a) => a.runtime_id === runtimeId && !a.archived_at,
      );
      if (!agent) {
        setResult({ status: "error", message: "No active agent uses this runtime" });
        toast.error("No active agent uses this runtime");
        return;
      }

      // 2. Create temp session
      const session = await api.createChatSession({
        agent_id: agent.id,
        title: "__discover_commands__",
      });
      sessionId = session.id;

      // 3. Send discover prompt
      const prompt =
        "IMPORTANT: Your response must contain ONLY a valid JSON array. No text before or after it. No markdown code fences. No explanations. Just the raw JSON starting with [ and ending with ].\n\nList all available slash commands as a JSON array:\n[{\"command\":\"/name\",\"label\":\"Name\",\"prompt\":\"What this command does\"}]";
      const result = await api.sendChatMessage(session.id, prompt);
      taskId = result.task_id;

      // 4. Poll for assistant reply
      let attempts = 0;
      let completed = false;
      while (attempts < 30 && !completed) {
        await new Promise((r) => setTimeout(r, 2000));
        const messages = await api.listChatMessages(session.id);
        const assistantMsg = messages.find((m) => m.role === "assistant");
        if (assistantMsg) {
          completed = true;

          // 5. Parse JSON
          const commands = extractCommandsJSON(assistantMsg.content);
          if (commands && commands.length > 0) {
            // 6. Update all agents using this runtime
            const allAgents = agents.filter(
              (a) => a.runtime_id === runtimeId,
            );
            for (const a of allAgents) {
              const config = {
                ...((a.runtime_config as Record<string, unknown>) ?? {}),
                slash_commands: commands,
              };
              await api.updateAgent(a.id, { runtime_config: config });
            }
            setResult({
              status: "success",
              message: `Discovered ${commands.length} commands`,
            });
            toast.success(
              `Discovered ${commands.length} commands for this runtime`,
            );
          } else {
            setResult({
              status: "error",
              message: "Failed to parse commands from agent response",
            });
            toast.error("Failed to parse commands from agent response");
          }
        }
        attempts++;
      }

      if (!completed) {
        // Cancel the running task on timeout
        if (taskId) {
          try { await api.cancelTaskById(taskId); } catch {}
        }
        setResult({ status: "error", message: "Discovery timed out" });
        toast.error("Discovery timed out");
      }
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : "Failed to discover commands";
      setResult({ status: "error", message: msg });
      toast.error(msg);
    } finally {
      // Always archive temp session
      if (sessionId) {
        try { await api.archiveChatSession(sessionId); } catch {}
      }
      setDiscovering(false);
    }
  };

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="xs"
          onClick={handleDiscover}
          disabled={discovering}
        >
          {discovering ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Search className="h-3 w-3" />
          )}
          {discovering ? "Discovering..." : "Discover Commands"}
        </Button>

        {result && (
          <span
            className={`inline-flex items-center gap-1 text-xs ${
              result.status === "success"
                ? "text-success"
                : "text-destructive"
            }`}
          >
            {result.status === "success" ? (
              <CheckCircle2 className="h-3 w-3" />
            ) : (
              <XCircle className="h-3 w-3" />
            )}
            {result.message}
          </span>
        )}
      </div>
    </div>
  );
}
