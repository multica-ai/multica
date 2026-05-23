"use client";

// Cerebro-only avatar picker: wraps the upstream AvatarPicker (manual upload)
// with an optional AI generation trigger. Mounted from upstream zone via thin
// imports in agent-detail-inspector.tsx and create-agent-dialog.tsx. See
// docs/cerebro-patches.md (`agent-avatar-generate`).

import { useState } from "react";
import { Loader2, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useT } from "@multica/views/i18n";
import { api } from "@multica/core/api";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { cn } from "@multica/ui/lib/utils";
import { AvatarPicker } from "@multica/views/agents/components/avatar-picker";

interface CerebroAvatarPickerProps {
  value: string | null;
  onChange: (url: string | null) => void;
  agentName: string;
  size?: number;
}

/**
 * Extends AvatarPicker with an optional AI generation button.
 * When cerebro_agent_avatar is enabled and OPENROUTER_API_KEY is set on the
 * server, a "Generate AI" trigger appears below the avatar square.
 * Clicking it reveals an optional prompt input — leaving it blank triggers
 * auto-generation with a Scandinavian-appearance prompt and a clothing
 * colour seeded from the agent name.
 */
export function CerebroAvatarPicker({
  value,
  onChange,
  agentName,
  size = 56,
}: CerebroAvatarPickerProps) {
  const { t } = useT("cerebro-agent-avatar");
  const enabled = useFeatureFlag("cerebro_agent_avatar");
  const [generating, setGenerating] = useState(false);
  const [expanded, setExpanded] = useState(false);
  const [prompt, setPrompt] = useState("");

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const result = await api.generateAgentAvatar(
        agentName || "Agent",
        prompt || undefined,
      );
      onChange(result.url);
      setExpanded(false);
      setPrompt("");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.generate_error),
      );
    } finally {
      setGenerating(false);
    }
  };

  return (
    <div className="shrink-0 flex flex-col items-center gap-1.5" style={{ width: size }}>
      <AvatarPicker value={value} onChange={onChange} size={size} />

      {enabled && (
        expanded ? (
          <div className="flex flex-col gap-1" style={{ minWidth: 176 }}>
            <input
              autoFocus
              type="text"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder={t(($) => $.prompt_placeholder)}
              className="h-7 w-full rounded border bg-background px-2 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
              onKeyDown={(e) => {
                if (e.key === "Enter") void handleGenerate();
                if (e.key === "Escape") setExpanded(false);
              }}
            />
            <div className="flex gap-1">
              <button
                type="button"
                onClick={() => void handleGenerate()}
                disabled={generating}
                className={cn(
                  "flex flex-1 items-center justify-center gap-1 rounded bg-primary px-2 py-1 text-[11px] text-primary-foreground transition-opacity",
                  "hover:opacity-90 disabled:opacity-50",
                )}
              >
                {generating ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Sparkles className="h-3 w-3" />
                )}
                {generating ? t(($) => $.generating) : t(($) => $.generate_button)}
              </button>
              <button
                type="button"
                onClick={() => setExpanded(false)}
                className="rounded border px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted"
              >
                ✕
              </button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => setExpanded(true)}
            disabled={generating}
            className="flex items-center gap-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
          >
            <Sparkles className="h-3 w-3" />
            {t(($) => $.generate_ai)}
          </button>
        )
      )}
    </div>
  );
}
