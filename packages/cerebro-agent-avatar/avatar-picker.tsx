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
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { AvatarPicker } from "@multica/views/agents/components/avatar-picker";

interface CerebroAvatarPickerProps {
  value: string | null;
  onChange: (url: string | null) => void;
  agentName: string;
  size?: number;
}

/**
 * Extends AvatarPicker with an optional AI generation button.
 * When cerebro_agent_avatar is enabled and the Data Registry gateway is
 * configured on the server, a "Generate AI" trigger appears below the avatar
 * square. Clicking it opens a real centred modal with an optional prompt
 * input — leaving it blank triggers auto-generation with a Scandinavian-
 * appearance prompt and a background colour seeded from the agent name.
 */
export function CerebroAvatarPicker({
  value,
  onChange,
  agentName,
  size = 56,
}: CerebroAvatarPickerProps) {
  const { t } = useT("cerebro-agent-avatar");
  const enabled = useFeatureFlag("cerebro_agent_avatar");
  const [open, setOpen] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [prompt, setPrompt] = useState("");

  const handleGenerate = async () => {
    setGenerating(true);
    try {
      const result = await api.generateAgentAvatar(
        agentName || "Agent",
        prompt || undefined,
      );
      onChange(result.url);
      setOpen(false);
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
    <div
      className="flex shrink-0 flex-col items-center gap-1.5"
      style={{ width: size }}
    >
      <AvatarPicker value={value} onChange={onChange} size={size} />

      {enabled && (
        <>
          <button
            type="button"
            onClick={() => setOpen(true)}
            disabled={generating}
            className="flex items-center gap-1 text-[11px] text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
          >
            {generating ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Sparkles className="h-3 w-3" />
            )}
            {generating ? t(($) => $.generating) : t(($) => $.generate_ai)}
          </button>

          <Dialog
            open={open}
            onOpenChange={(next) => {
              // Don't let a backdrop click or Esc abandon an in-flight
              // generation — the request keeps running server-side (~45s) and
              // its result/toast would resolve against a closed dialog.
              if (!generating) setOpen(next);
            }}
          >
            <DialogContent className="sm:max-w-md">
              <DialogHeader>
                <DialogTitle>{t(($) => $.modal_title)}</DialogTitle>
                <DialogDescription>
                  {t(($) => $.modal_description)}
                </DialogDescription>
              </DialogHeader>

              <input
                autoFocus
                type="text"
                value={prompt}
                disabled={generating}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder={t(($) => $.prompt_placeholder)}
                className="h-9 w-full rounded-md border bg-background px-3 text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !generating) void handleGenerate();
                }}
              />

              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  disabled={generating}
                  onClick={() => setOpen(false)}
                >
                  {t(($) => $.cancel)}
                </Button>
                <Button
                  type="button"
                  disabled={generating}
                  onClick={() => void handleGenerate()}
                >
                  {generating ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <Sparkles className="h-4 w-4" />
                  )}
                  {generating
                    ? t(($) => $.generating)
                    : t(($) => $.generate_button)}
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  );
}
