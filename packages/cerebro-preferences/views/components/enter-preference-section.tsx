"use client";

// CEREBRO-PATCH(enter-preference-section): cerebro modification of upstream file

import { useState } from "react";
import { Keyboard } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { SUBMIT_ON_ENTER_KEY } from "./use-submit-on-enter";

/**
 * Fork-specific: lets the user choose between Enter-to-send and
 * Cmd/Ctrl+Enter-to-send across all message composers (chat, comments, replies).
 *
 * Default is Cmd/Ctrl+Enter (preserves the long-form composer behaviour);
 * users opt into Enter-to-send when they treat composers as chat inputs.
 */
export function EnterPreferenceSection() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);
  const [saving, setSaving] = useState(false);

  const submitOnEnter = user?.preferences?.[SUBMIT_ON_ENTER_KEY] === true;
  const isMac = typeof navigator !== "undefined" && /Mac|iPhone|iPad|iPod/i.test(navigator.platform);
  const modKey = isMac ? "⌘" : "Ctrl";

  const handleChange = async (next: boolean) => {
    setSaving(true);
    try {
      const updated = await api.updateMyPreferences({ [SUBMIT_ON_ENTER_KEY]: next });
      setUser(updated);
      toast.success(next ? "Enter now sends" : `${modKey}+Enter now sends`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update preference");
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="space-y-4">
      <div className="flex items-center gap-2">
        <Keyboard className="h-4 w-4 text-muted-foreground" />
        <h2 className="text-sm font-semibold">Composer</h2>
      </div>

      <Card>
        <CardContent>
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-1">
              <Label className="text-sm font-medium">Send messages with Enter</Label>
              <p className="text-xs text-muted-foreground">
                {submitOnEnter
                  ? `Enter sends. Use Shift+Enter (or ${modKey}+Enter) for a new line.`
                  : `${modKey}+Enter sends. Enter inserts a new line.`}
                <br />
                Applies to chat, comments, and replies.
              </p>
            </div>
            <Switch
              checked={submitOnEnter}
              disabled={saving || !user}
              onCheckedChange={handleChange}
              aria-label="Send messages with Enter"
            />
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
